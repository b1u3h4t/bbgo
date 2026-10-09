package server

import (
	"math"
	"sort"
	"time"

	"github.com/c9s/bbgo/pkg/types"
)

// Historical regime timing backtest on 4h (and optional 1d) bars.
// After a ≥minDropPct decline from a swing high, measure how many bars the
// subsequent rebound lasts until it either tags EMA20 (success) or breaks the
// swing low (fail). Stats are pooled across symbols for dashboard windows.

const (
	regimeEMAFast      = 20
	regimeEMASlow      = 50
	regimeRSIPeriod    = 14
	regimeMinDropPct   = 5.0 // % decline from swing high to qualify
	regimeSwingLook    = 3   // pivot: high > neighbors
	regimeMaxRebound   = 40  // 4h bars ≈ 6.7 days
	regimeWarmup       = 60
)

type regimeDurationStats struct {
	Samples int     `json:"samples"`
	P50Bars float64 `json:"p50Bars"`
	P80Bars float64 `json:"p80Bars"`
	P50Hours float64 `json:"p50Hours"`
	P80Hours float64 `json:"p80Hours"`
	MeanBars float64 `json:"meanBars"`
	SuccessRate float64 `json:"successRate"` // % that tag EMA20 before failing
}

type regimeEpisode struct {
	Symbol       string    `json:"symbol"`
	HighTime     time.Time `json:"highTime"`
	LowTime      time.Time `json:"lowTime"`
	High         float64   `json:"high"`
	Low          float64   `json:"low"`
	DropPct      float64   `json:"dropPct"`
	ReboundBars  int       `json:"reboundBars"`
	Outcome      string    `json:"outcome"` // ema20 | fail | open
	HitEMA20     bool      `json:"hitEMA20"`
}

type regimeBTPool struct {
	Interval     string              `json:"interval"`
	Symbols      int                 `json:"symbols"`
	Episodes     int                 `json:"episodes"`
	ToEMA20      regimeDurationStats `json:"toEMA20"`
	ToFail       regimeDurationStats `json:"toFail"`
	AllClosed    regimeDurationStats `json:"allClosed"`
	RecentOpen   []regimeEpisode     `json:"recentOpen,omitempty"`
}

func percentileSorted(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := p / 100 * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	w := rank - float64(lo)
	return sorted[lo]*(1-w) + sorted[hi]*w
}

func summarizeDurations(bars []float64, hoursPerBar float64, successN, totalN int) regimeDurationStats {
	st := regimeDurationStats{Samples: len(bars)}
	if totalN > 0 {
		st.SuccessRate = roundFloat(100*float64(successN)/float64(totalN), 1)
	}
	if len(bars) == 0 {
		return st
	}
	cp := append([]float64(nil), bars...)
	sort.Float64s(cp)
	var sum float64
	for _, b := range cp {
		sum += b
	}
	st.MeanBars = roundFloat(sum/float64(len(cp)), 2)
	st.P50Bars = roundFloat(percentileSorted(cp, 50), 2)
	st.P80Bars = roundFloat(percentileSorted(cp, 80), 2)
	st.P50Hours = roundFloat(st.P50Bars*hoursPerBar, 1)
	st.P80Hours = roundFloat(st.P80Bars*hoursPerBar, 1)
	return st
}

func isSwingHigh(highs []float64, i, look int) bool {
	if i < look || i+look >= len(highs) {
		return false
	}
	for j := 1; j <= look; j++ {
		if highs[i] <= highs[i-j] || highs[i] < highs[i+j] {
			return false
		}
	}
	return true
}

func isSwingLow(lows []float64, i, look int) bool {
	if i < look || i+look >= len(lows) {
		return false
	}
	for j := 1; j <= look; j++ {
		if lows[i] >= lows[i-j] || lows[i] > lows[i+j] {
			return false
		}
	}
	return true
}

// backtestRegimeRebounds scans chronological klines for pullback→rebound episodes.
func backtestRegimeRebounds(symbol string, klines []types.KLine, hoursPerBar float64) (regimeBTPool, []regimeEpisode) {
	pool := regimeBTPool{}
	n := len(klines)
	if n < regimeWarmup+10 {
		return pool, nil
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

	var episodes []regimeEpisode
	var successBars, failBars, allBars []float64
	successN := 0

	i := regimeWarmup
	for i < n-2 {
		if !isSwingHigh(highs, i, regimeSwingLook) {
			i++
			continue
		}
		hiIdx, hi := i, highs[i]
		// find subsequent swing low with enough drop
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
			// allow non-confirmed low if we already dropped enough and price turned up
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

		ep := regimeEpisode{
			Symbol:   symbol,
			HighTime: klines[hiIdx].StartTime.Time().UTC(),
			LowTime:  klines[lowIdx].StartTime.Time().UTC(),
			High:     roundFloat(hi, 8),
			Low:      roundFloat(lowPx, 8),
			DropPct:  roundFloat(drop, 2),
			Outcome:  "open",
		}

		outcome := "open"
		end := lowIdx
		for j := lowIdx + 1; j < n && j-lowIdx <= regimeMaxRebound; j++ {
			end = j
			if closes[j] > emaF[j] && emaF[j] > 0 {
				outcome = "ema20"
				ep.HitEMA20 = true
				break
			}
			if lows[j] < lowPx*0.998 {
				outcome = "fail"
				break
			}
		}
		bars := end - lowIdx
		if bars < 1 {
			bars = 1
		}
		ep.ReboundBars = bars
		ep.Outcome = outcome

		if outcome == "open" {
			// still running — only keep if this is the latest unfinished episode
			episodes = append(episodes, ep)
			i = lowIdx + 1
			continue
		}
		allBars = append(allBars, float64(bars))
		if outcome == "ema20" {
			successBars = append(successBars, float64(bars))
			successN++
		} else {
			failBars = append(failBars, float64(bars))
		}
		episodes = append(episodes, ep)
		i = end + 1
	}

	closed := 0
	for _, ep := range episodes {
		if ep.Outcome != "open" {
			closed++
		}
	}
	pool.Episodes = closed
	pool.ToEMA20 = summarizeDurations(successBars, hoursPerBar, successN, closed)
	pool.ToFail = summarizeDurations(failBars, hoursPerBar, 0, closed)
	pool.AllClosed = summarizeDurations(allBars, hoursPerBar, successN, closed)

	var openEps []regimeEpisode
	for _, ep := range episodes {
		if ep.Outcome == "open" {
			openEps = append(openEps, ep)
		}
	}
	return pool, openEps
}

type regimeTFState struct {
	Interval   string  `json:"interval"`
	Last       float64 `json:"last"`
	EMA20      float64 `json:"ema20"`
	EMA50      float64 `json:"ema50"`
	RSI14      float64 `json:"rsi14"`
	AboveEMA20 bool    `json:"aboveEMA20"`
	AboveEMA50 bool    `json:"aboveEMA50"`
	EMA20Slope string  `json:"ema20Slope"` // up | down
	FromHi20Pct float64 `json:"fromHi20Pct"`
	Bars       int     `json:"bars"`
	Bias       string  `json:"bias"` // bull | bear | chop
	Note       string  `json:"note"`
}

func classifyTF(klines []types.KLine) regimeTFState {
	st := regimeTFState{}
	n := len(klines)
	if n < regimeEMASlow+5 {
		st.Bias = "chop"
		st.Note = "K线不足"
		return st
	}
	closes := make([]float64, n)
	highs := make([]float64, n)
	for i, k := range klines {
		closes[i] = k.Close.Float64()
		highs[i] = k.High.Float64()
	}
	ema20 := emaSeries(closes, regimeEMAFast)
	ema50 := emaSeries(closes, regimeEMASlow)
	rsi := rsiSeries(closes, regimeRSIPeriod)
	last := closes[n-1]
	e20, e50 := ema20[n-1], ema50[n-1]
	e20Prev := ema20[n-6]
	if n < 6 {
		e20Prev = ema20[0]
	}
	hi20 := highs[n-1]
	for i := n - 20; i < n; i++ {
		if i >= 0 && highs[i] > hi20 {
			hi20 = highs[i]
		}
	}
	st.Last = roundFloat(last, 8)
	st.EMA20 = roundFloat(e20, 8)
	st.EMA50 = roundFloat(e50, 8)
	st.RSI14 = roundFloat(rsi[n-1], 1)
	st.AboveEMA20 = last > e20
	st.AboveEMA50 = last > e50
	st.EMA20Slope = "down"
	if e20 > e20Prev {
		st.EMA20Slope = "up"
	}
	if hi20 > 0 {
		st.FromHi20Pct = roundFloat((last/hi20-1)*100, 2)
	}
	st.Bars = n

	switch {
	case last > e20 && last > e50 && e20 >= e20Prev:
		st.Bias = "bull"
		st.Note = "收盘在 EMA20/50 上方且 EMA20 向上"
	case last < e20 && last < e50 && e20 <= e20Prev:
		st.Bias = "bear"
		st.Note = "收盘在 EMA20/50 下方且 EMA20 向下"
	case last > e50 && last < e20:
		st.Bias = "chop"
		st.Note = "在 EMA50 上方但跌破 EMA20（回调/震荡）"
	case last < e50 && last > e20:
		st.Bias = "chop"
		st.Note = "反抽站上 EMA20 但仍在 EMA50 下"
	default:
		st.Bias = "chop"
		st.Note = "多空信号不一致"
	}
	return st
}

func findRecentSwing(klines []types.KLine) (highIdx, lowIdx int, high, low float64) {
	n := len(klines)
	highIdx, lowIdx = -1, -1
	if n < 10 {
		return
	}
	highs := make([]float64, n)
	lows := make([]float64, n)
	for i, k := range klines {
		highs[i] = k.High.Float64()
		lows[i] = k.Low.Float64()
	}
	start := n - 48
	if start < regimeWarmup {
		start = regimeWarmup
	}
	for i := start; i < n-1; i++ {
		if isSwingHigh(highs, i, regimeSwingLook) {
			if highIdx < 0 || highs[i] >= high {
				highIdx, high = i, highs[i]
			}
		}
	}
	if highIdx < 0 {
		highIdx = start
		high = highs[highIdx]
		for i := start; i < n; i++ {
			if highs[i] > high {
				high, highIdx = highs[i], i
			}
		}
	}
	low = highs[highIdx]
	lowIdx = highIdx
	for i := highIdx; i < n; i++ {
		if lows[i] < low {
			low, lowIdx = lows[i], i
		}
	}
	return
}
