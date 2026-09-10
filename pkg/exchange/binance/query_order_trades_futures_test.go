package binance

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/types"
)

// Regression: QueryOrderTrades used to fall through to spot /api/v3/myTrades for
// USDT-M sessions, which returns -1121 Invalid symbol for futures-only pairs
// such as HYPEUSDT (grid2 recover / fee pull path). Futures must go through
// queryFuturesOrderTrades → GET /fapi/v1/userTrades.
func TestQueryOrderTrades_FuturesSessionFlags(t *testing.T) {
	ex := &Exchange{}
	ex.FuturesSettings.IsFutures = true

	assert.True(t, ex.IsFutures)
	assert.False(t, ex.IsDelivery)
	assert.False(t, ex.IsMargin)

	q := types.OrderQuery{Symbol: "HYPEUSDT", OrderID: "12875535797"}
	_, err := strconv.ParseInt(q.OrderID, 10, 64)
	assert.NoError(t, err)
	assert.Equal(t, "HYPEUSDT", q.Symbol)
}
