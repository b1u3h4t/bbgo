package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/delivery"
	"go.uber.org/multierr"

	"github.com/c9s/bbgo/pkg/exchange/binance/binanceapi"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

func (e *Exchange) TransferDeliveryAccountAsset(
	ctx context.Context, asset string, amount fixedpoint.Value, io types.TransferDirection,
) error {
	req := e.client2.NewFuturesTransferRequest()
	req.Asset(asset)
	req.Amount(amount.String())

	switch io {
	case types.TransferIn:
		req.TransferType(binanceapi.FuturesTransferSpotToCoinFutures)
	case types.TransferOut:
		req.TransferType(binanceapi.FuturesTransferCoinFuturesToSpot)
	default:
		return fmt.Errorf("unexpected transfer direction: %d given", io)
	}

	resp, err := req.Do(ctx)
	switch io {
	case types.TransferIn:
		log.Infof("internal transfer (spot) => (coin-m) %s %s, transaction = %+v, err = %+v", amount.String(), asset, resp, err)
	case types.TransferOut:
		log.Infof("internal transfer (coin-m) => (spot) %s %s, transaction = %+v, err = %+v", amount.String(), asset, resp, err)
	}
	return err
}

func (e *Exchange) QueryDeliveryAccount(ctx context.Context) (*types.Account, error) {
	account, err := e.deliveryClient.NewGetAccountService().Do(ctx)
	if err != nil {
		return nil, err
	}

	balancesResp, err := e.deliveryClient.NewGetBalanceService().Do(ctx)
	if err != nil {
		return nil, err
	}

	risks, err := e.deliveryClient.NewGetPositionRiskService().Do(ctx)
	if err != nil {
		return nil, err
	}

	balances := map[string]types.Balance{}
	for _, b := range balancesResp {
		bal := types.Balance{
			Currency:  b.Asset,
			Available: fixedpoint.MustNewFromString(b.Balance),
			Locked:    fixedpoint.Zero,
		}
		maxWithdraw := fixedpoint.MustNewFromString(b.WithdrawAvailable)
		bal.MaxWithdrawAmount = &maxWithdraw

		availMargin := fixedpoint.MustNewFromString(b.AvailableBalance)
		crossUnPnl := fixedpoint.MustNewFromString(b.CrossUnPnl)
		bal.LongAvailableCredit = availMargin.Add(crossUnPnl)
		bal.ShortAvailableCredit = availMargin.Add(crossUnPnl)

		balances[b.Asset] = bal
	}

	a := &types.Account{
		AccountType: types.AccountTypeFutures,
		FuturesInfo: toGlobalDeliveryAccount(account, risks),
		CanDeposit:  account.CanDeposit,
		CanTrade:    account.CanTrade,
		CanWithdraw: account.CanWithdraw,
	}
	a.UpdateBalances(balances)
	return a, nil
}

func (e *Exchange) cancelDeliveryOrders(ctx context.Context, orders ...types.Order) (err error) {
	for _, o := range orders {
		req := e.deliveryClient.NewCancelOrderService().Symbol(o.Symbol)
		if o.OrderID > 0 {
			req.OrderID(int64(o.OrderID))
		} else if len(o.ClientOrderID) > 0 {
			req.OrigClientOrderID(o.ClientOrderID)
		} else {
			err = multierr.Append(err, types.NewOrderError(
				fmt.Errorf("can not cancel %s coin-m order without orderID or clientOrderID", o.Symbol), o))
			continue
		}

		if _, err2 := req.Do(ctx); err2 != nil {
			err = multierr.Append(err, types.NewOrderError(err2, o))
		}
	}
	return err
}

func (e *Exchange) submitDeliveryOrder(ctx context.Context, order types.SubmitOrder) (*types.Order, error) {
	orderType, err := toLocalDeliveryOrderType(order.Type)
	if err != nil {
		return nil, err
	}

	req := e.deliveryClient.NewCreateOrderService().
		Symbol(order.Symbol).
		Type(orderType).
		Side(delivery.SideType(order.Side)).
		NewOrderResponseType(delivery.NewOrderRespTypeRESULT)

	if dualSidePosition {
		switch order.Side {
		case types.SideTypeBuy:
			req.PositionSide(delivery.PositionSideTypeLong)
		case types.SideTypeSell:
			req.PositionSide(delivery.PositionSideTypeShort)
		}
	} else if order.ReduceOnly {
		req.ReduceOnly(order.ReduceOnly)
	} else if order.ClosePosition {
		req.ClosePosition(order.ClosePosition)
	}

	clientOrderID := newFuturesClientOrderID(order.ClientOrderID)
	if len(clientOrderID) > 0 {
		req.NewClientOrderID(clientOrderID)
	}

	if !order.ClosePosition {
		if order.Market.Symbol != "" {
			req.Quantity(order.Market.FormatQuantity(order.Quantity))
		} else {
			req.Quantity(order.Quantity.FormatString(8))
		}
	}

	switch order.Type {
	case types.OrderTypeStopLimit, types.OrderTypeLimit, types.OrderTypeLimitMaker:
		if order.Market.Symbol != "" {
			req.Price(order.Market.FormatPrice(order.Price))
		} else {
			req.Price(order.Price.FormatString(8))
		}
	}

	switch order.Type {
	case types.OrderTypeStopLimit, types.OrderTypeStopMarket, types.OrderTypeTakeProfitMarket:
		if order.Market.Symbol != "" {
			req.StopPrice(order.Market.FormatPrice(order.StopPrice))
		} else {
			req.StopPrice(order.StopPrice.FormatString(8))
		}
	}

	if len(order.TimeInForce) > 0 {
		req.TimeInForce(delivery.TimeInForceType(order.TimeInForce))
	} else {
		switch order.Type {
		case types.OrderTypeLimit, types.OrderTypeLimitMaker, types.OrderTypeStopLimit:
			req.TimeInForce(delivery.TimeInForceTypeGTC)
		}
	}

	response, err := req.Do(ctx)
	if err != nil {
		return nil, err
	}

	log.Infof("coin-m delivery order creation response: %+v", response)

	return toGlobalDeliveryOrder(&delivery.Order{
		Symbol:           response.Symbol,
		OrderID:          response.OrderID,
		ClientOrderID:    response.ClientOrderID,
		Price:            response.Price,
		OrigQuantity:     response.OrigQuantity,
		ExecutedQuantity: response.ExecutedQuantity,
		Status:           response.Status,
		TimeInForce:      response.TimeInForce,
		Type:             response.Type,
		Side:             response.Side,
		ReduceOnly:       response.ReduceOnly,
		ClosePosition:    response.ClosePosition,
		AvgPrice:         response.AvgPrice,
		StopPrice:        response.StopPrice,
		UpdateTime:       response.UpdateTime,
		Time:             response.UpdateTime,
	}, false)
}

func (e *Exchange) queryDeliveryOpenOrders(ctx context.Context, symbol string) ([]types.Order, error) {
	req := e.deliveryClient.NewListOpenOrdersService()
	if symbol != "" {
		req.Symbol(symbol)
	}
	binanceOrders, err := req.Do(ctx)
	if err != nil {
		return nil, err
	}
	return toGlobalDeliveryOrders(binanceOrders, false)
}

func (e *Exchange) queryDeliveryClosedOrders(
	ctx context.Context, symbol string, since, until time.Time, lastOrderID uint64,
) ([]types.Order, error) {
	req := e.deliveryClient.NewListOrdersService().Symbol(symbol)
	if lastOrderID > 0 {
		req.OrderID(int64(lastOrderID))
	} else {
		req.StartTime(since.UnixMilli())
		if until.Sub(since) < 24*time.Hour {
			req.EndTime(until.UnixMilli())
		}
	}

	binanceOrders, err := req.Do(ctx)
	if err != nil {
		return nil, err
	}
	return toGlobalDeliveryOrders(binanceOrders, false)
}

func (e *Exchange) queryDeliveryOrder(ctx context.Context, q types.OrderQuery) (*types.Order, error) {
	req := e.deliveryClient.NewGetOrderService().Symbol(q.Symbol)
	if len(q.OrderID) > 0 {
		orderID, err := strconv.ParseInt(q.OrderID, 10, 64)
		if err != nil {
			return nil, err
		}
		req.OrderID(orderID)
	} else if len(q.ClientOrderID) > 0 {
		req.OrigClientOrderID(q.ClientOrderID)
	} else {
		return nil, fmt.Errorf("empty order id")
	}

	o, err := req.Do(ctx)
	if err != nil {
		return nil, err
	}
	return toGlobalDeliveryOrder(o, false)
}

func (e *Exchange) QueryDeliveryKLines(
	ctx context.Context, symbol string, interval types.Interval, options types.KLineQueryOptions,
) ([]types.KLine, error) {
	limit := 1000
	if options.Limit > 0 {
		limit = options.Limit
	}

	log.Infof("querying coin-m kline %s %s %v", symbol, interval, options)

	req := e.deliveryClient.NewKlinesService().
		Symbol(symbol).
		Interval(string(interval)).
		Limit(limit)

	if options.StartTime != nil {
		req.StartTime(options.StartTime.UnixMilli())
	}
	if options.EndTime != nil {
		req.EndTime(options.EndTime.UnixMilli())
	}

	resp, err := req.Do(ctx)
	if err != nil {
		return nil, err
	}

	var kLines []types.KLine
	for _, k := range resp {
		kLines = append(kLines, types.KLine{
			Exchange:                 types.ExchangeBinance,
			Symbol:                   symbol,
			Interval:                 interval,
			StartTime:                types.NewTimeFromUnix(0, k.OpenTime*int64(time.Millisecond)),
			EndTime:                  types.NewTimeFromUnix(0, k.CloseTime*int64(time.Millisecond)),
			Open:                     fixedpoint.MustNewFromString(k.Open),
			Close:                    fixedpoint.MustNewFromString(k.Close),
			High:                     fixedpoint.MustNewFromString(k.High),
			Low:                      fixedpoint.MustNewFromString(k.Low),
			Volume:                   fixedpoint.MustNewFromString(k.Volume),
			QuoteVolume:              fixedpoint.MustNewFromString(k.QuoteAssetVolume),
			TakerBuyBaseAssetVolume:  fixedpoint.MustNewFromString(k.TakerBuyBaseAssetVolume),
			TakerBuyQuoteAssetVolume: fixedpoint.MustNewFromString(k.TakerBuyQuoteAssetVolume),
			NumberOfTrades:           uint64(k.TradeNum),
			Closed:                   true,
		})
	}
	return types.SortKLinesAscending(kLines), nil
}

// queryDeliveryDepth fetches public depth from dapi (go-binance delivery has no DepthService).
func (e *Exchange) queryDeliveryDepth(
	ctx context.Context, symbol string,
) (snapshot types.SliceOrderBook, finalUpdateID int64, err error) {
	baseURL := e.deliveryClient.BaseURL
	if baseURL == "" {
		baseURL = delivery.BaseApiMainUrl
	}

	u, err := url.Parse(baseURL + "/dapi/v1/depth")
	if err != nil {
		return snapshot, finalUpdateID, err
	}
	q := u.Query()
	q.Set("symbol", symbol)
	q.Set("limit", strconv.Itoa(DefaultFuturesDepthLimit))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return snapshot, finalUpdateID, err
	}

	resp, err := binanceapi.DefaultHttpClient.Do(req)
	if err != nil {
		return snapshot, finalUpdateID, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return snapshot, finalUpdateID, err
	}
	if resp.StatusCode >= 400 {
		return snapshot, finalUpdateID, fmt.Errorf("coin-m depth http %d: %s", resp.StatusCode, string(body))
	}

	var depthResp struct {
		LastUpdateID int64             `json:"lastUpdateId"`
		Bids         []json.RawMessage `json:"bids"`
		Asks         []json.RawMessage `json:"asks"`
	}
	if err := json.Unmarshal(body, &depthResp); err != nil {
		return snapshot, finalUpdateID, err
	}

	// Reuse futures depth conversion via legacy DepthResponse shape.
	var legacy binance.DepthResponse
	legacy.LastUpdateID = depthResp.LastUpdateID
	if err := json.Unmarshal(body, &legacy); err != nil {
		return snapshot, finalUpdateID, err
	}
	return convertDepthLegacy(snapshot, symbol, finalUpdateID, &legacy)
}

func (e *Exchange) setDeliveryLeverage(ctx context.Context, symbol string, leverage int) error {
	_, err := e.deliveryClient.NewChangeLeverageService().
		Symbol(symbol).
		Leverage(leverage).
		Do(ctx)
	return err
}

func (e *Exchange) queryDeliveryPositionRisk(ctx context.Context, symbol ...string) ([]types.PositionRisk, error) {
	req := e.deliveryClient.NewGetPositionRiskService()
	// delivery API filters by pair/marginAsset; fetch all then filter by symbol when needed
	risks, err := req.Do(ctx)
	if err != nil {
		return nil, err
	}

	all := toGlobalDeliveryPositionRisk(risks)
	if len(symbol) == 0 {
		return all, nil
	}

	set := make(map[string]struct{}, len(symbol))
	for _, s := range symbol {
		set[s] = struct{}{}
	}
	filtered := make([]types.PositionRisk, 0, len(symbol))
	for _, p := range all {
		if _, ok := set[p.Symbol]; ok {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}
