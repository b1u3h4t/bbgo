package grid2

import (
	"fmt"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/strategy/grid2/grid2types"
	"github.com/c9s/bbgo/pkg/types"
)

// isCoinM reports whether this grid is running on coin-margined (inverse) futures.
// Prefer session.Delivery; fall back to Market.ContractValue (set for Binance Coin-M markets).
func (s *Strategy) isCoinM() bool {
	if s.session != nil && s.session.Delivery {
		return true
	}
	if s.ExchangeSession != nil && s.ExchangeSession.Delivery {
		return true
	}
	return s.Market.ContractValue.Sign() > 0
}

// isUSDTMFutures reports whether this grid is on USDT-M (linear) futures.
// Linear futures can open shorts without base inventory; spot cannot.
func (s *Strategy) isUSDTMFutures() bool {
	if s.isCoinM() {
		return false
	}
	if s.session != nil && s.session.Futures {
		return true
	}
	if s.ExchangeSession != nil && s.ExchangeSession.Futures {
		return true
	}
	return false
}

// isFutures reports whether the grid runs on USDT-M or Coin-M futures.
func (s *Strategy) isFutures() bool {
	return s.isUSDTMFutures() || s.isCoinM()
}

// gridLimitOrderTypeAndTIF returns order type / TIF for grid limit orders.
// With MakerOnly: spot uses LIMIT_MAKER; Binance futures use LIMIT + GTX (post-only).
func (s *Strategy) gridLimitOrderTypeAndTIF() (types.OrderType, types.TimeInForce) {
	if !s.MakerOnly {
		return types.OrderTypeLimit, types.TimeInForceGTC
	}
	if s.isFutures() {
		return types.OrderTypeLimit, types.TimeInForceGTX
	}
	return types.OrderTypeLimitMaker, types.TimeInForceGTC
}

// newGridLimitOrder builds a tagged grid limit order (optionally post-only).
func (s *Strategy) newGridLimitOrder(side types.SideType, price, quantity fixedpoint.Value) types.SubmitOrder {
	orderType, tif := s.gridLimitOrderTypeAndTIF()
	return types.SubmitOrder{
		Symbol:        s.Symbol,
		Market:        s.Market,
		Type:          orderType,
		Side:          side,
		Price:         price,
		Quantity:      quantity,
		TimeInForce:   tif,
		Tag:           orderTag,
		GroupID:       s.OrderGroupID,
		ClientOrderID: s.newClientOrderID(),
	}
}

func (s *Strategy) leverageOrOne() fixedpoint.Value {
	if s.Leverage.Sign() <= 0 {
		return fixedpoint.One
	}
	leverage := s.Leverage
	if leverage.Compare(fixedpoint.NewFromFloat(10)) > 0 {
		return fixedpoint.NewFromFloat(10)
	}
	return leverage
}

// coinMInitialMargin approximates inverse initial margin in base coin:
//
//	margin ≈ (contracts * contractValue) / price / leverage
func (s *Strategy) coinMInitialMargin(qty, price fixedpoint.Value) fixedpoint.Value {
	if price.IsZero() || qty.IsZero() || s.Market.ContractValue.IsZero() {
		return fixedpoint.Zero
	}
	return qty.Mul(s.Market.ContractValue).Div(price).Div(s.leverageOrOne())
}

func (s *Strategy) applyCoinMDefaults() {
	if !s.isCoinM() {
		return
	}
	s.EarnBase = true
	s.Compound = false
	if s.FeeRate.IsZero() {
		// Coin-M VIP0 maker ~0.02%
		s.FeeRate = fixedpoint.NewFromFloat(0.0002)
	}
}

func (s *Strategy) validateCoinM() error {
	if !s.isCoinM() {
		return nil
	}
	if s.QuantityOrAmount.Quantity.IsZero() {
		return fmt.Errorf("coin-m: quantity (contracts) is required; quoteInvestment/amount sizing is not supported")
	}
	if s.QuantityOrAmount.Amount.Sign() > 0 {
		return fmt.Errorf("coin-m: amount mode is not supported; set quantity (contracts)")
	}
	if s.Compound {
		return fmt.Errorf("coin-m: compound is not supported")
	}
	if s.Market.ContractValue.IsZero() {
		return fmt.Errorf("coin-m: market contractValue is missing for %s", s.Symbol)
	}
	return nil
}

// checkRequiredInvestmentByQuantityCoinM checks that available base balance covers
// estimated initial margin for all grid orders (both buy and sell pins).
func (s *Strategy) checkRequiredInvestmentByQuantityCoinM(
	baseBalance, quantity, lastPrice fixedpoint.Value, pins []grid2types.Pin,
) (requiredBase fixedpoint.Value, err error) {
	requiredBase = fixedpoint.Zero
	si := -1

	for i := len(pins) - 1; i >= 0; i-- {
		price := fixedpoint.Value(pins[i])
		orderPrice := price

		if price.Compare(lastPrice) >= 0 {
			si = i
			if i == 0 {
				continue
			}
			if s.ProfitSpread.Sign() > 0 {
				orderPrice = price.Add(s.ProfitSpread)
			} else {
				// sell converts to buy at next lower pin when no base inventory
				orderPrice = fixedpoint.Value(pins[i-1])
			}
		} else {
			if s.ProfitSpread.IsZero() && i+1 == si {
				continue
			}
			if i == len(pins)-1 {
				continue
			}
		}

		requiredBase = requiredBase.Add(s.coinMInitialMargin(quantity, orderPrice))
	}

	if requiredBase.Compare(baseBalance) > 0 {
		return requiredBase, fmt.Errorf(
			"coin-m base margin (%f %s) is not enough, required ≈ %f (contracts=%s, leverage=%s, contractValue=%s)",
			baseBalance.Float64(), s.Market.BaseCurrency,
			requiredBase.Float64(),
			quantity.String(), s.leverageOrOne().String(), s.Market.ContractValue.String(),
		)
	}
	return requiredBase, nil
}

// generateCoinMGridOrders places fixed-contract grid orders and budgets by base initial margin.
func (s *Strategy) generateCoinMGridOrders(totalBase, lastPrice fixedpoint.Value) ([]types.SubmitOrder, error) {
	quantity := s.QuantityOrAmount.Quantity
	if quantity.IsZero() {
		return nil, fmt.Errorf("coin-m: quantity (contracts) is required")
	}

	pins := s.grid.Pins
	usedMargin := fixedpoint.Zero
	var submitOrders []types.SubmitOrder
	si := len(pins)

	for i := len(pins) - 1; i >= 0; i-- {
		price := fixedpoint.Value(pins[i])
		sellPrice := price
		if s.ProfitSpread.Sign() > 0 {
			sellPrice = sellPrice.Add(s.ProfitSpread)
		}

		placeSell := price.Compare(lastPrice) >= 0
		if s.BaseGridNum > 0 {
			placeSell = i >= len(pins)-1-s.BaseGridNum
		}

		if placeSell {
			si = i
			if i == 0 {
				continue
			}

			margin := s.coinMInitialMargin(quantity, sellPrice)
			if usedMargin.Add(margin).Compare(totalBase) <= 0 {
				submitOrders = append(submitOrders, s.newGridLimitOrder(types.SideTypeSell, sellPrice, quantity))
				usedMargin = usedMargin.Add(margin)
				continue
			}

			// Not enough margin for sell → place buy at next lower pin (same as spot fallback).
			nextPrice := fixedpoint.Value(pins[i-1])
			margin = s.coinMInitialMargin(quantity, nextPrice)
			if usedMargin.Add(margin).Compare(totalBase) > 0 {
				return nil, fmt.Errorf(
					"coin-m: used base margin %f + %f > available %f %s",
					usedMargin.Float64(), margin.Float64(), totalBase.Float64(), s.Market.BaseCurrency,
				)
			}
			submitOrders = append(submitOrders, s.newGridLimitOrder(types.SideTypeBuy, nextPrice, quantity))
			usedMargin = usedMargin.Add(margin)
			continue
		}

		if s.ProfitSpread.IsZero() && i+1 == si {
			continue
		}
		if i == len(pins)-1 {
			continue
		}

		margin := s.coinMInitialMargin(quantity, price)
		if usedMargin.Add(margin).Compare(totalBase) > 0 {
			return nil, fmt.Errorf(
				"coin-m: used base margin %f + %f > available %f %s",
				usedMargin.Float64(), margin.Float64(), totalBase.Float64(), s.Market.BaseCurrency,
			)
		}

		submitOrders = append(submitOrders, s.newGridLimitOrder(types.SideTypeBuy, price, quantity))
		usedMargin = usedMargin.Add(margin)
	}

	return submitOrders, nil
}
