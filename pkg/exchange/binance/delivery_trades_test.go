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
	// Compare fixedpoint values directly: Float64() is lossy under -tags dnum.
	assert.Equal(t, 0, trade.Fee.Compare(fixedpoint.MustNewFromString("0.0001")))
	assert.Equal(t, 0, trade.Quantity.Compare(fixedpoint.MustNewFromString("10")))
	// QuoteQuantity is price*contracts for linear avg-cost math (not contract USD notional).
	assert.Equal(t, 0, trade.QuoteQuantity.Compare(fixedpoint.MustNewFromString("1000000")))
	assert.Equal(t, 0, trade.Price.Compare(fixedpoint.MustNewFromString("100000")))
}
