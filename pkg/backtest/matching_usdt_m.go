package backtest

import (
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

// isUSDTM reports linear (USDT-margined) futures: futures mode without ContractValue.
func (m *SimplePriceMatching) isUSDTM() bool {
	return m.futures && !m.isCoinM()
}

// usdtMInitialMargin approximates linear initial margin in quote:
//
//	margin ≈ qty * price / leverage
func (m *SimplePriceMatching) usdtMInitialMargin(qty, price fixedpoint.Value) fixedpoint.Value {
	if price.IsZero() || qty.IsZero() {
		return fixedpoint.Zero
	}
	return qty.Mul(price).Div(m.leverageOrOne())
}

func (m *SimplePriceMatching) lockUSDTMMargin(qty, price fixedpoint.Value) error {
	margin := m.usdtMInitialMargin(qty, price)
	return m.account.LockBalance(m.Market.QuoteCurrency, margin)
}

func (m *SimplePriceMatching) unlockUSDTMMargin(qty, price fixedpoint.Value) error {
	margin := m.usdtMInitialMargin(qty, price)
	return m.account.UnlockBalance(m.Market.QuoteCurrency, margin)
}

// executeUSDTMTrade releases locked quote margin, realizes linear PnL into the
// quote wallet when reducing position, and deducts quote fee.
// Base is never credited — position lives on the book (usdtMPosition).
func (m *SimplePriceMatching) executeUSDTMTrade(trade types.Trade, lockedPrice fixedpoint.Value) error {
	margin := m.usdtMInitialMargin(trade.Quantity, lockedPrice)
	if err := m.account.UnlockBalance(m.Market.QuoteCurrency, margin); err != nil {
		return err
	}

	if pnl := m.applyUSDTMPositionAndRealizePnL(trade); !pnl.IsZero() {
		m.account.AddBalance(m.Market.QuoteCurrency, pnl)
	}

	if trade.Fee.Sign() > 0 && trade.FeeCurrency == m.Market.QuoteCurrency {
		m.account.AddBalance(m.Market.QuoteCurrency, trade.Fee.Neg())
	} else if trade.Fee.Sign() > 0 && trade.FeeCurrency == m.Market.BaseCurrency {
		// native buy fee in base — convert to quote at trade price
		m.account.AddBalance(m.Market.QuoteCurrency, trade.Fee.Mul(trade.Price).Neg())
	}
	return nil
}

// applyUSDTMPositionAndRealizePnL updates signed base position and returns
// realized linear PnL in quote: long close (exit-entry)*qty, short close (entry-exit)*qty.
func (m *SimplePriceMatching) applyUSDTMPositionAndRealizePnL(trade types.Trade) fixedpoint.Value {
	qty := trade.Quantity
	price := trade.Price
	pos := m.usdtMPosition
	pnl := fixedpoint.Zero

	if trade.IsBuyer {
		if pos.Sign() < 0 {
			closed := fixedpoint.Min(pos.Abs(), qty)
			pnl = m.usdtMAverageCost.Sub(price).Mul(closed)
			pos = pos.Add(closed)
			qty = qty.Sub(closed)
			if pos.IsZero() {
				m.usdtMAverageCost = fixedpoint.Zero
			}
		}
		if qty.Sign() > 0 {
			if pos.Sign() > 0 && !m.usdtMAverageCost.IsZero() {
				m.usdtMAverageCost = m.usdtMAverageCost.Mul(pos).Add(price.Mul(qty)).Div(pos.Add(qty))
			} else {
				m.usdtMAverageCost = price
			}
			pos = pos.Add(qty)
		}
	} else {
		if pos.Sign() > 0 {
			closed := fixedpoint.Min(pos, qty)
			pnl = price.Sub(m.usdtMAverageCost).Mul(closed)
			pos = pos.Sub(closed)
			qty = qty.Sub(closed)
			if pos.IsZero() {
				m.usdtMAverageCost = fixedpoint.Zero
			}
		}
		if qty.Sign() > 0 {
			negPos := pos.Neg()
			if pos.Sign() < 0 && !m.usdtMAverageCost.IsZero() {
				m.usdtMAverageCost = m.usdtMAverageCost.Mul(negPos).Add(price.Mul(qty)).Div(negPos.Add(qty))
			} else {
				m.usdtMAverageCost = price
			}
			pos = pos.Sub(qty)
		}
	}

	m.usdtMPosition = pos
	return pnl
}
