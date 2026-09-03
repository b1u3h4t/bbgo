package binance

import (
	"testing"

	"github.com/adshao/go-binance/v2/delivery"
	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/types"
)

func TestToGlobalDeliveryMarket(t *testing.T) {
	symbol := delivery.Symbol{
		Symbol:            "BTCUSD_PERP",
		Pair:              "BTCUSD",
		ContractType:      "PERPETUAL",
		ContractStatus:    "TRADING",
		ContractSize:      100,
		PricePrecision:    1,
		QuantityPrecision: 0,
		QuotePrecision:    8,
		BaseAsset:         "BTC",
		QuoteAsset:        "USD",
		MarginAsset:       "BTC",
		Filters: []map[string]any{
			{
				"filterType": "LOT_SIZE",
				"minQty":     "1",
				"maxQty":     "1000000",
				"stepSize":   "1",
			},
			{
				"filterType": "PRICE_FILTER",
				"minPrice":   "0.1",
				"maxPrice":   "1000000",
				"tickSize":   "0.1",
			},
		},
	}

	m := toGlobalDeliveryMarket(symbol)
	assert.Equal(t, "BTCUSD_PERP", m.Symbol)
	assert.Equal(t, "BTC", m.BaseCurrency)
	assert.Equal(t, "USD", m.QuoteCurrency)
	assert.Equal(t, 100.0, m.ContractValue.Float64())
	assert.Equal(t, 1.0, m.MinQuantity.Float64())
	assert.Equal(t, 0.1, m.TickSize.Float64())
}

func TestToGlobalDeliveryOrder(t *testing.T) {
	o := &delivery.Order{
		Symbol:           "BTCUSD_PERP",
		OrderID:          42,
		ClientOrderID:    "cid-1",
		Price:            "65000.0",
		OrigQuantity:     "10",
		ExecutedQuantity: "0",
		Status:           delivery.OrderStatusTypeNew,
		TimeInForce:      delivery.TimeInForceTypeGTC,
		Type:             delivery.OrderTypeLimit,
		Side:             delivery.SideTypeBuy,
		Time:             1_700_000_000_000,
		UpdateTime:       1_700_000_000_000,
	}

	g, err := toGlobalDeliveryOrder(o, false)
	assert.NoError(t, err)
	assert.Equal(t, uint64(42), g.OrderID)
	assert.Equal(t, types.SideTypeBuy, g.Side)
	assert.Equal(t, types.OrderTypeLimit, g.Type)
	assert.Equal(t, types.OrderStatusNew, g.Status)
	assert.True(t, g.IsFutures)
	assert.Equal(t, 10.0, g.Quantity.Float64())
	assert.Equal(t, 65000.0, g.Price.Float64())
}

func TestUseDeliverySettings(t *testing.T) {
	var s types.FuturesSettings
	s.UseDelivery()
	assert.True(t, s.IsFutures)
	assert.True(t, s.IsDelivery)
	assert.False(t, s.IsIsolatedFutures)

	s.UseFutures()
	assert.True(t, s.IsFutures)
	assert.False(t, s.IsDelivery)
}
