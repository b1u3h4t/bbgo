package grid2

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

func TestFilterGridSubmitOrders_ActivationBoundsAndMaxOpen(t *testing.T) {
	s := &Strategy{
		logger:           logrus.WithField("test", true),
		LowerPrice:       number(90),
		UpperPrice:       number(110),
		ActivationBounds: number(0.01), // 1%
		MaxOpenOrders:    2,
	}

	last := number(100)
	orders := []types.SubmitOrder{
		{Price: number(95), Side: types.SideTypeBuy},     // 5% away — out
		{Price: number(99), Side: types.SideTypeBuy},     // 1% — in
		{Price: number(100.5), Side: types.SideTypeSell}, // nearest
		{Price: number(101), Side: types.SideTypeSell},   // 1% — in
		{Price: number(110), Side: types.SideTypeSell},   // 10% — out
	}

	got := s.filterGridSubmitOrders(orders, last)
	assert.Len(t, got, 2)
	assert.Equal(t, number(100.5), got[0].Price)
	assert.True(t, got[1].Price.Compare(number(99)) == 0 || got[1].Price.Compare(number(101)) == 0)
}

func TestFilterGridSubmitOrders_ActivationSkippedOutsideGrid(t *testing.T) {
	s := &Strategy{
		logger:           logrus.WithField("test", true),
		LowerPrice:       number(719),
		UpperPrice:       number(729),
		ActivationBounds: number(0.007),
		MaxOpenOrders:    3,
	}
	// last below grid: activation must not wipe all pins; maxOpenOrders still applies
	orders := []types.SubmitOrder{
		{Price: number(720)},
		{Price: number(722)},
		{Price: number(724)},
		{Price: number(726)},
		{Price: number(728)},
	}
	got := s.filterGridSubmitOrders(orders, number(686))
	assert.Len(t, got, 3)
	assert.Equal(t, number(720), got[0].Price)
}

func TestFilterGridSubmitOrders_Disabled(t *testing.T) {
	s := &Strategy{logger: logrus.WithField("test", true)}
	orders := []types.SubmitOrder{
		{Price: number(1)},
		{Price: number(2)},
		{Price: number(3)},
	}
	got := s.filterGridSubmitOrders(orders, number(2))
	assert.Len(t, got, 3)
}

func TestAutoBollinger_Defaults(t *testing.T) {
	a := &AutoBollinger{}
	a.defaults()
	assert.Equal(t, types.Interval15m, a.Interval)
	assert.Equal(t, 100, a.Window)
	assert.Equal(t, 2.0, a.BandWidth)
}

func TestStrategy_Validate_AutoBollingerAllowsMissingPrices(t *testing.T) {
	s := &Strategy{
		Symbol:          "BNBUSD_PERP",
		GridNum:         8,
		AutoBollinger:   &AutoBollinger{Interval: types.Interval15m, Window: 20, BandWidth: 2},
		SkipSpreadCheck: true,
	}
	s.QuantityOrAmount.Quantity = fixedpoint.NewFromInt(1)
	assert.NoError(t, s.Validate())
}
