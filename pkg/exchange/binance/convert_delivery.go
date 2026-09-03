package binance

import (
	"fmt"
	"time"

	"github.com/adshao/go-binance/v2/delivery"
	"github.com/pkg/errors"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

func toGlobalDeliveryMarket(symbol delivery.Symbol) types.Market {
	market := types.Market{
		Exchange:        types.ExchangeBinance,
		Symbol:          symbol.Symbol,
		LocalSymbol:     symbol.Symbol,
		PricePrecision:  symbol.PricePrecision,
		VolumePrecision: symbol.QuantityPrecision,
		QuotePrecision:  symbol.QuotePrecision,
		QuoteCurrency:   symbol.QuoteAsset,
		BaseCurrency:    symbol.BaseAsset,
		ContractValue:   fixedpoint.NewFromInt(int64(symbol.ContractSize)),
	}

	if f := symbol.LotSizeFilter(); f != nil {
		market.MinQuantity = fixedpoint.MustNewFromString(f.MinQuantity)
		market.MaxQuantity = fixedpoint.MustNewFromString(f.MaxQuantity)
		market.StepSize = fixedpoint.MustNewFromString(f.StepSize)
	}

	if f := symbol.PriceFilter(); f != nil {
		market.MaxPrice = fixedpoint.MustNewFromString(f.MaxPrice)
		market.MinPrice = fixedpoint.MustNewFromString(f.MinPrice)
		market.TickSize = fixedpoint.MustNewFromString(f.TickSize)
	}

	// Coin-M min notional is not always present; use a conservative placeholder from contract value.
	if market.MinNotional.IsZero() && market.ContractValue.Sign() > 0 {
		market.MinNotional = market.ContractValue
		market.MinAmount = market.ContractValue
	}

	return market
}

func toLocalDeliveryOrderType(orderType types.OrderType) (delivery.OrderType, error) {
	switch orderType {
	case types.OrderTypeLimit, types.OrderTypeLimitMaker:
		return delivery.OrderTypeLimit, nil
	case types.OrderTypeMarket:
		return delivery.OrderTypeMarket, nil
	case types.OrderTypeStopLimit:
		return delivery.OrderTypeStop, nil
	case types.OrderTypeStopMarket:
		return delivery.OrderTypeStopMarket, nil
	case types.OrderTypeTakeProfit:
		return delivery.OrderTypeTakeProfit, nil
	case types.OrderTypeTakeProfitMarket:
		return delivery.OrderTypeTakeProfitMarket, nil
	default:
		return "", fmt.Errorf("coin-m delivery: order type %s not supported", orderType)
	}
}

func toGlobalDeliveryOrderType(orderType delivery.OrderType) types.OrderType {
	switch orderType {
	case delivery.OrderTypeLimit:
		return types.OrderTypeLimit
	case delivery.OrderTypeMarket:
		return types.OrderTypeMarket
	case delivery.OrderTypeStop:
		return types.OrderTypeStopLimit
	case delivery.OrderTypeStopMarket:
		return types.OrderTypeStopMarket
	case delivery.OrderTypeTakeProfit:
		return types.OrderTypeTakeProfit
	case delivery.OrderTypeTakeProfitMarket:
		return types.OrderTypeTakeProfitMarket
	case delivery.OrderTypeTrailingStopMarket:
		return types.OrderTypeTrailingStopMarket
	default:
		log.Errorf("coin-m delivery: unknown order type %q", orderType)
		return types.OrderType(orderType)
	}
}

func toGlobalDeliveryOrderStatus(status delivery.OrderStatusType) types.OrderStatus {
	switch status {
	case delivery.OrderStatusTypeNew:
		return types.OrderStatusNew
	case delivery.OrderStatusTypePartiallyFilled:
		return types.OrderStatusPartiallyFilled
	case delivery.OrderStatusTypeFilled:
		return types.OrderStatusFilled
	case delivery.OrderStatusTypeCanceled:
		return types.OrderStatusCanceled
	case delivery.OrderStatusTypeRejected:
		return types.OrderStatusRejected
	case delivery.OrderStatusTypeExpired:
		return types.OrderStatusExpired
	default:
		return types.OrderStatus(status)
	}
}

func toGlobalDeliverySideType(side delivery.SideType) types.SideType {
	switch side {
	case delivery.SideTypeBuy:
		return types.SideTypeBuy
	case delivery.SideTypeSell:
		return types.SideTypeSell
	default:
		return ""
	}
}

func toGlobalDeliveryOrder(o *delivery.Order, isIsolated bool) (*types.Order, error) {
	if o == nil {
		return nil, errors.New("nil delivery order")
	}

	orderPrice := o.Price
	if avg := fixedpoint.MustNewFromString(o.AvgPrice); avg.Sign() > 0 && fixedpoint.MustNewFromString(o.Price).IsZero() {
		orderPrice = o.AvgPrice
	}

	return &types.Order{
		SubmitOrder: types.SubmitOrder{
			ClientOrderID: o.ClientOrderID,
			Symbol:        o.Symbol,
			Side:          toGlobalDeliverySideType(o.Side),
			Type:          toGlobalDeliveryOrderType(o.Type),
			ReduceOnly:    o.ReduceOnly,
			ClosePosition: o.ClosePosition,
			Quantity:      fixedpoint.MustNewFromString(o.OrigQuantity),
			StopPrice:     fixedpoint.MustNewFromString(o.StopPrice),
			Price:         fixedpoint.MustNewFromString(orderPrice),
			TimeInForce:   types.TimeInForce(o.TimeInForce),
		},
		Exchange:         types.ExchangeBinance,
		OrderID:          uint64(o.OrderID),
		Status:           toGlobalDeliveryOrderStatus(o.Status),
		ExecutedQuantity: fixedpoint.MustNewFromString(o.ExecutedQuantity),
		CreationTime:     types.Time(millisecondTime(o.Time)),
		UpdateTime:       types.Time(millisecondTime(o.UpdateTime)),
		IsFutures:        true,
		IsIsolated:       isIsolated,
	}, nil
}

func toGlobalDeliveryOrders(orders []*delivery.Order, isIsolated bool) ([]types.Order, error) {
	out := make([]types.Order, 0, len(orders))
	for _, o := range orders {
		g, err := toGlobalDeliveryOrder(o, isIsolated)
		if err != nil {
			return out, err
		}
		out = append(out, *g)
	}
	return out, nil
}

func toGlobalDeliveryTicker(stats *delivery.PriceChangeStats) (*types.Ticker, error) {
	return &types.Ticker{
		Volume: fixedpoint.MustNewFromString(stats.Volume),
		Last:   fixedpoint.MustNewFromString(stats.LastPrice),
		Open:   fixedpoint.MustNewFromString(stats.OpenPrice),
		High:   fixedpoint.MustNewFromString(stats.HighPrice),
		Low:    fixedpoint.MustNewFromString(stats.LowPrice),
		Buy:    fixedpoint.MustNewFromString(stats.LastPrice),
		Sell:   fixedpoint.MustNewFromString(stats.LastPrice),
		Time:   time.Unix(0, stats.CloseTime*int64(time.Millisecond)),
	}, nil
}

func toGlobalDeliveryAccount(account *delivery.Account, risks []*delivery.PositionRisk) *types.FuturesAccount {
	assets := make(types.FuturesAssetMap)
	for _, a := range account.Assets {
		assets[a.Asset] = types.FuturesUserAsset{
			Asset:                  a.Asset,
			InitialMargin:          fixedpoint.MustNewFromString(a.InitialMargin),
			MaintMargin:            fixedpoint.MustNewFromString(a.MaintMargin),
			MarginBalance:          fixedpoint.MustNewFromString(a.MarginBalance),
			MaxWithdrawAmount:      fixedpoint.MustNewFromString(a.MaxWithdrawAmount),
			OpenOrderInitialMargin: fixedpoint.MustNewFromString(a.OpenOrderInitialMargin),
			PositionInitialMargin:  fixedpoint.MustNewFromString(a.PositionInitialMargin),
			UnrealizedProfit:       fixedpoint.MustNewFromString(a.UnrealizedProfit),
			WalletBalance:          fixedpoint.MustNewFromString(a.WalletBalance),
		}
	}

	riskMap := make(map[types.PositionKey]types.PositionRisk)
	for _, r := range toGlobalDeliveryPositionRisk(risks) {
		riskMap[types.NewPositionKey(r.Symbol, r.PositionSide)] = r
	}

	positions := make(types.FuturesPositionMap)
	for _, p := range account.Positions {
		side := toGlobalPositionSide(p.PositionSide)
		pos := types.FuturesPosition{
			Isolated:     p.Isolated,
			AverageCost:  fixedpoint.MustNewFromString(p.EntryPrice),
			Base:         fixedpoint.MustNewFromString(p.PositionAmt),
			Quote:        fixedpoint.Zero,
			PositionSide: side,
			Symbol:       p.Symbol,
			PositionRisk: &types.PositionRisk{
				Leverage: fixedpoint.MustNewFromString(p.Leverage),
			},
		}
		key := types.NewPositionKey(p.Symbol, side)
		if risk, ok := riskMap[key]; ok {
			risk.Leverage = fixedpoint.MustNewFromString(p.Leverage)
			pos.PositionRisk = &risk
		}
		positions[key] = pos
	}

	return &types.FuturesAccount{
		Assets:    assets,
		Positions: positions,
	}
}

func toGlobalDeliveryPositionRisk(risks []*delivery.PositionRisk) []types.PositionRisk {
	out := make([]types.PositionRisk, 0, len(risks))
	for _, r := range risks {
		if r == nil {
			continue
		}
		out = append(out, types.PositionRisk{
			Symbol:           r.Symbol,
			PositionSide:     toGlobalPositionSide(r.PositionSide),
			Leverage:         fixedpoint.MustNewFromString(r.Leverage),
			EntryPrice:       fixedpoint.MustNewFromString(r.EntryPrice),
			MarkPrice:        fixedpoint.MustNewFromString(r.MarkPrice),
			LiquidationPrice: fixedpoint.MustNewFromString(r.LiquidationPrice),
			UnrealizedPnL:    fixedpoint.MustNewFromString(r.UnRealizedProfit),
			PositionAmount:   fixedpoint.MustNewFromString(r.PositionAmt),
		})
	}
	return out
}
