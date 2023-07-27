// Code generated by "requestgen -method GET -responseType .APIResponse -responseDataField Result -url /v5/order/history -type GetOrderHistoriesRequest -responseDataType .OrdersResponse"; DO NOT EDIT.

package bybitapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"time"
)

func (g *GetOrderHistoriesRequest) Category(category Category) *GetOrderHistoriesRequest {
	g.category = category
	return g
}

func (g *GetOrderHistoriesRequest) Symbol(symbol string) *GetOrderHistoriesRequest {
	g.symbol = &symbol
	return g
}

func (g *GetOrderHistoriesRequest) OrderId(orderId string) *GetOrderHistoriesRequest {
	g.orderId = &orderId
	return g
}

func (g *GetOrderHistoriesRequest) OrderFilter(orderFilter string) *GetOrderHistoriesRequest {
	g.orderFilter = &orderFilter
	return g
}

func (g *GetOrderHistoriesRequest) OrderStatus(orderStatus OrderStatus) *GetOrderHistoriesRequest {
	g.orderStatus = &orderStatus
	return g
}

func (g *GetOrderHistoriesRequest) StartTime(startTime time.Time) *GetOrderHistoriesRequest {
	g.startTime = &startTime
	return g
}

func (g *GetOrderHistoriesRequest) EndTime(endTime time.Time) *GetOrderHistoriesRequest {
	g.endTime = &endTime
	return g
}

func (g *GetOrderHistoriesRequest) Limit(limit uint64) *GetOrderHistoriesRequest {
	g.limit = &limit
	return g
}

func (g *GetOrderHistoriesRequest) Cursor(cursor string) *GetOrderHistoriesRequest {
	g.cursor = &cursor
	return g
}

// GetQueryParameters builds and checks the query parameters and returns url.Values
func (g *GetOrderHistoriesRequest) GetQueryParameters() (url.Values, error) {
	var params = map[string]interface{}{}
	// check category field -> json key category
	category := g.category

	// TEMPLATE check-valid-values
	switch category {
	case "spot":
		params["category"] = category

	default:
		return nil, fmt.Errorf("category value %v is invalid", category)

	}
	// END TEMPLATE check-valid-values

	// assign parameter of category
	params["category"] = category
	// check symbol field -> json key symbol
	if g.symbol != nil {
		symbol := *g.symbol

		// assign parameter of symbol
		params["symbol"] = symbol
	} else {
	}
	// check orderId field -> json key orderId
	if g.orderId != nil {
		orderId := *g.orderId

		// assign parameter of orderId
		params["orderId"] = orderId
	} else {
	}
	// check orderFilter field -> json key orderFilter
	if g.orderFilter != nil {
		orderFilter := *g.orderFilter

		// assign parameter of orderFilter
		params["orderFilter"] = orderFilter
	} else {
	}
	// check orderStatus field -> json key orderStatus
	if g.orderStatus != nil {
		orderStatus := *g.orderStatus

		// TEMPLATE check-valid-values
		switch orderStatus {
		case OrderStatusCreated, OrderStatusNew, OrderStatusRejected, OrderStatusPartiallyFilled, OrderStatusPartiallyFilledCanceled, OrderStatusFilled, OrderStatusCancelled, OrderStatusDeactivated, OrderStatusActive:
			params["orderStatus"] = orderStatus

		default:
			return nil, fmt.Errorf("orderStatus value %v is invalid", orderStatus)

		}
		// END TEMPLATE check-valid-values

		// assign parameter of orderStatus
		params["orderStatus"] = orderStatus
	} else {
	}
	// check startTime field -> json key startTime
	if g.startTime != nil {
		startTime := *g.startTime

		// assign parameter of startTime
		// convert time.Time to milliseconds time stamp
		params["startTime"] = strconv.FormatInt(startTime.UnixNano()/int64(time.Millisecond), 10)
	} else {
	}
	// check endTime field -> json key endTime
	if g.endTime != nil {
		endTime := *g.endTime

		// assign parameter of endTime
		// convert time.Time to milliseconds time stamp
		params["endTime"] = strconv.FormatInt(endTime.UnixNano()/int64(time.Millisecond), 10)
	} else {
	}
	// check limit field -> json key limit
	if g.limit != nil {
		limit := *g.limit

		// assign parameter of limit
		params["limit"] = limit
	} else {
	}
	// check cursor field -> json key cursor
	if g.cursor != nil {
		cursor := *g.cursor

		// assign parameter of cursor
		params["cursor"] = cursor
	} else {
	}

	query := url.Values{}
	for _k, _v := range params {
		query.Add(_k, fmt.Sprintf("%v", _v))
	}

	return query, nil
}

// GetParameters builds and checks the parameters and return the result in a map object
func (g *GetOrderHistoriesRequest) GetParameters() (map[string]interface{}, error) {
	var params = map[string]interface{}{}

	return params, nil
}

// GetParametersQuery converts the parameters from GetParameters into the url.Values format
func (g *GetOrderHistoriesRequest) GetParametersQuery() (url.Values, error) {
	query := url.Values{}

	params, err := g.GetParameters()
	if err != nil {
		return query, err
	}

	for _k, _v := range params {
		if g.isVarSlice(_v) {
			g.iterateSlice(_v, func(it interface{}) {
				query.Add(_k+"[]", fmt.Sprintf("%v", it))
			})
		} else {
			query.Add(_k, fmt.Sprintf("%v", _v))
		}
	}

	return query, nil
}

// GetParametersJSON converts the parameters from GetParameters into the JSON format
func (g *GetOrderHistoriesRequest) GetParametersJSON() ([]byte, error) {
	params, err := g.GetParameters()
	if err != nil {
		return nil, err
	}

	return json.Marshal(params)
}

// GetSlugParameters builds and checks the slug parameters and return the result in a map object
func (g *GetOrderHistoriesRequest) GetSlugParameters() (map[string]interface{}, error) {
	var params = map[string]interface{}{}

	return params, nil
}

func (g *GetOrderHistoriesRequest) applySlugsToUrl(url string, slugs map[string]string) string {
	for _k, _v := range slugs {
		needleRE := regexp.MustCompile(":" + _k + "\\b")
		url = needleRE.ReplaceAllString(url, _v)
	}

	return url
}

func (g *GetOrderHistoriesRequest) iterateSlice(slice interface{}, _f func(it interface{})) {
	sliceValue := reflect.ValueOf(slice)
	for _i := 0; _i < sliceValue.Len(); _i++ {
		it := sliceValue.Index(_i).Interface()
		_f(it)
	}
}

func (g *GetOrderHistoriesRequest) isVarSlice(_v interface{}) bool {
	rt := reflect.TypeOf(_v)
	switch rt.Kind() {
	case reflect.Slice:
		return true
	}
	return false
}

func (g *GetOrderHistoriesRequest) GetSlugsMap() (map[string]string, error) {
	slugs := map[string]string{}
	params, err := g.GetSlugParameters()
	if err != nil {
		return slugs, nil
	}

	for _k, _v := range params {
		slugs[_k] = fmt.Sprintf("%v", _v)
	}

	return slugs, nil
}

func (g *GetOrderHistoriesRequest) Do(ctx context.Context) (*OrdersResponse, error) {

	// no body params
	var params interface{}
	query, err := g.GetQueryParameters()
	if err != nil {
		return nil, err
	}

	apiURL := "/v5/order/history"

	req, err := g.client.NewAuthenticatedRequest(ctx, "GET", apiURL, query, params)
	if err != nil {
		return nil, err
	}

	response, err := g.client.SendRequest(req)
	if err != nil {
		return nil, err
	}

	var apiResponse APIResponse
	if err := response.DecodeJSON(&apiResponse); err != nil {
		return nil, err
	}
	var data OrdersResponse
	if err := json.Unmarshal(apiResponse.Result, &data); err != nil {
		return nil, err
	}
	return &data, nil
}
