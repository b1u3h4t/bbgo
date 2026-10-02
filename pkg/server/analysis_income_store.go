package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/c9s/bbgo/pkg/exchange/batch"
	"github.com/c9s/bbgo/pkg/exchange/binance"
	"github.com/c9s/bbgo/pkg/exchange/binance/binanceapi"
)

// /fapi/v1/income costs weight 30 per call, so income history is cached in the
// database and only the tail since the last sync cursor is fetched from Binance.
const (
	incomeSyncMinInterval = time.Minute
	// Binance can publish income rows slightly after their timestamp.
	incomeSyncOverlap = 10 * time.Minute
	incomeChunkSpan   = 7 * 24 * time.Hour
)

var analysisIncomeTypes = []binanceapi.FuturesIncomeType{
	binanceapi.FuturesIncomeRealizedPnL,
	binanceapi.FuturesIncomeCommission,
	binanceapi.FuturesIncomeFundingFee,
}

type incomeRecord struct {
	IncomeType string  `db:"income_type"`
	Symbol     string  `db:"symbol"`
	Asset      string  `db:"asset"`
	Income     float64 `db:"income"`
	TimeMs     int64   `db:"time_ms"`
}

var (
	incomeTablesOnce sync.Once
	incomeTablesErr  error

	incomeSyncMu          sync.Mutex
	incomeLastSync        time.Time
	incomeSyncPausedUntil time.Time
)

const incomeRateLimitPause = 15 * time.Minute

func (s *Server) incomeDB() (*sqlx.DB, string) {
	if s.Environ == nil || s.Environ.DatabaseService == nil || s.Environ.DatabaseService.DB == nil {
		return nil, ""
	}
	return s.Environ.DatabaseService.DB, s.Environ.DatabaseService.Driver
}

func ensureIncomeTables(ctx context.Context, db *sqlx.DB, driver string) error {
	incomeTablesOnce.Do(func() {
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS analysis_futures_incomes (
				income_type VARCHAR(32) NOT NULL,
				tran_id BIGINT NOT NULL,
				symbol VARCHAR(32) NOT NULL DEFAULT '',
				asset VARCHAR(16) NOT NULL DEFAULT '',
				income DECIMAL(36,18) NOT NULL,
				time_ms BIGINT NOT NULL,
				PRIMARY KEY (income_type, tran_id, symbol)
			)`,
			`CREATE TABLE IF NOT EXISTS analysis_futures_income_sync (
				income_type VARCHAR(32) NOT NULL PRIMARY KEY,
				synced_until_ms BIGINT NOT NULL
			)`,
		}
		if driver != "mysql" {
			stmts = append(stmts, `CREATE INDEX IF NOT EXISTS analysis_futures_incomes_time_idx ON analysis_futures_incomes (time_ms)`)
		}
		for _, q := range stmts {
			if _, err := db.ExecContext(ctx, q); err != nil {
				incomeTablesErr = err
				return
			}
		}
	})
	return incomeTablesErr
}

func insertIncomes(ctx context.Context, db *sqlx.DB, driver string, rows []binanceapi.FuturesIncome) error {
	if len(rows) == 0 {
		return nil
	}
	q := `INSERT INTO analysis_futures_incomes (income_type, tran_id, symbol, asset, income, time_ms) VALUES (?, ?, ?, ?, ?, ?)`
	if driver == "mysql" {
		q = `INSERT IGNORE` + q[len(`INSERT`):]
	} else {
		q += ` ON CONFLICT DO NOTHING`
	}
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PreparexContext(ctx, db.Rebind(q))
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx, string(r.IncomeType), r.TranId, r.Symbol, r.Asset, r.Income.String(), r.Time.Time().UnixMilli()); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func setIncomeCursor(ctx context.Context, db *sqlx.DB, driver string, incomeType binanceapi.FuturesIncomeType, until time.Time) error {
	q := `INSERT INTO analysis_futures_income_sync (income_type, synced_until_ms) VALUES (?, ?)`
	if driver == "mysql" {
		q += ` ON DUPLICATE KEY UPDATE synced_until_ms = VALUES(synced_until_ms)`
	} else {
		q += ` ON CONFLICT (income_type) DO UPDATE SET synced_until_ms = excluded.synced_until_ms`
	}
	_, err := db.ExecContext(ctx, db.Rebind(q), string(incomeType), until.UnixMilli())
	return err
}

// syncFuturesIncomes pulls income rows newer than each type's cursor (never before floor).
// Calls are throttled to one per incomeSyncMinInterval even when they fail, so a
// rate-limited or banned IP is not hammered by dashboard refreshes.
func syncFuturesIncomes(ctx context.Context, ex *binance.Exchange, db *sqlx.DB, driver string, floor time.Time) error {
	incomeSyncMu.Lock()
	defer incomeSyncMu.Unlock()
	if time.Since(incomeLastSync) < incomeSyncMinInterval || time.Now().Before(incomeSyncPausedUntil) {
		return nil
	}
	incomeLastSync = time.Now()
	err := syncFuturesIncomesLocked(ctx, ex, db, driver, floor)
	if batch.IsRateLimitError(err) {
		incomeSyncPausedUntil = time.Now().Add(incomeRateLimitPause)
	}
	return err
}

func syncFuturesIncomesLocked(ctx context.Context, ex *binance.Exchange, db *sqlx.DB, driver string, floor time.Time) error {

	now := time.Now()
	for _, t := range analysisIncomeTypes {
		start := floor
		var untilMs int64
		err := db.GetContext(ctx, &untilMs, db.Rebind(`SELECT synced_until_ms FROM analysis_futures_income_sync WHERE income_type = ?`), string(t))
		switch {
		case err == nil:
			if c := time.UnixMilli(untilMs).Add(-incomeSyncOverlap); c.After(start) {
				start = c
			}
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}

		for cur := start; cur.Before(now); {
			chunkStart, chunkEnd := cur, cur.Add(incomeChunkSpan)
			if chunkEnd.After(now) {
				chunkEnd = now
			}
			rows, err := ex.QueryFuturesIncomeHistory(ctx, "", t, &chunkStart, &chunkEnd)
			if err != nil {
				return fmt.Errorf("income %s: %w", t, err)
			}
			if err := insertIncomes(ctx, db, driver, rows); err != nil {
				return err
			}
			if err := setIncomeCursor(ctx, db, driver, t, chunkEnd); err != nil {
				return err
			}
			cur = chunkEnd
		}
	}
	return nil
}

// loadIncomes returns REALIZED_PNL/COMMISSION/FUNDING_FEE rows in [start, end].
// With a database it serves the cache (syncErr reports a failed tail refresh);
// without one it falls back to querying Binance directly.
func (s *Server) loadIncomes(
	ctx context.Context, ex *binance.Exchange, floor, start, end time.Time,
) (rows []incomeRecord, source string, syncErr error, err error) {
	db, driver := s.incomeDB()
	if db != nil {
		if err := ensureIncomeTables(ctx, db, driver); err != nil {
			log.WithError(err).Warn("analysis income tables unavailable, falling back to exchange")
			db = nil
		}
	}

	if db == nil {
		for _, t := range analysisIncomeTypes {
			got, err := queryFuturesIncomeChunked(ctx, ex, t, start, end)
			if err != nil {
				return nil, "exchange", nil, fmt.Errorf("income %s: %w", t, err)
			}
			for _, r := range got {
				rows = append(rows, incomeRecord{
					IncomeType: string(r.IncomeType),
					Symbol:     r.Symbol,
					Asset:      r.Asset,
					Income:     r.Income.Float64(),
					TimeMs:     r.Time.Time().UnixMilli(),
				})
			}
		}
		return rows, "exchange", nil, nil
	}

	if syncErr = syncFuturesIncomes(ctx, ex, db, driver, floor); syncErr != nil {
		log.WithError(syncErr).Warn("analysis income sync failed, serving cached rows")
	}
	err = db.SelectContext(ctx, &rows, db.Rebind(
		`SELECT income_type, symbol, asset, income, time_ms FROM analysis_futures_incomes
		 WHERE time_ms >= ? AND time_ms <= ? ORDER BY time_ms`),
		start.UnixMilli(), end.UnixMilli())
	return rows, "db", syncErr, err
}
