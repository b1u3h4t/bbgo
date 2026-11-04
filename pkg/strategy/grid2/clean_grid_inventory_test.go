//go:build !dnum

package grid2

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

func hypeMarket() types.Market {
	return types.Market{
		Symbol:        "HYPEUSDT",
		BaseCurrency:  "HYPE",
		QuoteCurrency: "USDT",
		MinQuantity:   fixedpoint.NewFromFloat(0.01),
		MinNotional:   fixedpoint.NewFromFloat(5),
		TickSize:      fixedpoint.NewFromFloat(0.001),
		StepSize:      fixedpoint.NewFromFloat(0.01),
	}
}

func hypeCleanStrategy() *Strategy {
	return &Strategy{
		Position:   types.NewPositionFromMarket(hypeMarket()),
		FeeRate:    fixedpoint.NewFromFloat(0.0002),
		LowerPrice: fixedpoint.NewFromFloat(90),
		UpperPrice: fixedpoint.NewFromFloat(97),
		GridNum:    12,
		QuantityOrAmount: bbgo.QuantityOrAmount{
			Quantity: fixedpoint.NewFromFloat(21.3),
		},
	}
}

// maxBase for HYPE defaults: 21.3 * 11 * 2 = 468.6
func TestIsCleanGridInventory_Table(t *testing.T) {
	type fields struct {
		base, avg, lower, upper, qty float64
		gridNum                      int64
		nilPosition                  bool
		omitLower, omitUpper         bool
		omitQty                      bool
	}
	cases := []struct {
		name  string
		f     fields
		clean bool
		why   string
	}{
		{
			name:  "nil position is clean",
			f:     fields{nilPosition: true},
			clean: true,
			why:   "no inventory to gate",
		},
		{
			name:  "flat zero base is clean",
			f:     fields{base: 0, avg: 0, lower: 90, upper: 97, qty: 21.3, gridNum: 12},
			clean: true,
			why:   "flat book",
		},
		{
			name:  "missing lowerPrice is dirty",
			f:     fields{base: 21.3, avg: 93, upper: 97, qty: 21.3, gridNum: 12, omitLower: true},
			clean: false,
			why:   "cannot verify band without lower",
		},
		{
			name:  "missing upperPrice is dirty",
			f:     fields{base: 21.3, avg: 93, lower: 90, qty: 21.3, gridNum: 12, omitUpper: true},
			clean: false,
			why:   "cannot verify band without upper",
		},
		{
			name:  "gridNum zero is dirty",
			f:     fields{base: 21.3, avg: 93, lower: 90, upper: 97, qty: 21.3, gridNum: 0},
			clean: false,
			why:   "cannot compute capacity",
		},
		{
			name:  "quantity unset is dirty",
			f:     fields{base: 21.3, avg: 93, lower: 90, upper: 97, gridNum: 12, omitQty: true},
			clean: false,
			why:   "cannot compute capacity without qty",
		},
		{
			name:  "avgCost below lower is dirty",
			f:     fields{base: 21.3, avg: 89.999, lower: 90, upper: 97, qty: 21.3, gridNum: 12},
			clean: false,
			why:   "foreign/manual inventory below band",
		},
		{
			name:  "avgCost above upper is dirty",
			f:     fields{base: 21.3, avg: 97.001, lower: 90, upper: 97, qty: 21.3, gridNum: 12},
			clean: false,
			why:   "foreign/manual inventory above band",
		},
		{
			name:  "avgCost exactly at lower is clean",
			f:     fields{base: 21.3, avg: 90, lower: 90, upper: 97, qty: 21.3, gridNum: 12},
			clean: true,
			why:   "band edges inclusive",
		},
		{
			name:  "avgCost exactly at upper is clean",
			f:     fields{base: 21.3, avg: 97, lower: 90, upper: 97, qty: 21.3, gridNum: 12},
			clean: true,
			why:   "band edges inclusive",
		},
		{
			name:  "single pin long inside band is clean",
			f:     fields{base: 21.3, avg: 92.545, lower: 90, upper: 97, qty: 21.3, gridNum: 12},
			clean: true,
			why:   "normal one-fill inventory",
		},
		{
			name:  "multi pin long HYPE walk-down is clean",
			f:     fields{base: 64.32, avg: 91.91559, lower: 90, upper: 97, qty: 21.3, gridNum: 12},
			clean: true,
			why:   "stacked buys still within one grid capacity",
		},
		{
			name:  "short inventory inside band is clean",
			f:     fields{base: -42.6, avg: 93.818, lower: 90, upper: 97, qty: 21.3, gridNum: 12},
			clean: true,
			why:   "abs(base) used for capacity",
		},
		{
			name:  "base exactly at max capacity is clean",
			// max = 21.3 * 11 * 2 = 468.6
			f:     fields{base: 468.6, avg: 93, lower: 90, upper: 97, qty: 21.3, gridNum: 12},
			clean: true,
			why:   "Compare > max only; equal allowed",
		},
		{
			name:  "base just over max capacity is dirty",
			f:     fields{base: 468.61, avg: 93, lower: 90, upper: 97, qty: 21.3, gridNum: 12},
			clean: false,
			why:   "larger than one grid can explain → stack/overlay",
		},
		{
			name:  "huge restart overlay is dirty",
			f:     fields{base: 2000, avg: 93, lower: 90, upper: 97, qty: 21.3, gridNum: 12},
			clean: false,
			why:   "multi-grid or leftover position",
		},
		{
			name:  "gridNum=1 uses maxPins=1",
			// max = 10 * 1 * 2 = 20
			f:     fields{base: 20, avg: 93, lower: 90, upper: 97, qty: 10, gridNum: 1},
			clean: true,
			why:   "max(gridNumber-1,1)=1",
		},
		{
			name:  "gridNum=1 over capacity is dirty",
			f:     fields{base: 20.01, avg: 93, lower: 90, upper: 97, qty: 10, gridNum: 1},
			clean: false,
			why:   "exceeds 1-pin capacity with compound slack",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s *Strategy
			if tc.f.nilPosition {
				s = &Strategy{}
			} else {
				s = &Strategy{
					Position: types.NewPositionFromMarket(hypeMarket()),
					FeeRate:  fixedpoint.NewFromFloat(0.0002),
					GridNum:  tc.f.gridNum,
				}
				if !tc.f.omitLower {
					s.LowerPrice = fixedpoint.NewFromFloat(tc.f.lower)
				}
				if !tc.f.omitUpper {
					s.UpperPrice = fixedpoint.NewFromFloat(tc.f.upper)
				}
				if !tc.f.omitQty {
					s.QuantityOrAmount = bbgo.QuantityOrAmount{
						Quantity: fixedpoint.NewFromFloat(tc.f.qty),
					}
				}
				if tc.f.base != 0 {
					_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(tc.f.base))
				}
				if tc.f.avg != 0 {
					_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(tc.f.avg))
				}
			}
			got := s.isCleanGridInventory()
			assert.Equal(t, tc.clean, got, tc.why)
		})
	}
}

func TestIsProfitablePinRoundTrip_Table(t *testing.T) {
	s := hypeCleanStrategy()
	// feeRate 0.02% → round-trip 0.04%
	cases := []struct {
		name    string
		entry   float64
		exit    float64
		side    types.SideType
		want    bool
	}{
		{"long twin above entry", 91.272, 91.909, types.SideTypeSell, true},
		{"long twin equal entry", 91.272, 91.272, types.SideTypeSell, false},
		{"long twin below entry", 91.272, 91.000, types.SideTypeSell, false},
		{"long thin edge eaten by 2x fee", 100, 100.03, types.SideTypeSell, false}, // +0.03% < 0.04%
		{"long just clears 2x fee", 100, 100.05, types.SideTypeSell, true},          // +0.05% > 0.04%
		{"short cover below entry", 93.818, 93.181, types.SideTypeBuy, true},
		{"short cover above entry", 93.818, 94.000, types.SideTypeBuy, false},
		{"zero entry rejected", 0, 91.909, types.SideTypeSell, false},
		{"zero exit rejected", 91.272, 0, types.SideTypeSell, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := s.isProfitablePinRoundTrip(
				fixedpoint.NewFromFloat(tc.entry),
				fixedpoint.NewFromFloat(tc.exit),
				tc.side,
			)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestShouldPlaceGridOrderWithEntry_CleanVsDirty(t *testing.T) {
	t.Run("clean stacked long uses pin not avgCost", func(t *testing.T) {
		s := hypeCleanStrategy()
		_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(64.32))
		_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(91.91559))
		assert.True(t, s.isCleanGridInventory())

		// avgCost gate alone would reject sell @ 91.909 (< 91.91559 after fee)
		assert.False(t, s.isProfitableCloseAt(fixedpoint.NewFromFloat(91.909)),
			"precondition: avgCost gate blocks")

		assert.True(t, s.shouldPlaceGridOrderWithEntry(
			types.SideTypeSell,
			fixedpoint.NewFromFloat(91.909),
			fixedpoint.NewFromFloat(91.272),
		), "clean+entry → pin gate allows")
	})

	t.Run("clean without entry allows closing pin", func(t *testing.T) {
		s := hypeCleanStrategy()
		_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(64.32))
		_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(91.91559))
		assert.True(t, s.shouldPlaceGridOrder(types.SideTypeSell, fixedpoint.NewFromFloat(91.909)),
			"clean bulk place/filter must not avgCost-block")
	})

	t.Run("dirty outside-band falls back to avgCost", func(t *testing.T) {
		s := hypeCleanStrategy()
		_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(21.3))
		_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(98))
		assert.False(t, s.isCleanGridInventory())

		assert.True(t, s.isProfitablePinRoundTrip(
			fixedpoint.NewFromFloat(91.272),
			fixedpoint.NewFromFloat(91.909),
			types.SideTypeSell,
		))
		assert.False(t, s.shouldPlaceGridOrderWithEntry(
			types.SideTypeSell,
			fixedpoint.NewFromFloat(91.909),
			fixedpoint.NewFromFloat(91.272),
		), "dirty ignores pin win when avgCost loses")
	})

	t.Run("dirty oversized base uses avgCost", func(t *testing.T) {
		s := hypeCleanStrategy()
		_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(500)) // > 468.6
		_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(93))
		assert.False(t, s.isCleanGridInventory())

		// sell well above avg → avgCost allows
		assert.True(t, s.shouldPlaceGridOrderWithEntry(
			types.SideTypeSell,
			fixedpoint.NewFromFloat(95),
			fixedpoint.NewFromFloat(91.272),
		))
		// sell below avg → avgCost blocks even if pin entry was lower
		assert.False(t, s.shouldPlaceGridOrderWithEntry(
			types.SideTypeSell,
			fixedpoint.NewFromFloat(92.5),
			fixedpoint.NewFromFloat(91.272),
		))
	})

	t.Run("opening side always allowed", func(t *testing.T) {
		s := hypeCleanStrategy()
		_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(21.3))
		_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(92))
		// buy while long is not closing
		assert.True(t, s.shouldPlaceGridOrderWithEntry(
			types.SideTypeBuy,
			fixedpoint.NewFromFloat(90),
			fixedpoint.NewFromFloat(91),
		))
	})

	t.Run("no band config is dirty like WLD legacy", func(t *testing.T) {
		s := &Strategy{
			Position: types.NewPositionFromMarket(hypeMarket()),
			FeeRate:  fixedpoint.NewFromFloat(0.0002),
			QuantityOrAmount: bbgo.QuantityOrAmount{
				Quantity: fixedpoint.NewFromFloat(21.3),
			},
			GridNum: 12,
			// no Lower/Upper
		}
		_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(-21.3))
		_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(93))
		assert.False(t, s.isCleanGridInventory())
		assert.False(t, s.shouldPlaceGridOrder(types.SideTypeBuy, fixedpoint.NewFromFloat(94)),
			"cover above avg blocked by avgCost")
		assert.True(t, s.shouldPlaceGridOrder(types.SideTypeBuy, fixedpoint.NewFromFloat(92)),
			"cover below avg allowed by avgCost")
	})
}

func TestCleanGridMaxBaseFormula(t *testing.T) {
	s := hypeCleanStrategy()
	// Document the formula used in production comments.
	qty := 21.3
	gridNum := int64(12)
	maxPins := gridNum - 1
	maxBase := qty * float64(maxPins) * 2.0
	assert.InDelta(t, 468.6, maxBase, 1e-9)

	_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(93))
	_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(maxBase))
	assert.True(t, s.isCleanGridInventory())
	_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(maxBase + 0.01))
	assert.False(t, s.isCleanGridInventory())
}
