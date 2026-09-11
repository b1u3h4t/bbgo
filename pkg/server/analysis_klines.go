package server

import (
	"context"
	"sort"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/service"
	"github.com/c9s/bbgo/pkg/strategy/grid2"
	"github.com/c9s/bbgo/pkg/types"
)

var (
	analysisKlineSyncOnce sync.Once
	analysisKlineSyncMu   sync.Mutex
)

func (s *Server) analysisBacktestService() *service.BacktestService {
	if s.Environ == nil || s.Environ.DatabaseService == nil || s.Environ.DatabaseService.DB == nil {
		return nil
	}
	return service.NewBacktestService(s.Environ.DatabaseService.DB)
}

func analysisDefaultSymbols() []string {
	return []string{
		"BTCUSDT", "ETHUSDT", "XRPUSDT", "NEARUSDT", "HYPEUSDT", "AVAXUSDT",
		"DOGEUSDT", "WLDUSDT", "ENAUSDT", "SOLUSDT", "BNBUSDT", "SUIUSDT", "DOTUSDT",
	}
}

// intervals we keep warm in MySQL (binance_futures_klines). 8h has no historical sync coverage.
func analysisSyncIntervals() []types.Interval {
	return []types.Interval{
		types.Interval5m, types.Interval15m, types.Interval30m,
		types.Interval1h, types.Interval2h, types.Interval4h,
		types.Interval6h, types.Interval12h, types.Interval1d,
	}
}

func intervalSupportedInDB(iv types.Interval) bool {
	switch iv {
	case types.Interval5m, types.Interval15m, types.Interval30m,
		types.Interval1h, types.Interval2h, types.Interval4h,
		types.Interval6h, types.Interval12h, types.Interval1d,
		types.Interval1m, types.Interval3d, types.Interval1w:
		return true
	default:
		return false // e.g. 8h — exchange only
	}
}

// queryAnalysisKLines prefers MySQL binance_*_klines, then fills / refreshes the tip from the exchange.
// Returns klines (oldest→newest), data source tag, error.
func (s *Server) queryAnalysisKLines(
	ctx context.Context,
	session *bbgo.ExchangeSession,
	symbol string,
	interval types.Interval,
	limit int,
) ([]types.KLine, string, error) {
	if limit <= 0 {
		limit = 72
	}

	bt := s.analysisBacktestService()
	var dbKlines []types.KLine
	source := "exchange"

	if bt != nil && intervalSupportedInDB(interval) {
		if ks, err := bt.QueryKLinesBackward(session.Exchange, symbol, interval, time.Now(), limit); err == nil && len(ks) > 0 {
			dbKlines = ks
			source = "mysql"
		}
	}

	needExchange := len(dbKlines) == 0
	if len(dbKlines) > 0 {
		last := dbKlines[len(dbKlines)-1]
		// always refresh forming / recent candles from exchange
		gap := time.Since(last.EndTime.Time())
		if gap > interval.Duration()/2 || !last.Closed {
			needExchange = true
		}
		if len(dbKlines) < limit {
			needExchange = true
		}
	}

	if !needExchange {
		return dbKlines, source, nil
	}

	exLimit := limit
	if exLimit < 100 {
		exLimit = 100
	}
	if exLimit > 500 {
		exLimit = 500
	}
	exKlines, err := session.Exchange.QueryKLines(ctx, symbol, interval, types.KLineQueryOptions{Limit: exLimit})
	if err != nil {
		if len(dbKlines) > 0 {
			return trimKLinesTail(dbKlines, limit), source + "-stale", nil
		}
		return nil, source, err
	}

	merged := mergeKLinesPreferNewer(dbKlines, exKlines)
	if len(dbKlines) > 0 {
		source = "mysql+exchange"
	} else {
		source = "exchange"
	}

	// best-effort persist closed bars back into MySQL
	if bt != nil && intervalSupportedInDB(interval) {
		go s.persistClosedKlines(bt, session.Exchange, exKlines)
	}

	return trimKLinesTail(merged, limit), source, nil
}

func trimKLinesTail(klines []types.KLine, limit int) []types.KLine {
	if limit <= 0 || len(klines) <= limit {
		return klines
	}
	return klines[len(klines)-limit:]
}

func mergeKLinesPreferNewer(db, ex []types.KLine) []types.KLine {
	if len(db) == 0 {
		return ex
	}
	if len(ex) == 0 {
		return db
	}
	byStart := map[int64]types.KLine{}
	order := make([]int64, 0, len(db)+len(ex))
	seen := map[int64]bool{}
	add := func(k types.KLine) {
		key := k.StartTime.UnixMilli()
		byStart[key] = k
		if !seen[key] {
			seen[key] = true
			order = append(order, key)
		}
	}
	for _, k := range db {
		add(k)
	}
	for _, k := range ex {
		add(k) // exchange overwrites same start
	}
	// sort by start time
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]types.KLine, 0, len(order))
	for _, key := range order {
		out = append(out, byStart[key])
	}
	return out
}

func (s *Server) persistClosedKlines(bt *service.BacktestService, ex types.Exchange, klines []types.KLine) {
	now := time.Now()
	for _, k := range klines {
		if len(k.Exchange) == 0 {
			k.Exchange = ex.Name()
		}
		// only store fully closed bars
		if k.EndTime.After(now) {
			continue
		}
		if k.EndTime.Before(k.StartTime.Time().Add(k.Interval.Duration() - time.Second)) {
			continue
		}
		k.Closed = true
		_ = bt.Insert(k, ex) // ignore duplicate key errors
	}
}

func (s *Server) startAnalysisKlineSync() {
	analysisKlineSyncOnce.Do(func() {
		go s.analysisKlineSyncLoop()
	})
}

func (s *Server) analysisKlineSyncLoop() {
	// let the bot finish bootstrapping sessions first
	time.Sleep(45 * time.Second)
	s.runAnalysisKlineSync(context.Background())

	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.runAnalysisKlineSync(context.Background())
	}
}

func (s *Server) runAnalysisKlineSync(ctx context.Context) {
	if !analysisKlineSyncMu.TryLock() {
		return
	}
	defer analysisKlineSyncMu.Unlock()

	bt := s.analysisBacktestService()
	if bt == nil || s.Environ == nil {
		log.Debug("analysis kline sync skipped: no database")
		return
	}
	session, ok := s.Environ.Session("binance")
	if !ok || session == nil || session.Exchange == nil {
		return
	}

	symbols := map[string]struct{}{}
	for _, sym := range analysisDefaultSymbols() {
		symbols[sym] = struct{}{}
	}
	if s.Trader != nil {
		_ = s.Trader.IterateStrategies(func(st types.StrategyID) error {
			if g, ok := st.(*grid2.Strategy); ok && g.Symbol != "" {
				symbols[g.Symbol] = struct{}{}
			}
			return nil
		})
	}

	symList := make([]string, 0, len(symbols))
	for sym := range symbols {
		symList = append(symList, sym)
	}

	end := time.Now()
	log.Infof("analysis kline sync start: %d symbols × %d intervals", len(symList), len(analysisSyncIntervals()))
	for _, sym := range symList {
		for _, iv := range analysisSyncIntervals() {
			start := end.Add(-14 * 24 * time.Hour)
			if k, err := bt.QueryKLine(session.Exchange, sym, iv, "DESC", 1); err == nil && k != nil {
				// resume from last stored bar
				start = k.StartTime.Time()
			}
			if err := bt.SyncKLineByInterval(ctx, session.Exchange, sym, iv, start, end); err != nil {
				log.WithError(err).Warnf("analysis SyncKLine %s %s", sym, iv)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	log.Info("analysis kline sync done")
}
