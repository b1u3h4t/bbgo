package grid2

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

func TestQuantumAllocator_ZoneAndIntensity(t *testing.T) {
	s := &Strategy{
		logger:  logrus.WithField("test", true),
		GridNum: 8,
		Market: types.Market{
			Symbol:         "BNBUSD_PERP",
			BaseCurrency:   "BNB",
			QuoteCurrency:  "USD",
			PricePrecision: 2,
			TickSize:       number(0.01),
		},
		QuantumAllocator: &QuantumAllocator{
			TargetPosition:     fixedpoint.Zero,
			MaxPositionAbs:     number(7),
			BaseMaxOpenOrders:  4,
			BaseGridValuePct:   number(0.08),
			MaxGridValuePct:    number(0.15),
			MaxDeviation:       number(0.05),
			LongOnlyThreshold:  number(0.2),
			ShortOnlyThreshold: number(0.2),
		},
	}
	s.QuantityOrAmount.Quantity = number(1)
	s.Position = types.NewPositionFromMarket(s.Market)
	s.QuantumAllocator.defaults()

	// flat → hedge, base intensity → maxOpen 4
	assert.Equal(t, QuantumZoneHedge, s.quantumZone(s.quantumDeviation()))
	assert.Equal(t, 4, s.quantumEffectiveMaxOpenOrders(s.quantumDeviation()))

	// underweight (need buys): position -3 / 7 ≈ -0.43 → long_only, high intensity
	s.Position.Base = number(-3)
	dev := s.quantumDeviation()
	assert.Equal(t, QuantumZoneLongOnly, s.quantumZone(dev))
	assert.Equal(t, s.QuantumAllocator.MaxGridValuePct, s.quantumGridValuePct(dev))
	n := s.quantumEffectiveMaxOpenOrders(dev)
	assert.Greater(t, n, 4)

	// overweight → short_only
	s.Position.Base = number(3)
	assert.Equal(t, QuantumZoneShortOnly, s.quantumZone(s.quantumDeviation()))
}

func TestQuantumAllocator_SideBiasLongOnly(t *testing.T) {
	s := &Strategy{
		logger:           logrus.WithField("test", true),
		MaxOpenOrders:    4,
		quantumZoneState: QuantumZoneLongOnly,
		QuantumAllocator: &QuantumAllocator{},
	}
	orders := []types.SubmitOrder{
		{Price: number(99), Side: types.SideTypeBuy},
		{Price: number(98), Side: types.SideTypeBuy},
		{Price: number(101), Side: types.SideTypeSell},
		{Price: number(105), Side: types.SideTypeSell},
		{Price: number(110), Side: types.SideTypeSell},
	}
	got := s.filterQuantumSideBias(orders, number(100))
	buys, sells := 0, 0
	for _, o := range got {
		if o.Side == types.SideTypeBuy {
			buys++
		} else {
			sells++
		}
	}
	assert.Equal(t, 2, buys)
	assert.Equal(t, 2, sells) // half of maxOpen=4
	// nearest sells kept
	for _, o := range got {
		if o.Side == types.SideTypeSell {
			assert.True(t, o.Price.Compare(number(105)) <= 0)
		}
	}
}

func TestQuantumAllocator_RangeSymmetricFlat(t *testing.T) {
	market := types.Market{
		Symbol:         "X",
		BaseCurrency:   "X",
		QuoteCurrency:  "USD",
		PricePrecision: 2,
		TickSize:       number(0.01),
	}
	s := &Strategy{
		logger: logrus.WithField("test", true),
		Market: market,
		QuantumAllocator: &QuantumAllocator{
			GridRange:      number(0.01),
			TpSlRatio:      number(0.8),
			TargetPosition: fixedpoint.Zero,
			MaxPositionAbs: number(10),
		},
		Position: types.NewPositionFromMarket(market),
	}
	s.QuantumAllocator.defaults()
	err := s.applyQuantumAllocatorRange(nil, number(100))
	assert.NoError(t, err)
	assert.InDelta(t, 99.0, s.LowerPrice.Float64(), 0.02)
	assert.InDelta(t, 101.0, s.UpperPrice.Float64(), 0.02)
}

func TestStrategy_Validate_QuantumRangeAllowsMissingPrices(t *testing.T) {
	s := &Strategy{
		Symbol:  "BNBUSD_PERP",
		GridNum: 8,
		QuantumAllocator: &QuantumAllocator{
			GridRange: number(0.007),
		},
		SkipSpreadCheck: true,
	}
	s.QuantityOrAmount.Quantity = number(1)
	assert.NoError(t, s.Validate())
}
