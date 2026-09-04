package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2/delivery"
	"github.com/pkg/errors"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

// deliveryAccountTrade mirrors Binance GET /dapi/v1/userTrades response.
type deliveryAccountTrade struct {
	Buyer           bool   `json:"buyer"`
	Commission      string `json:"commission"`
	CommissionAsset string `json:"commissionAsset"`
	ID              int64  `json:"id"`
	Maker           bool   `json:"maker"`
	OrderID         int64  `json:"orderId"`
	Price           string `json:"price"`
	Quantity        string `json:"qty"`
	BaseQuantity    string `json:"baseQty"`
	RealizedPnl     string `json:"realizedPnl"`
	Side            string `json:"side"`
	PositionSide    string `json:"positionSide"`
	Symbol          string `json:"symbol"`
	Time            int64  `json:"time"`
}

func (e *Exchange) deliverySignedGET(ctx context.Context, endpoint string, params url.Values) ([]byte, error) {
	if e.deliveryClient == nil {
		return nil, fmt.Errorf("delivery client is not initialized")
	}
	if params == nil {
		params = url.Values{}
	}

	baseURL := e.deliveryClient.BaseURL
	if baseURL == "" {
		baseURL = delivery.BaseApiMainUrl
	}

	apiKey := e.deliveryClient.APIKey
	secret := e.deliveryClient.SecretKey
	if apiKey == "" {
		apiKey = e.key
	}
	if secret == "" {
		secret = e.secret
	}

	ts := time.Now().UnixMilli() - e.deliveryClient.TimeOffset
	params.Set("timestamp", strconv.FormatInt(ts, 10))
	raw := params.Encode()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(raw))
	params.Set("signature", hex.EncodeToString(mac.Sum(nil)))

	u := baseURL + endpoint + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", apiKey)

	client := e.deliveryClient.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("coin-m %s http %d: %s", endpoint, resp.StatusCode, string(body))
	}
	return body, nil
}

func toGlobalDeliveryAccountTrade(t deliveryAccountTrade, isIsolated bool) (*types.Trade, error) {
	side := types.SideTypeSell
	if t.Buyer {
		side = types.SideTypeBuy
	}

	price, err := fixedpoint.NewFromString(t.Price)
	if err != nil {
		return nil, errors.Wrapf(err, "price parse error: %v", t.Price)
	}
	quantity, err := fixedpoint.NewFromString(t.Quantity)
	if err != nil {
		return nil, errors.Wrapf(err, "quantity parse error: %v", t.Quantity)
	}

	// Coin-M: qty is contracts. QuoteQuantity uses price*contracts so Position
	// average-cost math stays linear (avg = sum(pq)/sum(q)). True USD notional
	// is contracts * ContractValue — use Position.DisplayNotional() for UI.
	quoteQuantity := price.Mul(quantity)

	fee, err := fixedpoint.NewFromString(t.Commission)
	if err != nil {
		return nil, errors.Wrapf(err, "commission parse error: %v", t.Commission)
	}

	return &types.Trade{
		ID:            uint64(t.ID),
		OrderID:       uint64(t.OrderID),
		Price:         price,
		Symbol:        t.Symbol,
		Exchange:      "binance",
		Quantity:      quantity,
		QuoteQuantity: quoteQuantity,
		Side:          side,
		IsBuyer:       t.Buyer,
		IsMaker:       t.Maker,
		Fee:           fee,
		FeeCurrency:   t.CommissionAsset,
		Time:          types.Time(millisecondTime(t.Time)),
		IsFutures:     true,
		IsIsolated:    isIsolated,
	}, nil
}

func (e *Exchange) queryDeliveryUserTrades(
	ctx context.Context, symbol string, orderID int64, options *types.TradeQueryOptions,
) ([]types.Trade, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	if orderID > 0 {
		params.Set("orderId", strconv.FormatInt(orderID, 10))
	}
	if options != nil {
		// Binance dapi: fromId cannot be combined with startTime/endTime.
		// Prefer time range (same as USDT-M futures QueryTrades).
		if options.StartTime != nil || options.EndTime != nil {
			if options.StartTime != nil {
				params.Set("startTime", strconv.FormatInt(options.StartTime.UnixMilli(), 10))
			}
			if options.EndTime != nil {
				params.Set("endTime", strconv.FormatInt(options.EndTime.UnixMilli(), 10))
			}
			if options.LastTradeID > 0 {
				log.Warning("coin-m: ignoring LastTradeID because startTime/endTime is set (fromId cannot be combined)")
			}
		} else if options.LastTradeID > 0 {
			params.Set("fromId", strconv.FormatUint(options.LastTradeID, 10))
		}
		if options.Limit > 0 {
			params.Set("limit", strconv.FormatInt(options.Limit, 10))
		}
	}

	body, err := e.deliverySignedGET(ctx, "/dapi/v1/userTrades", params)
	if err != nil {
		return nil, err
	}

	var remote []deliveryAccountTrade
	if err := json.Unmarshal(body, &remote); err != nil {
		return nil, err
	}

	trades := make([]types.Trade, 0, len(remote))
	for _, t := range remote {
		gt, err := toGlobalDeliveryAccountTrade(t, false)
		if err != nil {
			log.WithError(err).Errorf("coin-m: unable to convert trade: %+v", t)
			continue
		}
		trades = append(trades, *gt)
	}
	return types.SortTradesAscending(trades), nil
}

func (e *Exchange) queryDeliveryOrderTrades(ctx context.Context, q types.OrderQuery) ([]types.Trade, error) {
	orderID, err := strconv.ParseInt(q.OrderID, 10, 64)
	if err != nil {
		return nil, err
	}
	return e.queryDeliveryUserTrades(ctx, q.Symbol, orderID, &types.TradeQueryOptions{Limit: 1000})
}

func (e *Exchange) queryDeliveryIncome(
	ctx context.Context, symbol string, incomeType string, startTime, endTime *time.Time,
) ([]types.FundingFee, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	if incomeType != "" {
		params.Set("incomeType", incomeType)
	}
	if startTime != nil {
		params.Set("startTime", strconv.FormatInt(startTime.UnixMilli(), 10))
	}
	if endTime != nil {
		params.Set("endTime", strconv.FormatInt(endTime.UnixMilli(), 10))
	}
	params.Set("limit", "1000")

	body, err := e.deliverySignedGET(ctx, "/dapi/v1/income", params)
	if err != nil {
		return nil, err
	}

	var rows []struct {
		Symbol     string `json:"symbol"`
		IncomeType string `json:"incomeType"`
		Income     string `json:"income"`
		Asset      string `json:"asset"`
		Time       int64  `json:"time"`
		TranID     int64  `json:"tranId"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}

	out := make([]types.FundingFee, 0, len(rows))
	for _, r := range rows {
		out = append(out, types.FundingFee{
			Exchange: e.Name(),
			Symbol:   r.Symbol,
			Asset:    r.Asset,
			Amount:   fixedpoint.MustNewFromString(r.Income),
			Txn:      r.TranID,
			Time:     time.UnixMilli(r.Time),
		})
	}
	return out, nil
}
