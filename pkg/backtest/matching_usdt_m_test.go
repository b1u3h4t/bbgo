package backtest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

func getUSDTMTestMarket() types.Market {
	return types.Market{
		Symbol:          "BTCUSDT",
		PricePrecision:  2,
		VolumePrecision: 3,
		QuoteCurrency:   "USDT",
		BaseCurrency:    "BTC",
		MinNotional:     fixedpoint.NewFromFloat(5),
		MinAmount:       fixedpoint.NewFromFloat(5),
		MinQuantity:     fixedpoint.NewFromFloat(0.001),
		StepSize:        fixedpoint.NewFromFloat(0.001),
		TickSize:        fixedpoint.NewFromFloat(0.01),
	}
}

func getUSDTMTestAccount() *types.Account {
	account := &types.Account{
		MakerFeeRate: fixedpoint.NewFromFloat(0.0002),
		TakerFeeRate: fixedpoint.NewFromFloat(0.0005),
	}
	account.UpdateBalances(types.BalanceMap{
		"USDT": {Currency: "USDT", Available: fixedpoint.NewFromFloat(10_000)},
	})
	return account
}

func newUSDTMMatching(account *types.Account, market types.Market, lastPrice float64, leverage int64) *SimplePriceMatching {
	return &SimplePriceMatching{
		account:      account,
		Market:       market,
		Symbol:       market.Symbol,
		closedOrders: make(map[uint64]types.Order),
		lastPrice:    fixedpoint.NewFromFloat(lastPrice),
		currentTime:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Leverage:     fixedpoint.NewFromInt(leverage),
		futures:      true,
	}
}

func TestSimplePriceMatching_USDTM_ShortOpenWithoutBase(t *testing.T) {
	account := getUSDTMTestAccount()
	market := getUSDTMTestMarket()
	engine := newUSDTMMatching(account, market, 70_000, 2)

	order, trade, err := engine.PlaceOrder(types.SubmitOrder{
		Symbol:   "BTCUSDT",
		Side:     types.SideTypeSell,
		Type:     types.OrderTypeMarket,
		Quantity: fixedpoint.NewFromFloat(0.05),
	})
	assert.NoError(t, err)
	assert.NotNil(t, order)
	assert.NotNil(t, trade)
	assert.True(t, trade.IsFutures)
	assert.Equal(t, "USDT", trade.FeeCurrency)

	// No BTC inventory required / credited
	_, hasBTC := account.Balance("BTC")
	assert.False(t, hasBTC)

	assert.True(t, engine.usdtMPosition.Sign() < 0)
	assert.Equal(t, fixedpoint.NewFromFloat(0.05), engine.usdtMPosition.Abs())

	bal, ok := account.Balance("USDT")
	assert.True(t, ok)
	assert.True(t, bal.Locked.IsZero())
	// only fee deducted from USDT (margin unlocked after fill)
	assert.True(t, bal.Available.Compare(fixedpoint.NewFromFloat(10_000)) < 0)
}

func TestSimplePriceMatching_USDTM_LongThenCloseNoDust(t *testing.T) {
	account := getUSDTMTestAccount()
	market := getUSDTMTestMarket()
	engine := newUSDTMMatching(account, market, 70_000, 2)

	_, _, err := engine.PlaceOrder(types.SubmitOrder{
		Symbol:   "BTCUSDT",
		Side:     types.SideTypeBuy,
		Type:     types.OrderTypeMarket,
		Quantity: fixedpoint.NewFromFloat(0.05),
	})
	assert.NoError(t, err)
	assert.Equal(t, fixedpoint.NewFromFloat(0.05), engine.usdtMPosition)

	engine.lastPrice = fixedpoint.NewFromFloat(71_000)
	_, trade, err := engine.PlaceOrder(types.SubmitOrder{
		Symbol:   "BTCUSDT",
		Side:     types.SideTypeSell,
		Type:     types.OrderTypeMarket,
		Quantity: fixedpoint.NewFromFloat(0.05),
	})
	assert.NoError(t, err)
	assert.NotNil(t, trade)
	assert.True(t, engine.usdtMPosition.IsZero())

	_, hasBTC := account.Balance("BTC")
	assert.False(t, hasBTC)

	bal, _ := account.Balance("USDT")
	// realized ~50 USDT minus fees
	assert.True(t, bal.Available.Compare(fixedpoint.NewFromFloat(10_000)) > 0)
}

func TestSimplePriceMatching_USDTM_SellLocksQuoteMarginNotBase(t *testing.T) {
	account := getUSDTMTestAccount()
	market := getUSDTMTestMarket()
	engine := newUSDTMMatching(account, market, 70_000, 5)

	_, _, err := engine.PlaceOrder(types.SubmitOrder{
		Symbol:   "BTCUSDT",
		Side:     types.SideTypeSell,
		Type:     types.OrderTypeLimit,
		Price:    fixedpoint.NewFromFloat(71_000),
		Quantity: fixedpoint.NewFromFloat(0.1),
	})
	assert.NoError(t, err)

	bal, ok := account.Balance("USDT")
	assert.True(t, ok)
	expected := engine.usdtMInitialMargin(fixedpoint.NewFromFloat(0.1), fixedpoint.NewFromFloat(71_000))
	assert.Equal(t, expected, bal.Locked)
	_, hasBTC := account.Balance("BTC")
	assert.False(t, hasBTC)
}
