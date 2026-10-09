package server

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/types"
)

type regimeHorizon struct {
	Name       string       `json:"name"` // short | medium | long
	Label      string       `json:"label"`
	Bias       string       `json:"bias"`
	Summary    string       `json:"summary"`
	Timeframes []regimeTFState `json:"timeframes"`
	NextClose  *regimeClock `json:"nextClose,omitempty"`
	FlipUp     []string     `json:"flipUp"`
	FlipDown   []string     `json:"flipDown"`
}

type regimeClock struct {
	Interval   string    `json:"interval"`
	UTC        time.Time `json:"utc"`
	CST        string    `json:"cst"`
	InHours    float64   `json:"inHours"`
	Note       string    `json:"note"`
}

type regimeCurrentEpisode struct {
	Symbol          string  `json:"symbol"`
	Interval        string  `json:"interval"`
	High            float64 `json:"high"`
	Low             float64 `json:"low"`
	DropPct         float64 `json:"dropPct"`
	BarsSinceLow    int     `json:"barsSinceLow"`
	HoursSinceLow   float64 `json:"hoursSinceLow"`
	Last            float64 `json:"last"`
	AboveEMA20      bool    `json:"aboveEMA20"`
	HistP50Hours    float64 `json:"histP50Hours"`
	HistP80Hours    float64 `json:"histP80Hours"`
	WindowNote      string  `json:"windowNote"`
	SuccessRateHist float64 `json:"successRateHist"`
}

type analysisRegimeResp struct {
	Symbol      string                `json:"symbol"`
	AsOf        time.Time             `json:"asOf"`
	Timezone    string                `json:"timezone"`
	Horizons    []regimeHorizon       `json:"horizons"`
	Clocks      []regimeClock         `json:"clocks"`
	Current     *regimeCurrentEpisode `json:"current,omitempty"`
	Backtest4h  regimeBTPool          `json:"backtest4h"`
	Backtest1d  regimeBTPool          `json:"backtest1d"`
	Rules       []string              `json:"rules"`
	SymbolsUsed []string              `json:"symbolsUsed"`
	KlineSource string                `json:"klineSource"`
	TookMs      int64                 `json:"tookMs"`
}

func nextIntervalClose(now time.Time, iv types.Interval) time.Time {
	d := iv.Duration()
	if d <= 0 {
		return now
	}
	unix := now.Unix()
	sec := int64(d / time.Second)
	if sec <= 0 {
		return now
	}
	next := (unix/sec + 1) * sec
	return time.Unix(next, 0).UTC()
}

func formatCST(t time.Time) string {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return t.In(loc).Format("2006-01-02 15:04 MST")
}

func makeClock(now time.Time, iv types.Interval, note string) regimeClock {
	next := nextIntervalClose(now, iv)
	hrs := next.Sub(now).Hours()
	if hrs < 0 {
		hrs = 0
	}
	return regimeClock{
		Interval: string(iv),
		UTC:      next,
		CST:      formatCST(next),
		InHours:  roundFloat(hrs, 2),
		Note:     note,
	}
}

func mergeBias(a, b string) string {
	if a == b {
		return a
	}
	if (a == "bull" && b == "chop") || (a == "chop" && b == "bull") {
		return "chop"
	}
	if (a == "bear" && b == "chop") || (a == "chop" && b == "bear") {
		return "chop"
	}
	return "chop"
}

func (s *Server) analysisRegime(c *gin.Context) {
	session, ok := s.sessionOrAbort(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	started := time.Now()

	symbol := strings.ToUpper(strings.TrimSpace(c.DefaultQuery("symbol", "BTCUSDT")))
	if symbol == "" {
		symbol = "BTCUSDT"
	}
	btSymbols := analysisDefaultSymbols()
	// ensure focus symbol is included
	hasFocus := false
	for _, sy := range btSymbols {
		if sy == symbol {
			hasFocus = true
			break
		}
	}
	if !hasFocus {
		btSymbols = append([]string{symbol}, btSymbols...)
	}

	now := time.Now().UTC()
	clocks := []regimeClock{
		makeClock(now, types.Interval4h, "中周期确认：下一根 4h 收盘"),
		makeClock(now, types.Interval1d, "中/长周期确认：下一根日线收盘"),
		makeClock(now, types.Interval1w, "长周期确认：下一根周线收盘（周一开盘对齐）"),
	}

	// live TFs for focus symbol
	type tfReq struct {
		iv    types.Interval
		limit int
	}
	reqs := []tfReq{
		{types.Interval15m, 120},
		{types.Interval1h, 120},
		{types.Interval4h, 200},
		{types.Interval1d, 200},
		{types.Interval1w, 120},
	}
	tfMap := map[string]regimeTFState{}
	klineSources := map[string]int{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, r := range reqs {
		wg.Add(1)
		go func(r tfReq) {
			defer wg.Done()
			ks, src, err := s.queryAnalysisKLines(ctx, session, symbol, r.iv, r.limit)
			st := classifyTF(ks)
			st.Interval = string(r.iv)
			mu.Lock()
			tfMap[string(r.iv)] = st
			if err != nil {
				log.WithError(err).Warnf("regime: klines %s %s", symbol, r.iv)
			} else {
				klineSources[src]++
			}
			mu.Unlock()
		}(r)
	}
	wg.Wait()

	short := buildHorizon("short", "短周期（15m+1h）", tfMap["15m"], tfMap["1h"],
		&clocks[0], // reuse 4h as nearby — replace below
		[]string{
			"1h 收盘重新站上 EMA20 且 RSI 回升 > 45",
			"15m 不再创新低，连续 2 根收阳",
		},
		[]string{
			"1h 收盘跌破近期摆动低点",
			"15m/1h 同时收在 EMA20 下方且 EMA20 向下",
		},
	)
	// short next close = 1h
	clk1h := makeClock(now, types.Interval1h, "短周期确认：下一根 1h 收盘")
	short.NextClose = &clk1h

	med := buildHorizon("medium", "中周期（4h+日线）", tfMap["4h"], tfMap["1d"],
		&clocks[0],
		[]string{
			"4h 收盘站回 EMA20，且日线仍在 EMA50 上方",
			"日线收盘创出反弹高点并站上前一日高点",
		},
		[]string{
			"4h 收盘跌破本轮摆动低点",
			"日线收盘跌破 EMA50 且 EMA20 拐头向下",
		},
	)

	longTF := tfMap["1d"]
	weekTF := tfMap["1w"]
	longBias := mergeBias(longTF.Bias, weekTF.Bias)
	if weekTF.Bars > 0 && weekTF.Bias != "chop" {
		longBias = weekTF.Bias
	}
	long := regimeHorizon{
		Name:       "long",
		Label:      "长周期（日线+周线）",
		Bias:       longBias,
		Timeframes: filterTF(longTF, weekTF),
		NextClose:  &clocks[2],
		FlipUp: []string{
			"周线收盘站上 EMA20",
			"日线重新站上 EMA50 且周线未破前低",
		},
		FlipDown: []string{
			"周线收盘跌破 EMA20 / 前低",
			"日线收盘跌破本轮上升结构低点",
		},
	}
	long.Summary = horizonSummary(long.Bias, long.Label)

	horizons := []regimeHorizon{short, med, long}

	// multi-symbol historical backtest (4h + 1d)
	bt4h, open4hFocus := s.regimeBacktestPool(ctx, session, btSymbols, types.Interval4h, 300, 4.0, symbol)
	bt1d, _ := s.regimeBacktestPool(ctx, session, btSymbols, types.Interval1d, 260, 24.0, symbol)
	bt4h.Interval = "4h"
	bt1d.Interval = "1d"

	var current *regimeCurrentEpisode
	ks4h, _, _ := s.queryAnalysisKLines(ctx, session, symbol, types.Interval4h, 200)
	if len(ks4h) > regimeWarmup {
		hiIdx, lowIdx, hi, lo := findRecentSwing(ks4h)
		if hiIdx >= 0 && lowIdx >= hiIdx {
			closes := make([]float64, len(ks4h))
			for i, k := range ks4h {
				closes[i] = k.Close.Float64()
			}
			ema20 := emaSeries(closes, regimeEMAFast)
			last := closes[len(closes)-1]
			barsSince := len(ks4h) - 1 - lowIdx
			if barsSince < 0 {
				barsSince = 0
			}
			drop := 0.0
			if hi > 0 {
				drop = (hi - lo) / hi * 100
			}
			current = &regimeCurrentEpisode{
				Symbol:          symbol,
				Interval:        "4h",
				High:            roundFloat(hi, 8),
				Low:             roundFloat(lo, 8),
				DropPct:         roundFloat(drop, 2),
				BarsSinceLow:    barsSince,
				HoursSinceLow:   roundFloat(float64(barsSince)*4, 1),
				Last:            roundFloat(last, 8),
				AboveEMA20:      last > ema20[len(ema20)-1],
				HistP50Hours:    bt4h.ToEMA20.P50Hours,
				HistP80Hours:    bt4h.ToEMA20.P80Hours,
				SuccessRateHist: bt4h.ToEMA20.SuccessRate,
			}
			switch {
			case current.AboveEMA20:
				current.WindowNote = "已站上 EMA20：中周期反弹确认条件之一已满足，等日线收盘验证"
			case bt4h.ToEMA20.P80Hours > 0 && current.HoursSinceLow > bt4h.ToEMA20.P80Hours:
				current.WindowNote = "反弹时长已超过历史 P80，继续磨底或失败概率上升，盯 4h 破低"
			case bt4h.ToEMA20.P50Hours > 0 && current.HoursSinceLow >= bt4h.ToEMA20.P50Hours:
				current.WindowNote = "进入历史中位～P80 窗口：变盘（站回 EMA20 或破低）临近"
			default:
				current.WindowNote = "仍在历史中位窗口内：默认当作中周期回调，用下一根 4h/日线收盘裁决"
			}
			_ = open4hFocus
		}
	}

	srcParts := make([]string, 0, len(klineSources))
	for k := range klineSources {
		srcParts = append(srcParts, k)
	}
	sort.Strings(srcParts)

	c.JSON(http.StatusOK, analysisRegimeResp{
		Symbol:   symbol,
		AsOf:     now,
		Timezone: "Asia/Shanghai",
		Horizons: horizons,
		Clocks:   clocks,
		Current:  current,
		Backtest4h: bt4h,
		Backtest1d: bt1d,
		Rules: []string{
			"变盘不看时钟，看收盘：短看 1h，中看 4h+日线，长看周线。",
			"历史回测：各币种 4h/日线出现 ≥5% 回调后，统计反弹到触及 EMA20（成功）或跌破摆动低点（失败）的时长分布。",
			"P50/P80 是历史分位窗口，不是预测点位；超时未确认则风险升、仓位应降。",
			"三周期共振（短中长同向）才提高操作权重；冲突时以更长周期为准。",
		},
		SymbolsUsed: btSymbols,
		KlineSource: strings.Join(srcParts, ","),
		TookMs:      time.Since(started).Milliseconds(),
	})
}

func filterTF(tfs ...regimeTFState) []regimeTFState {
	out := make([]regimeTFState, 0, len(tfs))
	for _, t := range tfs {
		if t.Bars > 0 || t.Last > 0 {
			out = append(out, t)
		}
	}
	return out
}

func horizonSummary(bias, label string) string {
	switch bias {
	case "bull":
		return label + "：偏多"
	case "bear":
		return label + "：偏空"
	default:
		return label + "：震荡/过渡"
	}
}

func buildHorizon(name, label string, a, b regimeTFState, next *regimeClock, up, down []string) regimeHorizon {
	bias := mergeBias(a.Bias, b.Bias)
	return regimeHorizon{
		Name:       name,
		Label:      label,
		Bias:       bias,
		Summary:    horizonSummary(bias, label),
		Timeframes: filterTF(a, b),
		NextClose:  next,
		FlipUp:     up,
		FlipDown:   down,
	}
}

func (s *Server) regimeBacktestPool(
	ctx context.Context,
	session *bbgo.ExchangeSession,
	symbols []string,
	iv types.Interval,
	limit int,
	hoursPerBar float64,
	focus string,
) (regimeBTPool, []regimeEpisode) {
	pool := regimeBTPool{Interval: string(iv)}
	var (
		mu          sync.Mutex
		wg          sync.WaitGroup
		successBars []float64
		failBars    []float64
		allBars     []float64
		successN    int
		okSymbols   int
		openFocus   []regimeEpisode
	)
	sem := make(chan struct{}, 4)
	for _, sy := range symbols {
		wg.Add(1)
		go func(sy string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ks, _, err := s.queryAnalysisKLines(ctx, session, sy, iv, limit)
			if err != nil || len(ks) < regimeWarmup+10 {
				return
			}
			_, openEps := backtestRegimeRebounds(sy, ks, hoursPerBar)
			rawSuccess, rawFail, rawAll, sn, _ := regimeCollectBars(ks)
			mu.Lock()
			defer mu.Unlock()
			okSymbols++
			successBars = append(successBars, rawSuccess...)
			failBars = append(failBars, rawFail...)
			allBars = append(allBars, rawAll...)
			successN += sn
			if sy == focus {
				openFocus = openEps
			}
		}(sy)
	}
	wg.Wait()

	closedN := len(allBars)
	pool.Symbols = okSymbols
	pool.Episodes = closedN
	pool.ToEMA20 = summarizeDurations(successBars, hoursPerBar, successN, closedN)
	pool.ToFail = summarizeDurations(failBars, hoursPerBar, 0, closedN)
	pool.AllClosed = summarizeDurations(allBars, hoursPerBar, successN, closedN)
	if len(openFocus) > 0 {
		pool.RecentOpen = openFocus[len(openFocus)-1:]
	}
	return pool, openFocus
}

func regimeCollectBars(klines []types.KLine) (success, fail, all []float64, successN, closedN int) {
	n := len(klines)
	if n < regimeWarmup+10 {
		return
	}
	highs := make([]float64, n)
	lows := make([]float64, n)
	closes := make([]float64, n)
	for i, k := range klines {
		highs[i] = k.High.Float64()
		lows[i] = k.Low.Float64()
		closes[i] = k.Close.Float64()
	}
	emaF := emaSeries(closes, regimeEMAFast)
	i := regimeWarmup
	for i < n-2 {
		if !isSwingHigh(highs, i, regimeSwingLook) {
			i++
			continue
		}
		hiIdx, hi := i, highs[i]
		lowIdx := -1
		lowPx := hi
		for j := i + 1; j < n-1 && j <= i+regimeMaxRebound; j++ {
			if lows[j] < lowPx {
				lowPx = lows[j]
				lowIdx = j
			}
			drop := (hi - lowPx) / hi * 100
			if lowIdx > hiIdx && drop >= regimeMinDropPct && isSwingLow(lows, lowIdx, regimeSwingLook) {
				break
			}
			if lowIdx > hiIdx && drop >= regimeMinDropPct && j-lowIdx >= 1 && closes[j] > closes[lowIdx] {
				break
			}
		}
		if lowIdx < 0 || lowIdx <= hiIdx {
			i++
			continue
		}
		drop := (hi - lowPx) / hi * 100
		if drop < regimeMinDropPct {
			i++
			continue
		}
		outcome := "open"
		end := lowIdx
		for j := lowIdx + 1; j < n && j-lowIdx <= regimeMaxRebound; j++ {
			end = j
			if closes[j] > emaF[j] && emaF[j] > 0 {
				outcome = "ema20"
				break
			}
			if lows[j] < lowPx*0.998 {
				outcome = "fail"
				break
			}
		}
		if outcome == "open" {
			i = lowIdx + 1
			continue
		}
		bars := float64(end - lowIdx)
		if bars < 1 {
			bars = 1
		}
		all = append(all, bars)
		closedN++
		if outcome == "ema20" {
			success = append(success, bars)
			successN++
		} else {
			fail = append(fail, bars)
		}
		i = end + 1
	}
	return
}
