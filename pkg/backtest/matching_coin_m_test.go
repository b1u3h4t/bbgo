package backtest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

func getCoinMTestMarket() types.Market {
	return types.Market{
		Symbol:          "BTCUSD_PERP",
		PricePrecision:  1,
		VolumePrecision: 0,
		QuoteCurrency:   "USD",
		BaseCurrency:    "BTC",
		MinNotional:     fixedpoint.NewFromInt(100),
		MinAmount:       fixedpoint.NewFromInt(100),
		MinQuantity:     fixedpoint.NewFromInt(1),
		StepSize:        fixedpoint.NewFromInt(1),
		TickSize:        fixedpoint.NewFromFloat(0.1),
		ContractValue:   fixedpoint.NewFromInt(100),
	}
}

func getCoinMTestAccount() *types.Account {
	account := &types.Account{
		MakerFeeRate: fixedpoint.NewFromFloat(0.0002),
		TakerFeeRate: fixedpoint.NewFromFloat(0.0005),
	}
	account.UpdateBalances(types.BalanceMap{
		"BTC": {Currency: "BTC", Available: fixedpoint.NewFromFloat(1.0)},
	})
	return account
}

func newCoinMMatching(account *types.Account, market types.Market, lastPrice float64, leverage int64) *SimplePriceMatching {
	return &SimplePriceMatching{
		account:      account,
		Market:       market,
		Symbol:       market.Symbol,
		closedOrders: make(map[uint64]types.Order),
		lastPrice:    fixedpoint.NewFromFloat(lastPrice),
		currentTime:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Leverage:     fixedpoint.NewFromInt(leverage),
	}
}

func TestSimplePriceMatching_CoinM_PlaceOrderLocksBaseMargin(t *testing.T) {
	account := getCoinMTestAccount()
	market := getCoinMTestMarket()
	engine := newCoinMMatching(account, market, 100_000, 5)

	// Maker buy below last price — lock margin, do not fill yet
	_, _, err := engine.PlaceOrder(newLimitOrder("BTCUSD_PERP", types.SideTypeBuy, 99_000, 10))
	assert.NoError(t, err)

	bal, ok := account.Balance("BTC")
	assert.True(t, ok)
	expectedMargin := engine.coinMInitialMargin(fixedpoint.NewFromInt(10), fixedpoint.NewFromInt(99_000))
	assert.Equal(t, expectedMargin, bal.Locked)
	assert.Equal(t, fixedpoint.NewFromFloat(1.0).Sub(expectedMargin), bal.Available)

	// No USD needed
	_, hasUSD := account.Balance("USD")
	assert.False(t, hasUSD)
}

func TestSimplePriceMatching_CoinM_SellAlsoLocksBaseMargin(t *testing.T) {
	account := getCoinMTestAccount()
	market := getCoinMTestMarket()
	engine := newCoinMMatching(account, market, 100_000, 5)

	_, _, err := engine.PlaceOrder(newLimitOrder("BTCUSD_PERP", types.SideTypeSell, 101_000, 10))
	assert.NoError(t, err)

	bal, ok := account.Balance("BTC")
	assert.True(t, ok)
	expectedMargin := engine.coinMInitialMargin(fixedpoint.NewFromInt(10), fixedpoint.NewFromInt(101_000))
	assert.Equal(t, expectedMargin, bal.Locked)
}

func TestSimplePriceMatching_CoinM_CancelUnlocksMargin(t *testing.T) {
	account := getCoinMTestAccount()
	market := getCoinMTestMarket()
	engine := newCoinMMatching(account, market, 100_000, 5)

	order, _, err := engine.PlaceOrder(newLimitOrder("BTCUSD_PERP", types.SideTypeBuy, 99_000, 10))
	assert.NoError(t, err)
	assert.NotNil(t, order)

	_, err = engine.CancelOrder(*order)
	assert.NoError(t, err)

	bal, ok := account.Balance("BTC")
	assert.True(t, ok)
	assert.True(t, bal.Locked.IsZero())
	assert.InDelta(t, 1.0, bal.Available.Float64(), 1e-12)
}

func TestSimplePriceMatching_CoinM_FillDoesNotMintBase(t *testing.T) {
	account := getCoinMTestAccount()
	market := getCoinMTestMarket()
	engine := newCoinMMatching(account, market, 100_000, 5)

	var lastTrade types.Trade
	engine.OnTradeUpdate(func(trade types.Trade) {
		lastTrade = trade
	})

	_, _, err := engine.PlaceOrder(newLimitOrder("BTCUSD_PERP", types.SideTypeBuy, 99_000, 10))
	assert.NoError(t, err)

	btcBeforeBal, ok := account.Balance("BTC")
	assert.True(t, ok)
	btcBefore := btcBeforeBal.Total().Float64()

	t2 := engine.currentTime.Add(time.Minute)
	// price dips to 99_000 to fill the buy
	k := newKLine("BTCUSD_PERP", types.Interval1m, t2, 100_000, 100_500, 98_500, 99_500)
	engine.processKLine(k)

	assert.Equal(t, types.SideTypeBuy, lastTrade.Side)
	assert.Equal(t, 10.0, lastTrade.Quantity.Float64())
	assert.Equal(t, 99_000.0*10, lastTrade.QuoteQuantity.Float64())
	assert.Equal(t, "BTC", lastTrade.FeeCurrency)
	assert.True(t, lastTrade.IsFutures)
	assert.True(t, lastTrade.Fee.Sign() > 0)

	btcAfter, ok := account.Balance("BTC")
	assert.True(t, ok)
	// Must not credit +10 BTC (contracts). Only fee deducted from ~1.0.
	assert.True(t, btcAfter.Locked.IsZero())
	assert.Less(t, btcAfter.Available.Float64(), btcBefore)
	assert.Greater(t, btcAfter.Available.Float64(), 0.99)
	assert.InDelta(t, btcBefore-lastTrade.Fee.Float64(), btcAfter.Available.Float64(), 1e-12)
}

func TestSimplePriceMatching_CoinM_RoundTripSellThenBuy(t *testing.T) {
	account := getCoinMTestAccount()
	market := getCoinMTestMarket()
	engine := newCoinMMatching(account, market, 100_000, 5)

	var trades []types.Trade
	engine.OnTradeUpdate(func(trade types.Trade) {
		trades = append(trades, trade)
	})

	// Open short then cover — classic grid coin-m path
	_, _, err := engine.PlaceOrder(newLimitOrder("BTCUSD_PERP", types.SideTypeSell, 101_000, 10))
	assert.NoError(t, err)
	_, _, err = engine.PlaceOrder(newLimitOrder("BTCUSD_PERP", types.SideTypeBuy, 99_000, 10))
	assert.NoError(t, err)

	t1 := engine.currentTime.Add(time.Minute)
	engine.processKLine(newKLine("BTCUSD_PERP", types.Interval1m, t1, 100_000, 101_500, 100_000, 101_200))

	t2 := t1.Add(time.Minute)
	engine.processKLine(newKLine("BTCUSD_PERP", types.Interval1m, t2, 101_200, 101_200, 98_500, 99_500))

	assert.GreaterOrEqual(t, len(trades), 2)

	bal, ok := account.Balance("BTC")
	assert.True(t, ok)
	assert.True(t, bal.Locked.IsZero())
	// Still ~1 BTC minus fees — never +/- 10 contracts as coins
	assert.InDelta(t, 1.0, bal.Available.Float64(), 0.01)
}

func TestSimplePriceMatching_CoinM_RealizePnLToWallet(t *testing.T) {
	account := getCoinMTestAccount()
	market := getCoinMTestMarket()
	engine := newCoinMMatching(account, market, 100_000, 5)

	// Open short 10 @ 101000, cover 10 @ 99000 → profit in BTC
	_, _, err := engine.PlaceOrder(newLimitOrder("BTCUSD_PERP", types.SideTypeSell, 101_000, 10))
	assert.NoError(t, err)
	_, _, err = engine.PlaceOrder(newLimitOrder("BTCUSD_PERP", types.SideTypeBuy, 99_000, 10))
	assert.NoError(t, err)

	t1 := engine.currentTime.Add(time.Minute)
	engine.processKLine(newKLine("BTCUSD_PERP", types.Interval1m, t1, 100_000, 101_500, 100_000, 101_200))
	assert.Equal(t, -10.0, engine.coinMPosition.Float64())

	before, ok := account.Balance("BTC")
	assert.True(t, ok)

	t2 := t1.Add(time.Minute)
	engine.processKLine(newKLine("BTCUSD_PERP", types.Interval1m, t2, 101_200, 101_200, 98_500, 99_500))

	assert.True(t, engine.coinMPosition.IsZero())
	after, ok := account.Balance("BTC")
	assert.True(t, ok)
	// Wallet should increase by inverse PnL minus fees
	assert.Greater(t, after.Available.Float64(), before.Available.Float64())
}

func TestSimplePriceMatching_CoinM_FundingFee(t *testing.T) {
	account := getCoinMTestAccount()
	market := getCoinMTestMarket()
	engine := newCoinMMatching(account, market, 100_000, 5)
	engine.FundingRatePerSettlement = fixedpoint.NewFromFloat(0.0001)
	engine.coinMPosition = fixedpoint.NewFromInt(10)
	engine.coinMAverageCost = fixedpoint.NewFromInt(100_000)

	before, _ := account.Balance("BTC")
	// 08:00 UTC funding
	ts := time.Date(2024, 1, 1, 8, 0, 30, 0, time.UTC)
	engine.applyCoinMFunding(fixedpoint.NewFromInt(100_000), ts)
	after, _ := account.Balance("BTC")
	// Long pays: 10*100*0.0001/100000 = 0.000001 BTC
	assert.InDelta(t, before.Available.Float64()-0.000001, after.Available.Float64(), 1e-12)

	// Same hour should not double-charge
	engine.applyCoinMFunding(fixedpoint.NewFromInt(100_000), ts.Add(time.Minute))
	after2, _ := account.Balance("BTC")
	assert.Equal(t, after.Available.String(), after2.Available.String())
}

func TestSimplePriceMatching_CoinM_Liquidation(t *testing.T) {
	account := &types.Account{
		MakerFeeRate: fixedpoint.NewFromFloat(0.0002),
		TakerFeeRate: fixedpoint.NewFromFloat(0.0005),
	}
	// Tiny balance so maintenance margin fails quickly
	account.UpdateBalances(types.BalanceMap{
		"BTC": {Currency: "BTC", Available: fixedpoint.NewFromFloat(0.0001)},
	})
	market := getCoinMTestMarket()
	engine := newCoinMMatching(account, market, 100_000, 5)
	engine.MaintenanceMarginRate = fixedpoint.NewFromFloat(0.5) // 50% — easy to breach
	engine.coinMPosition = fixedpoint.NewFromInt(10)
	engine.coinMAverageCost = fixedpoint.NewFromInt(100_000)

	var trades int
	engine.OnTradeUpdate(func(types.Trade) { trades++ })

	engine.checkCoinMLiquidation(fixedpoint.NewFromInt(50_000)) // mark dumped → large unrealized loss
	assert.True(t, engine.coinMPosition.IsZero(), "position should be flattened")
	assert.Equal(t, 1, trades)
}

