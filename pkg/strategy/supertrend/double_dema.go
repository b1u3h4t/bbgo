package supertrend

import (
	"github.com/c9s/bbgo/pkg/bbgo"
	indicatorv2 "github.com/c9s/bbgo/pkg/indicator/v2"
	"github.com/c9s/bbgo/pkg/types"
)

type DoubleDema struct {
	Interval types.Interval `json:"interval"`

	// FastDEMAWindow DEMA window for checking breakout
	FastDEMAWindow int `json:"fastDEMAWindow"`
	// SlowDEMAWindow DEMA window for checking breakout
	SlowDEMAWindow int `json:"slowDEMAWindow"`
	fastDEMA       *indicatorv2.DEMAStream
	slowDEMA       *indicatorv2.DEMAStream
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

// newDoubleDema initializes double DEMA indicators on the close prices of the given interval,
// the kline history of the market data store is loaded by the indicator set.
func newDoubleDema(indicators *bbgo.IndicatorSet, interval types.Interval, fastDEMAWindow int, slowDEMAWindow int) *DoubleDema {
	dd := DoubleDema{Interval: interval, FastDEMAWindow: fastDEMAWindow, SlowDEMAWindow: slowDEMAWindow}

	// DEMA
	if dd.FastDEMAWindow == 0 {
		dd.FastDEMAWindow = 144
	}
	dd.fastDEMA = indicators.DEMA(types.IntervalWindow{Interval: dd.Interval, Window: dd.FastDEMAWindow})

	if dd.SlowDEMAWindow == 0 {
		dd.SlowDEMAWindow = 169
	}
	dd.slowDEMA = indicators.DEMA(types.IntervalWindow{Interval: dd.Interval, Window: dd.SlowDEMAWindow})

	return &dd
}
