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
		lastFundingHour: -1,
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

// executeCoinMTrade releases locked margin back to available, realizes inverse
// PnL into the base wallet when reducing position, and deducts base fee.
// Contracts are not credited as spot base/quote — position lives on the book.
func (m *SimplePriceMatching) executeCoinMTrade(trade types.Trade, lockedPrice fixedpoint.Value) error {
	margin := m.coinMInitialMargin(trade.Quantity, lockedPrice)
	if err := m.account.UnlockBalance(m.Market.BaseCurrency, margin); err != nil {
		return err
	}

	if pnl := m.applyCoinMPositionAndRealizePnL(trade); !pnl.IsZero() {
		m.account.AddBalance(m.Market.BaseCurrency, pnl)
	}

	if trade.Fee.Sign() > 0 && trade.FeeCurrency == m.Market.BaseCurrency {
		m.account.AddBalance(m.Market.BaseCurrency, trade.Fee.Neg())
	}
	return nil
}

// applyCoinMPositionAndRealizePnL updates signed contract position and returns
// realized inverse PnL in base: CV * closedQty * (1/buy - 1/sell).
func (m *SimplePriceMatching) applyCoinMPositionAndRealizePnL(trade types.Trade) fixedpoint.Value {
	qty := trade.Quantity
	price := trade.Price
	cv := m.Market.ContractValue
	pos := m.coinMPosition
	pnl := fixedpoint.Zero

	if trade.IsBuyer {
		// Buy: closes short first, then opens/increases long
		if pos.Sign() < 0 {
			closed := fixedpoint.Min(pos.Abs(), qty)
			// short cover: entry was sell at averageCost, exit buy at price
			pnl = cv.Mul(closed).Mul(fixedpoint.One.Div(price).Sub(fixedpoint.One.Div(m.coinMAverageCost)))
			pos = pos.Add(closed)
			qty = qty.Sub(closed)
			if pos.IsZero() {
				m.coinMAverageCost = fixedpoint.Zero
			}
		}
		if qty.Sign() > 0 {
			// open/increase long
			if pos.Sign() > 0 && !m.coinMAverageCost.IsZero() {
				m.coinMAverageCost = m.coinMAverageCost.Mul(pos).Add(price.Mul(qty)).Div(pos.Add(qty))
			} else {
				m.coinMAverageCost = price
			}
			pos = pos.Add(qty)
		}
	} else {
		// Sell: closes long first, then opens/increases short
		if pos.Sign() > 0 {
			closed := fixedpoint.Min(pos, qty)
			pnl = cv.Mul(closed).Mul(fixedpoint.One.Div(m.coinMAverageCost).Sub(fixedpoint.One.Div(price)))
			pos = pos.Sub(closed)
			qty = qty.Sub(closed)
			if pos.IsZero() {
				m.coinMAverageCost = fixedpoint.Zero
			}
		}
		if qty.Sign() > 0 {
			negPos := pos.Neg() // size of existing short
			if pos.Sign() < 0 && !m.coinMAverageCost.IsZero() {
				m.coinMAverageCost = m.coinMAverageCost.Mul(negPos).Add(price.Mul(qty)).Div(negPos.Add(qty))
			} else {
				m.coinMAverageCost = price
			}
			pos = pos.Sub(qty)
		}
	}

	m.coinMPosition = pos
	return pnl
}

// coinMUnrealizedPnL estimates open inverse PnL in base at mark price.
func (m *SimplePriceMatching) coinMUnrealizedPnL(mark fixedpoint.Value) fixedpoint.Value {
	if m.coinMPosition.IsZero() || m.coinMAverageCost.IsZero() || mark.IsZero() {
		return fixedpoint.Zero
	}
	cv := m.Market.ContractValue
	qty := m.coinMPosition.Abs()
	if m.coinMPosition.Sign() > 0 {
		return cv.Mul(qty).Mul(fixedpoint.One.Div(m.coinMAverageCost).Sub(fixedpoint.One.Div(mark)))
	}
	return cv.Mul(qty).Mul(fixedpoint.One.Div(mark).Sub(fixedpoint.One.Div(m.coinMAverageCost)))
}

func (m *SimplePriceMatching) coinMMaintenanceRate() fixedpoint.Value {
	if m.MaintenanceMarginRate.Sign() > 0 {
		return m.MaintenanceMarginRate
	}
	return fixedpoint.NewFromFloat(0.005) // 0.5%
}

// applyCoinMFunding settles funding into the base wallet at 00/08/16 UTC.
// Fee = positionNotionalUSD * rate / price (longs pay when rate > 0).
func (m *SimplePriceMatching) applyCoinMFunding(mark fixedpoint.Value, t time.Time) {
	if !m.isCoinM() || m.coinMPosition.IsZero() || m.FundingRatePerSettlement.IsZero() || mark.IsZero() {
		return
	}
	utc := t.UTC()
	hour := utc.Hour()
	if hour != 0 && hour != 8 && hour != 16 {
		return
	}
	key := utc.YearDay()*24 + hour
	if m.lastFundingHour == key {
		return
	}
	m.lastFundingHour = key

	notional := m.coinMNotionalUSD(m.coinMPosition.Abs())
	fee := notional.Mul(m.FundingRatePerSettlement).Div(mark)
	if m.coinMPosition.Sign() > 0 {
		m.account.AddBalance(m.Market.BaseCurrency, fee.Neg())
	} else {
		m.account.AddBalance(m.Market.BaseCurrency, fee)
	}
	m.EmitBalanceUpdate(m.account.Balances())
}

// checkCoinMLiquidation force-closes the book position if equity < maintenance margin.
func (m *SimplePriceMatching) checkCoinMLiquidation(mark fixedpoint.Value) {
	if !m.isCoinM() || m.coinMPosition.IsZero() || mark.IsZero() {
		return
	}
	bal, ok := m.account.Balance(m.Market.BaseCurrency)
	if !ok {
		return
	}
	equity := bal.Total().Add(m.coinMUnrealizedPnL(mark))
	// maintenance margin in base ≈ notionalUSD / price * mmr
	mm := m.coinMNotionalUSD(m.coinMPosition.Abs()).Div(mark).Mul(m.coinMMaintenanceRate())
	if equity.Compare(mm) >= 0 {
		return
	}

	klineMatchingLogger.Warnf("coin-m liquidation triggered: equity=%s mm=%s pos=%s mark=%s",
		equity.String(), mm.String(), m.coinMPosition.String(), mark.String())

	qty := m.coinMPosition.Abs()
	side := types.SideTypeSell
	if m.coinMPosition.Sign() < 0 {
		side = types.SideTypeBuy
	}
	// Synthetic market fill to flatten
	order := types.Order{
		SubmitOrder: types.SubmitOrder{
			Symbol:   m.Symbol,
			Side:     side,
			Type:     types.OrderTypeMarket,
			Quantity: qty,
			Price:    mark,
		},
		OrderID:   incOrderID(),
		Status:    types.OrderStatusFilled,
		ExecutedQuantity: qty,
	}
	trade := m.newTradeFromOrder(&order, false, mark)
	// No margin was locked for liquidation — adjust position/PnL/fee directly
	if pnl := m.applyCoinMPositionAndRealizePnL(trade); !pnl.IsZero() {
		m.account.AddBalance(m.Market.BaseCurrency, pnl)
	}
	if trade.Fee.Sign() > 0 && trade.FeeCurrency == m.Market.BaseCurrency {
		m.account.AddBalance(m.Market.BaseCurrency, trade.Fee.Neg())
	}
	m.EmitTradeUpdate(trade)
	m.EmitOrderUpdate(order)
	m.EmitBalanceUpdate(m.account.Balances())
}

