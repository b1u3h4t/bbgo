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

func TestShouldPlaceGridOrder_WLDShortCover(t *testing.T) {
	market := types.Market{
		Symbol:        "WLDUSDT",
		BaseCurrency:  "WLD",
		QuoteCurrency: "USDT",
		MinQuantity:   fixedpoint.NewFromFloat(1),
		MinNotional:   fixedpoint.NewFromFloat(5),
		TickSize:      fixedpoint.NewFromFloat(0.0001),
		StepSize:      fixedpoint.NewFromFloat(1),
	}

	// WLD short ~-43098 @ 0.431; buy cover above avg must be blocked.
	s := &Strategy{
		Position: types.NewPositionFromMarket(market),
		FeeRate:  fixedpoint.NewFromFloat(0.0002), // 0.02% maker
	}
	_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(-43098))
	_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(0.431))

	assert.True(t, s.isClosingOrderSide(types.SideTypeBuy))
	assert.False(t, s.isClosingOrderSide(types.SideTypeSell))

	assert.False(t, s.shouldPlaceGridOrder(types.SideTypeBuy, fixedpoint.NewFromFloat(0.4393)),
		"buy above avg covers short at a loss")
	assert.True(t, s.shouldPlaceGridOrder(types.SideTypeBuy, fixedpoint.NewFromFloat(0.420)),
		"buy well below avg covers short at a profit after fee")
	assert.True(t, s.shouldPlaceGridOrder(types.SideTypeSell, fixedpoint.NewFromFloat(0.4393)),
		"sell while short opens/adds — not a close")

	// Thin edge: gross positive but fee eats it.
	// maxBuy ≈ avg/(1+fee) = 0.431/1.0002 ≈ 0.4309138
	assert.False(t, s.shouldPlaceGridOrder(types.SideTypeBuy, fixedpoint.NewFromFloat(0.43095)),
		"buy just below avg still loses after fee")
	assert.True(t, s.shouldPlaceGridOrder(types.SideTypeBuy, fixedpoint.NewFromFloat(0.4308)),
		"buy below fee-adjusted breakeven is ok")
}

func TestNetCloseProfitAfterFee_Long(t *testing.T) {
	market := types.Market{
		Symbol:        "BTCUSDT",
		BaseCurrency:  "BTC",
		QuoteCurrency: "USDT",
		TickSize:      fixedpoint.NewFromFloat(0.1),
		StepSize:      fixedpoint.NewFromFloat(0.001),
	}
	s := &Strategy{
		Position: types.NewPositionFromMarket(market),
		FeeRate:  fixedpoint.NewFromFloat(0.001), // 0.1% so edge cases are obvious
	}
	_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(1))
	_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(100))

	// Sell 100.05: gross +0.05, fee 0.10005 → net negative
	assert.False(t, s.isProfitableCloseAt(fixedpoint.NewFromFloat(100.05)))
	// Sell 100.2: gross +0.2, fee 0.1002 → net positive
	assert.True(t, s.isProfitableCloseAt(fixedpoint.NewFromFloat(100.2)))
}

func TestTwinPinReverseIsUnprofitableClose(t *testing.T) {
	market := types.Market{
		Symbol:        "WLDUSDT",
		BaseCurrency:  "WLD",
		QuoteCurrency: "USDT",
		TickSize:      fixedpoint.NewFromFloat(0.0001),
		StepSize:      fixedpoint.NewFromFloat(1),
	}
	s := &Strategy{
		Market:     market,
		Position:   types.NewPositionFromMarket(market),
		UpperPrice: fixedpoint.NewFromFloat(0.44),
		LowerPrice: fixedpoint.NewFromFloat(0.40),
		GridNum:    5,
	}
	_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(-1000))
	_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(0.42))
	s.grid = s.newGrid()

	// Twin key is sell pin; reverse buy is next lower (0.43). Cover above avg → loss.
	sellPin := fixedpoint.NewFromFloat(0.44)
	assert.True(t, s.twinPinReverseIsUnprofitableClose(sellPin))

	// Long: sell at pin below avg is unprofitable close
	_ = s.Position.ModifyBase(fixedpoint.NewFromFloat(1000))
	_ = s.Position.ModifyAverageCost(fixedpoint.NewFromFloat(0.431))
	assert.True(t, s.twinPinReverseIsUnprofitableClose(fixedpoint.NewFromFloat(0.42)))
	assert.False(t, s.twinPinReverseIsUnprofitableClose(fixedpoint.NewFromFloat(0.44)))
}
