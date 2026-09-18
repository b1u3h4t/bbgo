package supertrend

import (
	"github.com/c9s/bbgo/pkg/indicator"
	"github.com/c9s/bbgo/pkg/types"
)

type DoubleDema struct {
	Interval types.Interval `json:"interval"`

	// FastDEMAWindow DEMA window for checking breakout
	FastDEMAWindow int `json:"fastDEMAWindow"`
	// SlowDEMAWindow DEMA window for checking breakout
	SlowDEMAWindow int `json:"slowDEMAWindow"`
	fastDEMA       *indicator.DEMA
	slowDEMA       *indicator.DEMA
}

// getDemaSignal returns the DEMA regime used to filter Supertrend noise.
// Up = close above both DEMAs; Down = close below both. DirectionNone means mixed/chop.
// (Previously this required an intra-bar crossover, which almost never aligned with
// Supertrend and produced empty backtests on already-trending markets.)
func (dd *DoubleDema) getDemaSignal(openPrice float64, closePrice float64) types.Direction {
	_ = openPrice
	var demaSignal types.Direction = types.DirectionNone
	fast := dd.fastDEMA.Last(0)
	slow := dd.slowDEMA.Last(0)

	if closePrice > fast && closePrice > slow {
		demaSignal = types.DirectionUp
	} else if closePrice < fast && closePrice < slow {
		demaSignal = types.DirectionDown
	}

	return demaSignal
}

// preloadDema preloads DEMA indicators
func (dd *DoubleDema) preloadDema(kLineStore *types.MarketDataStore) {
	if klines, ok := kLineStore.KLinesOfInterval(dd.fastDEMA.Interval); ok {
		for i := 0; i < len(*klines); i++ {
			dd.fastDEMA.Update((*klines)[i].GetClose().Float64())
		}
	}
	if klines, ok := kLineStore.KLinesOfInterval(dd.slowDEMA.Interval); ok {
		for i := 0; i < len(*klines); i++ {
			dd.slowDEMA.Update((*klines)[i].GetClose().Float64())
		}
	}
}

// newDoubleDema initializes double DEMA indicators
func newDoubleDema(kLineStore *types.MarketDataStore, interval types.Interval, fastDEMAWindow int, slowDEMAWindow int) *DoubleDema {
	dd := DoubleDema{Interval: interval, FastDEMAWindow: fastDEMAWindow, SlowDEMAWindow: slowDEMAWindow}

	// DEMA
	if dd.FastDEMAWindow == 0 {
		dd.FastDEMAWindow = 144
	}
	dd.fastDEMA = &indicator.DEMA{IntervalWindow: types.IntervalWindow{Interval: dd.Interval, Window: dd.FastDEMAWindow}}
	dd.fastDEMA.Bind(kLineStore)

	if dd.SlowDEMAWindow == 0 {
		dd.SlowDEMAWindow = 169
	}
	dd.slowDEMA = &indicator.DEMA{IntervalWindow: types.IntervalWindow{Interval: dd.Interval, Window: dd.SlowDEMAWindow}}
	dd.slowDEMA.Bind(kLineStore)

	dd.preloadDema(kLineStore)

	return &dd
}
