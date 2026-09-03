package binance

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/fixedpoint"
)

func TestToGlobalDeliveryAccountTrade(t *testing.T) {
	trade, err := toGlobalDeliveryAccountTrade(deliveryAccountTrade{
		Buyer:           true,
		Commission:      "0.0001",
		CommissionAsset: "BTC",
		ID:              42,
		Maker:           true,
		OrderID:         1001,
		Price:           "100000",
		Quantity:        "10",
		BaseQuantity:    "0.01",
		Symbol:          "BTCUSD_PERP",
		Time:            1_700_000_000_000,
	}, false)
	assert.NoError(t, err)
	assert.Equal(t, uint64(42), trade.ID)
	assert.Equal(t, uint64(1001), trade.OrderID)
	assert.Equal(t, "BTCUSD_PERP", trade.Symbol)
	assert.True(t, trade.IsFutures)
	assert.True(t, trade.IsBuyer)
	assert.True(t, trade.IsMaker)
	assert.Equal(t, "BTC", trade.FeeCurrency)
	assert.Equal(t, 0.0001, trade.Fee.Float64())
	assert.Equal(t, 10.0, trade.Quantity.Float64())
	// QuoteQuantity keeps USD notional = price * contracts
	assert.Equal(t, 1_000_000.0, trade.QuoteQuantity.Float64())
	assert.Equal(t, fixedpoint.NewFromFloat(100000), trade.Price)
}
