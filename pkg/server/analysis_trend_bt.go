package server

import (
	"math"
	"sort"

	"github.com/c9s/bbgo/pkg/types"
)

// Walk-forward pullback-continuation strategy on real OHLC.
// Long: EMA20>EMA50 + (RSI turn up from ≤35 OR touch EMA20). Short: mirror.
// Entry: next bar open. Stop 1.5×ATR14, TP 2.5×ATR14, maxHold 48 bars.
// Round-trip fee 8bps (futures taker×2). Ideal entry = signalClose × (1 ± median
// winning-trade pullback depth from historical signals).

const (
	trendBTWarmup    = 60
	trendATRPeriod   = 14
	trendEMAFast     = 20
	trendEMASlow     = 50
	trendRSIPeriod   = 14
	trendStopATRMult = 1.5
	trendTPATRMult   = 2.5
	trendMaxHold     = 48
	trendFeeRT       = 0.0008
	trendMinTrades   = 8
)

type trendSideStats struct {
	Trades       int     `json:"trades"`
	Wins         int     `json:"wins"`
	WinRate      float64 `json:"winRate"`      // 0-100
	Expectancy   float64 `json:"expectancy"`   // avg net pnl %
	ProfitFactor float64 `json:"profitFactor"`
	AvgWinPct    float64 `json:"avgWinPct"`
	AvgLossPct   float64 `json:"avgLossPct"`
	// MedianPullbackPct: among winning trades, median (signalClose−extreme)/signalClose * 100
	MedianPullbackPct float64 `json:"medianPullbackPct"`
}

type trendBTTrade struct {
	Side         int // +1 long, -1 short
	SignalClose  float64
	Entry        float64
	Exit         float64
	PnLPct       float64 // net of fee
	PullbackPct  float64 // adverse depth from signal close before favorable exit
	ExitReason   string
	BarsHeld     int
	Win          bool
}

type trendBTResult struct {
	Bars   int
	Long   trendSideStats
	Short  trendSideStats
	Trades []trendBTTrade
	// Live setup: anchored to signal close (not last). Entry target stays fixed until fill/expire.
	LiveSide        int     // 0 none, +1 long, -1 short
	LiveSignalClose float64 // frozen signal bar close
	LiveATR         float64
	LiveBarsAgo     int  // bars since signal (0 = signal on last bar)
	LiveTouched     bool // OHLC since signal already tagged ideal entry
	LiveExpired     bool // beyond maxHold without usable setup
}

func emaSeries(xs []float64, period int) []float64 {
	out := make([]float64, len(xs))
	if len(xs) == 0 || period < 1 {
		return out
	}
	if len(xs) < period {
		var s float64
		for i, x := range xs {
			s += x
			out[i] = s / float64(i+1)
		}
		return out
	}
	var sum float64
	for i := 0; i < period; i++ {
		sum += xs[i]
		out[i] = sum / float64(i+1)
	}
	out[period-1] = sum / float64(period)
	k := 2.0 / float64(period+1)
	for i := period; i < len(xs); i++ {
		out[i] = xs[i]*k + out[i-1]*(1-k)
	}
	return out
}

func atrSeries(highs, lows, closes []float64, period int) []float64 {
	n := len(closes)
	out := make([]float64, n)
	if n < 2 || period < 1 {
		return out
	}
	trs := make([]float64, n)
	for i := 1; i < n; i++ {
		tr := highs[i] - lows[i]
		if d := math.Abs(highs[i] - closes[i-1]); d > tr {
			tr = d
		}
		if d := math.Abs(lows[i] - closes[i-1]); d > tr {
			tr = d
		}
		trs[i] = tr
	}
	if n <= period {
		var s float64
		for i := 1; i < n; i++ {
			s += trs[i]
			out[i] = s / float64(i)
		}
		return out
	}
	var s float64
	for i := 1; i <= period; i++ {
		s += trs[i]
	}
	out[period] = s / float64(period)
	for i := period + 1; i < n; i++ {
		out[i] = (out[i-1]*float64(period-1) + trs[i]) / float64(period)
	}
	return out
}

func rsiSeries(closes []float64, period int) []float64 {
	n := len(closes)
	out := make([]float64, n)
	for i := range out {
		out[i] = 50
	}
	if n < period+1 || period < 1 {
		return out
	}
	var avgGain, avgLoss float64
	for i := 1; i <= period; i++ {
		d := closes[i] - closes[i-1]
		if d >= 0 {
			avgGain += d
		} else {
			avgLoss -= d
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)
	if avgLoss == 0 {
		out[period] = 100
	} else {
		out[period] = 100 - 100/(1+avgGain/avgLoss)
	}
	for i := period + 1; i < n; i++ {
		d := closes[i] - closes[i-1]
		var g, l float64
		if d >= 0 {
			g = d
		} else {
			l = -d
		}
		avgGain = (avgGain*float64(period-1) + g) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + l) / float64(period)
		if avgLoss == 0 {
			out[i] = 100
		} else {
			out[i] = 100 - 100/(1+avgGain/avgLoss)
		}
	}
	return out
}

func medianFloat(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]float64(nil), xs...)
	sort.Float64s(cp)
	m := len(cp) / 2
	if len(cp)%2 == 0 {
		return (cp[m-1] + cp[m]) / 2
	}
	return cp[m]
}

func summarizeSide(trades []trendBTTrade, side int) trendSideStats {
	var st trendSideStats
	var winSum, lossSum float64
	var pullbacks []float64
	var lossesAbs float64
	for _, t := range trades {
		if t.Side != side {
			continue
		}
		st.Trades++
		if t.Win {
			st.Wins++
			winSum += t.PnLPct
			pullbacks = append(pullbacks, t.PullbackPct)
		} else {
			lossSum += t.PnLPct
			lossesAbs += -t.PnLPct
		}
	}
	if st.Trades == 0 {
		return st
	}
	st.WinRate = roundFloat(100*float64(st.Wins)/float64(st.Trades), 1)
	total := winSum + lossSum
	st.Expectancy = roundFloat(total/float64(st.Trades), 3)
	if st.Wins > 0 {
		st.AvgWinPct = roundFloat(winSum/float64(st.Wins), 3)
	}
	losses := st.Trades - st.Wins
	if losses > 0 {
		st.AvgLossPct = roundFloat(lossSum/float64(losses), 3)
	}
	if lossesAbs > 1e-12 {
		st.ProfitFactor = roundFloat(winSum/lossesAbs, 3)
	} else if winSum > 0 {
		st.ProfitFactor = 99
	}
	st.MedianPullbackPct = roundFloat(medianFloat(pullbacks), 3)
	return st
}

// backtestTrendPullback runs a bar-by-bar simulation on chronological klines.
func backtestTrendPullback(klines []types.KLine) trendBTResult {
	res := trendBTResult{Bars: len(klines)}
	n := len(klines)
	if n < trendBTWarmup+trendMaxHold+5 {
		return res
	}

	highs := make([]float64, n)
	lows := make([]float64, n)
	opens := make([]float64, n)
	closes := make([]float64, n)
	for i, k := range klines {
		highs[i] = k.High.Float64()
		lows[i] = k.Low.Float64()
		opens[i] = k.Open.Float64()
		closes[i] = k.Close.Float64()
	}
	emaF := emaSeries(closes, trendEMAFast)
	emaS := emaSeries(closes, trendEMASlow)
	rsi := rsiSeries(closes, trendRSIPeriod)
	atr := atrSeries(highs, lows, closes, trendATRPeriod)

	longSig := func(i int) bool {
		if i < 1 || atr[i] <= 0 {
			return false
		}
		if emaF[i] <= emaS[i] {
			return false
		}
		rsiTurn := rsi[i-1] <= 35 && rsi[i] > rsi[i-1]
		touch := lows[i] <= emaF[i]*1.001 && closes[i] > emaS[i]
		return rsiTurn || touch
	}
	shortSig := func(i int) bool {
		if i < 1 || atr[i] <= 0 {
			return false
		}
		if emaF[i] >= emaS[i] {
			return false
		}
		rsiTurn := rsi[i-1] >= 65 && rsi[i] < rsi[i-1]
		touch := highs[i] >= emaF[i]*0.999 && closes[i] < emaS[i]
		return rsiTurn || touch
	}

	type posState struct {
		active       bool
		side         int
		entry        float64
		stop         float64
		tp           float64
		entryIdx     int
		signalClose  float64
		extremeAdv   float64 // worst price vs signal (low for long, high for short)
	}
	type pendingState struct {
		active      bool
		side        int
		signalIdx   int
		signalClose float64
		atr         float64
	}

	var pos posState
	var pending pendingState
	var trades []trendBTTrade

	closePos := func(exitIdx int, price float64, reason string) {
		if !pos.active || pos.entry <= 0 {
			pos = posState{}
			return
		}
		pnl := float64(pos.side) * (price/pos.entry - 1) * 100
		pnl -= trendFeeRT * 100
		pb := 0.0
		if pos.signalClose > 0 {
			if pos.side > 0 {
				pb = (pos.signalClose - pos.extremeAdv) / pos.signalClose * 100
			} else {
				pb = (pos.extremeAdv - pos.signalClose) / pos.signalClose * 100
			}
			if pb < 0 {
				pb = 0
			}
		}
		trades = append(trades, trendBTTrade{
			Side:        pos.side,
			SignalClose: pos.signalClose,
			Entry:       pos.entry,
			Exit:        price,
			PnLPct:      pnl,
			PullbackPct: pb,
			ExitReason:  reason,
			BarsHeld:    exitIdx - pos.entryIdx,
			Win:         pnl > 0,
		})
		pos = posState{}
	}

	for i := trendBTWarmup; i < n; i++ {
		// 1) manage open position on bar i
		if pos.active {
			if pos.side > 0 {
				if lows[i] < pos.extremeAdv || pos.extremeAdv == 0 {
					pos.extremeAdv = lows[i]
				}
				hitSL := lows[i] <= pos.stop
				hitTP := highs[i] >= pos.tp
				switch {
				case hitSL && hitTP:
					closePos(i, pos.stop, "sl") // conservative
				case hitSL:
					closePos(i, pos.stop, "sl")
				case hitTP:
					closePos(i, pos.tp, "tp")
				case i-pos.entryIdx >= trendMaxHold:
					closePos(i, closes[i], "timeout")
				}
			} else if pos.side < 0 {
				if highs[i] > pos.extremeAdv || pos.extremeAdv == 0 {
					pos.extremeAdv = highs[i]
				}
				hitSL := highs[i] >= pos.stop
				hitTP := lows[i] <= pos.tp
				switch {
				case hitSL && hitTP:
					closePos(i, pos.stop, "sl")
				case hitSL:
					closePos(i, pos.stop, "sl")
				case hitTP:
					closePos(i, pos.tp, "tp")
				case i-pos.entryIdx >= trendMaxHold:
					closePos(i, closes[i], "timeout")
				}
			}
		}

		// 2) fill pending at this bar open
		if !pos.active && pending.active {
			entry := opens[i]
			if entry <= 0 {
				pending = pendingState{}
			} else {
				atrV := pending.atr
				if atrV <= 0 {
					atrV = entry * 0.01
				}
				pos = posState{
					active:      true,
					side:        pending.side,
					entry:       entry,
					entryIdx:    i,
					signalClose: pending.signalClose,
					extremeAdv:  entry,
				}
				if pending.side > 0 {
					pos.stop = entry - trendStopATRMult*atrV
					pos.tp = entry + trendTPATRMult*atrV
					pos.extremeAdv = entry
				} else {
					pos.stop = entry + trendStopATRMult*atrV
					pos.tp = entry - trendTPATRMult*atrV
					pos.extremeAdv = entry
				}
				pending = pendingState{}
				// same-bar exit check after fill
				if pos.side > 0 {
					if lows[i] < pos.extremeAdv {
						pos.extremeAdv = lows[i]
					}
					if lows[i] <= pos.stop {
						closePos(i, pos.stop, "sl")
					} else if highs[i] >= pos.tp {
						closePos(i, pos.tp, "tp")
					}
				} else {
					if highs[i] > pos.extremeAdv {
						pos.extremeAdv = highs[i]
					}
					if highs[i] >= pos.stop {
						closePos(i, pos.stop, "sl")
					} else if lows[i] <= pos.tp {
						closePos(i, pos.tp, "tp")
					}
				}
			}
		}

		// 3) new signal on closed bar (need a next bar to enter)
		if pos.active || pending.active {
			continue
		}
		if i >= n-1 {
			res.LiveATR = atr[i]
			continue
		}
		if longSig(i) {
			pending = pendingState{active: true, side: 1, signalIdx: i, signalClose: closes[i], atr: atr[i]}
		} else if shortSig(i) {
			pending = pendingState{active: true, side: -1, signalIdx: i, signalClose: closes[i], atr: atr[i]}
		}
	}

	// force-close open pos at last close
	if pos.active {
		closePos(n-1, closes[n-1], "eod")
	}

	res.Trades = trades
	res.Long = summarizeSide(trades, 1)
	res.Short = summarizeSide(trades, -1)
	resolveLiveSetup(&res, highs, lows, closes, atr, longSig, shortSig)
	return res
}

// resolveLiveSetup finds the earliest signal in the latest same-side streak within maxHold,
// so ideal entry is anchored to that signal close and does not chase last.
func resolveLiveSetup(
	res *trendBTResult,
	highs, lows, closes, atr []float64,
	longSig, shortSig func(int) bool,
) {
	n := len(closes)
	if n < trendBTWarmup+2 {
		return
	}
	if res.LiveATR <= 0 {
		res.LiveATR = atr[n-1]
	}

	// latest signal bar
	sigIdx := -1
	sigSide := 0
	for i := n - 1; i >= trendBTWarmup; i-- {
		ago := n - 1 - i
		if ago > trendMaxHold {
			break
		}
		if longSig(i) {
			sigIdx, sigSide = i, 1
			break
		}
		if shortSig(i) {
			sigIdx, sigSide = i, -1
			break
		}
	}
	if sigIdx < 0 {
		return
	}

	// walk to earliest bar in contiguous same-side streak (freeze anchor)
	anchor := sigIdx
	for j := sigIdx - 1; j >= trendBTWarmup; j-- {
		if n-1-j > trendMaxHold {
			break
		}
		ok := false
		if sigSide > 0 {
			ok = longSig(j)
		} else {
			ok = shortSig(j)
		}
		if !ok {
			break
		}
		anchor = j
	}

	// invalidate if opposite signal after anchor
	for j := anchor + 1; j < n; j++ {
		if sigSide > 0 && shortSig(j) {
			return
		}
		if sigSide < 0 && longSig(j) {
			return
		}
	}

	pb := 0.005
	if sigSide > 0 && res.Long.MedianPullbackPct > 0 {
		pb = res.Long.MedianPullbackPct / 100
	}
	if sigSide < 0 && res.Short.MedianPullbackPct > 0 {
		pb = res.Short.MedianPullbackPct / 100
	}
	sigClose := closes[anchor]
	var entry float64
	if sigSide > 0 {
		entry = sigClose * (1 - pb)
	} else {
		entry = sigClose * (1 + pb)
	}

	touched := false
	for j := anchor + 1; j < n; j++ {
		if sigSide > 0 && lows[j] <= entry {
			touched = true
			break
		}
		if sigSide < 0 && highs[j] >= entry {
			touched = true
			break
		}
	}

	barsAgo := n - 1 - anchor
	res.LiveSide = sigSide
	res.LiveSignalClose = sigClose
	res.LiveATR = atr[anchor]
	if res.LiveATR <= 0 {
		res.LiveATR = atr[n-1]
	}
	res.LiveBarsAgo = barsAgo
	res.LiveTouched = touched
	if barsAgo > trendMaxHold {
		res.LiveExpired = true
		res.LiveSide = 0
	}
}

func sideEdgeOK(st trendSideStats) bool {
	return st.Trades >= trendMinTrades && st.Expectancy > 0 && st.ProfitFactor >= 1.0
}

// idealEntryFromBT anchors to signalClose only — never chases last.
// last is unused for the entry level (kept in signature for call-site clarity).
func idealEntryFromBT(_, signalClose, atr float64, side int, st trendSideStats) (entry, stop, tp float64) {
	base := signalClose
	if base <= 0 {
		return 0, 0, 0
	}
	pb := st.MedianPullbackPct / 100
	if pb <= 0 {
		pb = 0.005
	}
	if atr <= 0 {
		atr = base * 0.01
	}
	if side > 0 {
		entry = base * (1 - pb)
		stop = entry - trendStopATRMult*atr
		tp = entry + trendTPATRMult*atr
	} else {
		entry = base * (1 + pb)
		stop = entry + trendStopATRMult*atr
		tp = entry - trendTPATRMult*atr
	}
	return roundFloat(entry, 8), roundFloat(stop, 8), roundFloat(tp, 8)
}
