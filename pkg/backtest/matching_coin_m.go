package backtest

import (
	"time"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

// NewMatching constructs a SimplePriceMatching for tests or synthetic drivers.
func NewMatching(
	account *types.Account,
	market types.Market,
	lastPrice fixedpoint.Value,
	leverage fixedpoint.Value,
) *SimplePriceMatching {
	return &SimplePriceMatching{
		Symbol:       market.Symbol,
		Market:       market,
		account:      account,
		closedOrders: make(map[uint64]types.Order),
		lastPrice:    lastPrice,
		currentTime:  time.Now().UTC(),
		Leverage:     leverage,
	}
}

// isCoinM reports inverse (coin-margined) markets via ContractValue.
func (m *SimplePriceMatching) isCoinM() bool {
	return m.Market.ContractValue.Sign() > 0
}

func (m *SimplePriceMatching) leverageOrOne() fixedpoint.Value {
	if m.Leverage.Sign() <= 0 {
		return fixedpoint.One
	}
	return m.Leverage
}

// coinMNotionalUSD is contracts * ContractValue (true USD notional).
func (m *SimplePriceMatching) coinMNotionalUSD(qty fixedpoint.Value) fixedpoint.Value {
	return qty.Mul(m.Market.ContractValue)
}

// coinMInitialMargin approximates inverse initial margin in base coin:
//
//	margin ≈ (contracts * contractValue) / price / leverage
func (m *SimplePriceMatching) coinMInitialMargin(qty, price fixedpoint.Value) fixedpoint.Value {
	if price.IsZero() || qty.IsZero() || m.Market.ContractValue.IsZero() {
		return fixedpoint.Zero
	}
	return qty.Mul(m.Market.ContractValue).Div(price).Div(m.leverageOrOne())
}

// coinMFeeInBase computes commission in the base coin:
//
//	fee ≈ notionalUSD / price * feeRate
func coinMFeeInBase(qty, price, contractValue, feeRate fixedpoint.Value) fixedpoint.Value {
	if price.IsZero() || qty.IsZero() || contractValue.IsZero() {
		return fixedpoint.Zero
	}
	return qty.Mul(contractValue).Div(price).Mul(feeRate)
}

func (m *SimplePriceMatching) lockCoinMMargin(qty, price fixedpoint.Value) error {
	margin := m.coinMInitialMargin(qty, price)
	return m.account.LockBalance(m.Market.BaseCurrency, margin)
}

func (m *SimplePriceMatching) unlockCoinMMargin(qty, price fixedpoint.Value) error {
	margin := m.coinMInitialMargin(qty, price)
	return m.account.UnlockBalance(m.Market.BaseCurrency, margin)
}

// executeCoinMTrade releases locked margin back to available and deducts base fee.
// Contracts are not credited as spot base/quote — position lives in the strategy.
func (m *SimplePriceMatching) executeCoinMTrade(trade types.Trade, lockedPrice fixedpoint.Value) error {
	margin := m.coinMInitialMargin(trade.Quantity, lockedPrice)
	if err := m.account.UnlockBalance(m.Market.BaseCurrency, margin); err != nil {
		return err
	}

	if trade.Fee.Sign() > 0 && trade.FeeCurrency == m.Market.BaseCurrency {
		m.account.AddBalance(m.Market.BaseCurrency, trade.Fee.Neg())
	}
	return nil
}
