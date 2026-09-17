//go:build !dnum

package grid2

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

func TestCanTakeProfitClose(t *testing.T) {
	market := types.Market{
		Symbol:      "NEARUSDT",
		BaseCurrency: "NEAR",
		QuoteCurrency: "USDT",
		MinQuantity: fixedpoint.NewFromFloat(0.1),
		MinNotional: fixedpoint.NewFromFloat(5),
		TickSize:    fixedpoint.NewFromFloat(0.001),
		StepSize:    fixedpoint.NewFromFloat(0.1),
	}

	t.Run("nil position allows close", func(t *testing.T) {
		s := &Strategy{}
		assert.True(t, s.canTakeProfitClose(fixedpoint.NewFromFloat(2.7)))
	})

	t.Run("flat position allows close", func(t *testing.T) {
		s := &Strategy{Position: types.NewPositionFromMarket(market)}
		assert.True(t, s.canTakeProfitClose(fixedpoint.NewFromFloat(2.7)))
	})

	t.Run("profitable long allows close", func(t *testing.T) {
		s := &Strategy{Position: types.NewPositionFromMarket(market)}
		_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(100))
		_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(2.5))
		assert.True(t, s.canTakeProfitClose(fixedpoint.NewFromFloat(2.7)))
	})

	t.Run("losing long blocks close", func(t *testing.T) {
		s := &Strategy{Position: types.NewPositionFromMarket(market)}
		_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(100))
		_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(2.8))
		assert.False(t, s.canTakeProfitClose(fixedpoint.NewFromFloat(2.7)))
	})

	t.Run("losing short blocks close (NEAR TP case)", func(t *testing.T) {
		// Short opened ~2.56, mark 2.72 while takeProfitPrice 2.699 hit — must NOT close.
		s := &Strategy{Position: types.NewPositionFromMarket(market)}
		_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(-5134))
		_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(2.5598))
		assert.False(t, s.canTakeProfitClose(fixedpoint.NewFromFloat(2.72)))
	})

	t.Run("profitable short allows close", func(t *testing.T) {
		s := &Strategy{Position: types.NewPositionFromMarket(market)}
		_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(-100))
		_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(2.8))
		assert.True(t, s.canTakeProfitClose(fixedpoint.NewFromFloat(2.5)))
	})
}
