package grid2

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/types"
)

func TestCollectOccupiedPinPrices(t *testing.T) {
	active := []types.Order{
		newTestOrder(number(1900.0), number(0.1), types.SideTypeSell),
		func() types.Order {
			o := newTestOrder(number(1800.0), number(0.1), types.SideTypeBuy)
			o.Type = types.OrderTypeStopMarket
			return o
		}(),
	}
	open := []types.Order{
		newTestOrder(number(1700.0), number(0.1), types.SideTypeBuy),
		newTestOrder(number(1900.0), number(0.1), types.SideTypeSell), // same pin as active
	}

	got := collectOccupiedPinPrices(active, open)
	assert.Len(t, got, 2)
	assert.Contains(t, got, number(1900.0).String())
	assert.Contains(t, got, number(1700.0).String())
	assert.NotContains(t, got, number(1800.0).String(), "non-pin types ignored")
}

func TestFilterSubmitOrdersByOccupiedPins(t *testing.T) {
	orders := []types.SubmitOrder{
		{Side: types.SideTypeSell, Price: number(1900.0), Quantity: number(1)},
		{Side: types.SideTypeBuy, Price: number(1800.0), Quantity: number(1)},
		{Side: types.SideTypeSell, Price: number(2000.0), Quantity: number(1)},
	}
	occupied := map[string]struct{}{
		number(1900.0).String(): {},
	}
	kept, skipped := filterSubmitOrdersByOccupiedPins(orders, occupied)
	assert.Equal(t, 1, skipped)
	assert.Len(t, kept, 2)
	assert.Equal(t, number(1800.0), kept[0].Price)
	assert.Equal(t, number(2000.0), kept[1].Price)
}

func TestDropOccupiedSubmitOrdersInBatchDedup(t *testing.T) {
	s := newTestStrategy()
	orders := []types.SubmitOrder{
		{Side: types.SideTypeSell, Price: number(1900.0), Quantity: number(1)},
		{Side: types.SideTypeSell, Price: number(1900.0), Quantity: number(1)}, // in-batch dup
		{Side: types.SideTypeBuy, Price: number(1800.0), Quantity: number(1)},
	}
	got := s.dropOccupiedSubmitOrders(nil, orders)
	assert.Len(t, got, 2)
	assert.Equal(t, number(1900.0), got[0].Price)
	assert.Equal(t, number(1800.0), got[1].Price)
}
