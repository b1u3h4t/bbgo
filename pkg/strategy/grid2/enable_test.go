package grid2

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/types"
)

func TestStrategy_isEnabled(t *testing.T) {
	s := &Strategy{Symbol: "ENAUSDT"}
	assert.True(t, s.isEnabled(), "omitted enable defaults to true")

	off := false
	s.Enable = &off
	assert.False(t, s.isEnabled())

	on := true
	s.Enable = &on
	assert.True(t, s.isEnabled())
}

func TestIsGridPinOpenOrder(t *testing.T) {
	assert.True(t, isGridPinOpenOrder(types.Order{SubmitOrder: types.SubmitOrder{Type: types.OrderTypeLimit}}))
	assert.True(t, isGridPinOpenOrder(types.Order{SubmitOrder: types.SubmitOrder{Type: types.OrderTypeLimitMaker}}))
	assert.False(t, isGridPinOpenOrder(types.Order{SubmitOrder: types.SubmitOrder{Type: types.OrderTypeTakeProfitMarket}}))
	assert.False(t, isGridPinOpenOrder(types.Order{SubmitOrder: types.SubmitOrder{Type: types.OrderTypeStopMarket}}))
	assert.False(t, isGridPinOpenOrder(types.Order{SubmitOrder: types.SubmitOrder{Type: types.OrderTypeMarket}}))
}

func TestFilterGridPinOpenOrders(t *testing.T) {
	orders := []types.Order{
		{SubmitOrder: types.SubmitOrder{Type: types.OrderTypeLimit}},
		{SubmitOrder: types.SubmitOrder{Type: types.OrderTypeTakeProfitMarket}},
		{SubmitOrder: types.SubmitOrder{Type: types.OrderTypeLimitMaker}},
		{SubmitOrder: types.SubmitOrder{Type: types.OrderTypeStopMarket}},
	}
	got := filterGridPinOpenOrders(orders)
	assert.Len(t, got, 2)
	assert.Equal(t, types.OrderTypeLimit, got[0].Type)
	assert.Equal(t, types.OrderTypeLimitMaker, got[1].Type)
}
