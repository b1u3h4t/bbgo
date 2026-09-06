package udbox

import (
	"math"

	"github.com/c9s/bbgo/pkg/types"
)

// Box is a consolidation range (箱体): top = resistance, bottom = support.
type Box struct {
	Top    float64
	Bottom float64
	Start  int // inclusive index in the kline buffer
	End    int // exclusive index
}

func (b Box) Mid() float64 {
	return (b.Top + b.Bottom) / 2
}

func (b Box) Width() float64 {
	return b.Top - b.Bottom
}

func (b Box) WidthPct() float64 {
	if b.Bottom <= 0 {
		return 0
	}
	return b.Width() / b.Bottom
}

func (b Box) Valid() bool {
	return b.Top > b.Bottom && b.Bottom > 0
}

// DetectBox builds a box from the last window closed klines.
// UD / Darvas idea: range of recent highs/lows while price consolidates.
// A box is accepted when width% is within [minWidthPct, maxWidthPct].
func DetectBox(klines []types.KLine, window int, minWidthPct, maxWidthPct float64) (Box, bool) {
	n := len(klines)
	if window < 3 || n < window {
		return Box{}, false
	}

	start := n - window
	hi := -math.MaxFloat64
	lo := math.MaxFloat64
	for i := start; i < n; i++ {
		if klines[i].High.Float64() > hi {
			hi = klines[i].High.Float64()
		}
		if klines[i].Low.Float64() < lo {
			lo = klines[i].Low.Float64()
		}
	}

	box := Box{Top: hi, Bottom: lo, Start: start, End: n}
	if !box.Valid() {
		return Box{}, false
	}
	w := box.WidthPct()
	if w < minWidthPct || w > maxWidthPct {
		return Box{}, false
	}
	return box, true
}

// IsInside returns true if price is strictly inside the box (not a breakout).
func (b Box) IsInside(price float64) bool {
	return price > b.Bottom && price < b.Top
}

// InLowerZone: near box bottom (range long entry). zonePct is fraction of box width, e.g. 0.25.
func (b Box) InLowerZone(price, zonePct float64) bool {
	if !b.Valid() || zonePct <= 0 {
		return false
	}
	return price <= b.Bottom+b.Width()*zonePct
}

// InUpperZone: near box top (range short entry / long take-profit).
func (b Box) InUpperZone(price, zonePct float64) bool {
	if !b.Valid() || zonePct <= 0 {
		return false
	}
	return price >= b.Top-b.Width()*zonePct
}

// BreakLong: close above top (向上突破箱顶 → 多信号)
func (b Box) BreakLong(close float64, bufferPct float64) bool {
	return close > b.Top*(1+bufferPct)
}

// BreakShort: close below bottom (向下跌破箱底 → 空信号)
func (b Box) BreakShort(close float64, bufferPct float64) bool {
	return close < b.Bottom*(1-bufferPct)
}

// VolatilityCompressing approximates UD "起涨点" precondition:
// recent range smaller than earlier range (波动收敛).
func VolatilityCompressing(klines []types.KLine, lookback int) bool {
	n := len(klines)
	if lookback < 4 || n < lookback*2 {
		return true // not enough data → don't block
	}
	recent := rangePct(klines[n-lookback:])
	prior := rangePct(klines[n-lookback*2 : n-lookback])
	if prior <= 0 {
		return true
	}
	return recent < prior
}

func rangePct(ks []types.KLine) float64 {
	if len(ks) == 0 {
		return 0
	}
	hi := -math.MaxFloat64
	lo := math.MaxFloat64
	for _, k := range ks {
		if k.High.Float64() > hi {
			hi = k.High.Float64()
		}
		if k.Low.Float64() < lo {
			lo = k.Low.Float64()
		}
	}
	if lo <= 0 {
		return 0
	}
	return (hi - lo) / lo
}
