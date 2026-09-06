package udbox

import (
	"testing"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
	"github.com/stretchr/testify/assert"
)

func k(h, l, c float64) types.KLine {
	return types.KLine{
		High:  fixedpoint.NewFromFloat(h),
		Low:   fixedpoint.NewFromFloat(l),
		Close: fixedpoint.NewFromFloat(c),
	}
}

func TestDetectBox(t *testing.T) {
	var ks []types.KLine
	for i := 0; i < 20; i++ {
		ks = append(ks, k(100.5, 99.5, 100))
	}
	box, ok := DetectBox(ks, 20, 0.005, 0.05)
	assert.True(t, ok)
	assert.InDelta(t, 100.5, box.Top, 1e-9)
	assert.InDelta(t, 99.5, box.Bottom, 1e-9)
	assert.True(t, box.BreakLong(100.6, 0))
	assert.False(t, box.BreakLong(100.4, 0))
	assert.True(t, box.BreakShort(99.4, 0))
}

func TestDetectBoxTooWide(t *testing.T) {
	var ks []types.KLine
	for i := 0; i < 10; i++ {
		ks = append(ks, k(120, 80, 100))
	}
	_, ok := DetectBox(ks, 10, 0.005, 0.05)
	assert.False(t, ok)
}

func TestZones(t *testing.T) {
	box := Box{Top: 100, Bottom: 90}
	assert.True(t, box.InLowerZone(91, 0.25)) // 90 + 2.5 = 92.5
	assert.False(t, box.InLowerZone(93, 0.25))
	assert.True(t, box.InUpperZone(98, 0.25))
	assert.False(t, box.InUpperZone(96, 0.25))
}

func TestVolatilityCompressing(t *testing.T) {
	var ks []types.KLine
	for i := 0; i < 10; i++ {
		ks = append(ks, k(110, 90, 100))
	}
	for i := 0; i < 10; i++ {
		ks = append(ks, k(101, 99, 100))
	}
	assert.True(t, VolatilityCompressing(ks, 10))
}

func TestVolatilityCompressing_Expanding(t *testing.T) {
	var ks []types.KLine
	for i := 0; i < 10; i++ {
		ks = append(ks, k(101, 99, 100))
	}
	for i := 0; i < 10; i++ {
		ks = append(ks, k(110, 90, 100))
	}
	assert.False(t, VolatilityCompressing(ks, 10))
}

func TestValidate_OK(t *testing.T) {
	s := &Strategy{
		Symbol:         "BTCUSDT",
		Interval:       types.Interval4h,
		BoxWindow:      20,
		MinBoxWidthPct: 0.015,
		MaxBoxWidthPct: 0.05,
		Quantity:       fixedpoint.NewFromFloat(0.05),
		EnableRange:    true,
		UseNestFilter:  true,
		NestInterval:   types.Interval1d,
	}
	assert.NoError(t, s.Validate())
}

func TestValidate_RejectsBadBox(t *testing.T) {
	s := &Strategy{
		Symbol:         "BTCUSDT",
		MinBoxWidthPct: 0.08,
		MaxBoxWidthPct: 0.02,
		Quantity:       fixedpoint.NewFromFloat(0.01),
	}
	err := s.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "minBoxWidthPct")
}

func TestValidate_RejectsSameNestInterval(t *testing.T) {
	s := &Strategy{
		Symbol:         "BTCUSDT",
		Interval:       types.Interval1d,
		MinBoxWidthPct: 0.015,
		MaxBoxWidthPct: 0.05,
		Quantity:       fixedpoint.NewFromFloat(0.01),
		UseNestFilter:  true,
		NestInterval:   types.Interval1d,
	}
	err := s.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nestInterval")
}

func TestValidate_RejectsMissingSymbol(t *testing.T) {
	s := &Strategy{Quantity: fixedpoint.NewFromFloat(0.01)}
	assert.Error(t, s.Validate())
}

