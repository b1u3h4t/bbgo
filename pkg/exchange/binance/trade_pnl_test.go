package binance

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseExchangeRealizedPnL(t *testing.T) {
	assert.False(t, parseExchangeRealizedPnL("").Valid)
	assert.False(t, parseExchangeRealizedPnL("  ").Valid)
	assert.False(t, parseExchangeRealizedPnL("x").Valid)

	z := parseExchangeRealizedPnL("0")
	assert.True(t, z.Valid)
	assert.Equal(t, 0.0, z.Float64)

	n := parseExchangeRealizedPnL("-0.00655999")
	assert.True(t, n.Valid)
	assert.InDelta(t, -0.00655999, n.Float64, 1e-12)
}
