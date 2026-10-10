package grid2

import (
	"io"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

// Helpers are local so this file builds under both default and -tags dnum
// (newTestOrder / newTestStrategy live in strategy_test.go with //go:build !dnum).

func occupancyTestOrder(price float64, side types.SideType, typ types.OrderType) types.Order {
	return types.Order{
		SubmitOrder: types.SubmitOrder{
			Price: fixedpoint.NewFromFloat(price),
			Side:  side,
			Type:  typ,
		},
	}
}

func TestCollectOccupiedPinPrices(t *testing.T) {
	active := []types.Order{
		occupancyTestOrder(1900, types.SideTypeSell, types.OrderTypeLimit),
		occupancyTestOrder(1800, types.SideTypeBuy, types.OrderTypeStopMarket),
	}
	open := []types.Order{
		occupancyTestOrder(1700, types.SideTypeBuy, types.OrderTypeLimit),
		occupancyTestOrder(1900, types.SideTypeSell, types.OrderTypeLimit),
	}

	got := collectOccupiedPinPrices(active, open)
	assert.Len(t, got, 2)
	assert.Contains(t, got, fixedpoint.NewFromFloat(1900).String())
	assert.Contains(t, got, fixedpoint.NewFromFloat(1700).String())
	assert.NotContains(t, got, fixedpoint.NewFromFloat(1800).String(), "non-pin types ignored")
}

func TestFilterSubmitOrdersByOccupiedPins(t *testing.T) {
	orders := []types.SubmitOrder{
		{Side: types.SideTypeSell, Price: fixedpoint.NewFromFloat(1900), Quantity: fixedpoint.NewFromFloat(1)},
		{Side: types.SideTypeBuy, Price: fixedpoint.NewFromFloat(1800), Quantity: fixedpoint.NewFromFloat(1)},
		{Side: types.SideTypeSell, Price: fixedpoint.NewFromFloat(2000), Quantity: fixedpoint.NewFromFloat(1)},
	}
	occupied := map[string]struct{}{
		fixedpoint.NewFromFloat(1900).String(): {},
	}
	kept, skipped := filterSubmitOrdersByOccupiedPins(orders, occupied)
	assert.Equal(t, 1, skipped)
	assert.Len(t, kept, 2)
	assert.Equal(t, fixedpoint.NewFromFloat(1800), kept[0].Price)
	assert.Equal(t, fixedpoint.NewFromFloat(2000), kept[1].Price)
}

func TestDropOccupiedSubmitOrdersInBatchDedup(t *testing.T) {
	s := &Strategy{Symbol: "BTCUSDT", logger: newSilentLogger()}
	orders := []types.SubmitOrder{
		{Side: types.SideTypeSell, Price: fixedpoint.NewFromFloat(1900), Quantity: fixedpoint.NewFromFloat(1)},
		{Side: types.SideTypeSell, Price: fixedpoint.NewFromFloat(1900), Quantity: fixedpoint.NewFromFloat(1)},
		{Side: types.SideTypeBuy, Price: fixedpoint.NewFromFloat(1800), Quantity: fixedpoint.NewFromFloat(1)},
	}
	got := s.dropOccupiedSubmitOrders(nil, orders)
	assert.Len(t, got, 2)
	assert.Equal(t, fixedpoint.NewFromFloat(1900), got[0].Price)
	assert.Equal(t, fixedpoint.NewFromFloat(1800), got[1].Price)
}

func newSilentLogger() *logrus.Entry {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return logrus.NewEntry(l)
}
