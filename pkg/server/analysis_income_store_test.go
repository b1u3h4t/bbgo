package server

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"

	"github.com/c9s/bbgo/pkg/exchange/binance/binanceapi"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

func TestIncomeStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := sqlx.Connect("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, ensureIncomeTables(ctx, db, "sqlite3"))

	ts := time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)
	rows := []binanceapi.FuturesIncome{
		{Symbol: "BTCUSDT", IncomeType: binanceapi.FuturesIncomeRealizedPnL, Income: fixedpoint.MustNewFromString("12.5"), Asset: "USDT", Time: types.MillisecondTimestamp(ts), TranId: 1},
		{Symbol: "BTCUSDT", IncomeType: binanceapi.FuturesIncomeCommission, Income: fixedpoint.MustNewFromString("-0.0012"), Asset: "BNB", Time: types.MillisecondTimestamp(ts), TranId: 1},
	}
	require.NoError(t, insertIncomes(ctx, db, "sqlite3", rows))
	require.NoError(t, insertIncomes(ctx, db, "sqlite3", rows), "duplicate rows must be ignored")

	require.NoError(t, setIncomeCursor(ctx, db, "sqlite3", binanceapi.FuturesIncomeRealizedPnL, ts))
	require.NoError(t, setIncomeCursor(ctx, db, "sqlite3", binanceapi.FuturesIncomeRealizedPnL, ts.Add(time.Hour)))
	var until int64
	require.NoError(t, db.Get(&until, `SELECT synced_until_ms FROM analysis_futures_income_sync WHERE income_type = ?`, "REALIZED_PNL"))
	require.Equal(t, ts.Add(time.Hour).UnixMilli(), until)

	var got []incomeRecord
	require.NoError(t, db.Select(&got, `SELECT income_type, symbol, asset, income, time_ms FROM analysis_futures_incomes WHERE time_ms >= ? AND time_ms <= ? ORDER BY time_ms`, ts.UnixMilli(), ts.UnixMilli()))
	require.Len(t, got, 2)
	sum := 0.0
	for _, r := range got {
		sum += r.Income
	}
	require.InDelta(t, 12.4988, sum, 1e-9)
}
