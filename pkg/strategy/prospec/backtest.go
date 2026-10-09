package prospec

import (
	"strconv"

	"github.com/c9s/bbgo/pkg/types"
)

// BTStats aggregates walk-forward results for 1-2-3 and 2B.
type BTStats struct {
	Symbols     int     `json:"symbols"`
	Trades123   int     `json:"trades123"`
	Wins123     int     `json:"wins123"`
	Losses123   int     `json:"losses123"`
	WinRate123  float64 `json:"winRate123"`
	AvgR123     float64 `json:"avgR123"`
	Trades2B    int     `json:"trades2B"`
	Wins2B      int     `json:"wins2B"`
	Losses2B    int     `json:"losses2B"`
	WinRate2B   float64 `json:"winRate2B"`
	AvgR2B      float64 `json:"avgR2B"`
	HorizonBars int     `json:"horizonBars"`
	Note        string  `json:"note"`
}

type btTrade struct {
	R      float64
	Win    bool
	Closed bool
}

// simulate from entry bar with stop/target; R = pnl/risk.
func simulateTrade(hist []types.KLine, entryIdx int, long bool, entry, stop, target float64, horizon int) btTrade {
	n := len(hist)
	if entryIdx >= n-1 || stop <= 0 || target <= 0 {
		return btTrade{}
	}
	risk := entry - stop
	if long {
		risk = entry - stop
	} else {
		risk = stop - entry
	}
	if risk <= 0 {
		return btTrade{}
	}
	end := entryIdx + horizon
	if end >= n {
		end = n - 1
	}
	for i := entryIdx + 1; i <= end; i++ {
		hi := hist[i].High.Float64()
		lo := hist[i].Low.Float64()
		if long {
			if lo <= stop {
				return btTrade{R: -1, Win: false, Closed: true}
			}
			if hi >= target {
				return btTrade{R: (target - entry) / risk, Win: true, Closed: true}
			}
		} else {
			if hi >= stop {
				return btTrade{R: -1, Win: false, Closed: true}
			}
			if lo <= target {
				return btTrade{R: (entry - target) / risk, Win: true, Closed: true}
			}
		}
	}
	// mark to last close
	last := hist[end].Close.Float64()
	var r float64
	if long {
		r = (last - entry) / risk
	} else {
		r = (entry - last) / risk
	}
	return btTrade{R: r, Win: r > 0, Closed: true}
}

// Backtest123 walks bars; on newly confirmed 1-2-3, enter at that close with 1R geometry.
func Backtest123(ks []types.KLine, look, horizon int) (trades int, wins int, losses int, sumR float64) {
	hist := closedHist(ks)
	n := len(hist)
	if n < 80 || horizon < 4 {
		return
	}
	if look < 2 {
		look = 3
	}
	var lastKey string
	for i := 50; i < n-horizon; i++ {
		o := DetectOneTwoThree(hist[:i+1], look)
		if !o.Confirmed || o.Direction == "none" {
			continue
		}
		// only fire on the bar that just printed stage-3 (or the next bar)
		t3 := o.Stage3Time
		barT := hist[i].StartTime.Time().UTC()
		prevT := hist[i].StartTime.Time().UTC()
		if i > 0 {
			prevT = hist[i-1].StartTime.Time().UTC()
		}
		if !(t3.Equal(barT) || t3.Equal(prevT)) {
			continue
		}
		key := o.Direction + ":" + strconv.FormatFloat(o.Stage3Price, 'f', 4, 64) + ":" + strconv.FormatFloat(o.Stage2Price, 'f', 4, 64)
		if key == lastKey {
			continue
		}
		lastKey = key

		entry := hist[i].Close.Float64()
		long := o.Direction == "to_bull"
		var stop, target float64
		if long {
			stop, target = riskGeometryLong(entry, o.Stage2Price, o.PriorExtreme)
		} else {
			stop, target = riskGeometryShort(entry, o.Stage2Price, o.PriorExtreme)
		}
		tr := simulateTrade(hist, i, long, entry, stop, target, horizon)
		if !tr.Closed {
			continue
		}
		trades++
		sumR += tr.R
		if tr.Win {
			wins++
		} else {
			losses++
		}
	}
	return
}

// Backtest2B enters on 2B bar close when barsAgo==0 in rolling detect.
func Backtest2B(ks []types.KLine, look, horizon int) (trades int, wins int, losses int, sumR float64) {
	hist := closedHist(ks)
	n := len(hist)
	if n < 80 || horizon < 4 {
		return
	}
	if look < 2 {
		look = 3
	}
	lastSig := -100
	for i := 40; i < n-horizon; i++ {
		tb := DetectTwoB(hist[:i+1], look, 8)
		if tb == nil || tb.BarsAgo != 0 {
			continue
		}
		if i-lastSig < 4 {
			continue
		}
		entry := hist[i].Close.Float64()
		long := tb.Side == "long"
		var stop, target float64
		if long {
			stop, target = riskGeometryLong(entry, tb.Pierced*0.998, tb.Level*0.995)
		} else {
			stop, target = riskGeometryShort(entry, tb.Pierced*1.002, tb.Level*1.005)
		}
		tr := simulateTrade(hist, i, long, entry, stop, target, horizon)
		if !tr.Closed {
			continue
		}
		trades++
		sumR += tr.R
		if tr.Win {
			wins++
		} else {
			losses++
		}
		lastSig = i
	}
	return
}

func SummarizeBT(symbols int, t123, w123, l123 int, sum123 float64, t2b, w2b, l2b int, sum2b float64, horizon int) BTStats {
	st := BTStats{
		Symbols: symbols, Trades123: t123, Wins123: w123, Losses123: l123,
		Trades2B: t2b, Wins2B: w2b, Losses2B: l2b, HorizonBars: horizon,
		Note: "进场=信号收盘；停损/目标=1R 几何；先触停损/目标平仓，否则持有至窗口末按收盘计 R",
	}
	if t123 > 0 {
		st.WinRate123 = round8(100 * float64(w123) / float64(t123))
		st.AvgR123 = round8(sum123 / float64(t123))
	}
	if t2b > 0 {
		st.WinRate2B = round8(100 * float64(w2b) / float64(t2b))
		st.AvgR2B = round8(sum2b / float64(t2b))
	}
	return st
}
