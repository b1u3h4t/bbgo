package binanceapi

import (
	"github.com/c9s/requestgen"
)

// FuturesLeverageBracketTier is one notional/leverage tier from GET /fapi/v1/leverageBracket.
type FuturesLeverageBracketTier struct {
	Bracket          int     `json:"bracket"`
	InitialLeverage  int     `json:"initialLeverage"`
	NotionalCap      float64 `json:"notionalCap"`
	NotionalFloor    float64 `json:"notionalFloor"`
	MaintMarginRatio float64 `json:"maintMarginRatio"`
	Cum              float64 `json:"cum"`
}

// FuturesLeverageBracketSymbol groups tiers for one contract.
type FuturesLeverageBracketSymbol struct {
	Symbol   string                       `json:"symbol"`
	Brackets []FuturesLeverageBracketTier `json:"brackets"`
}

//go:generate requestgen -method GET -url "/fapi/v1/leverageBracket" -type FuturesGetLeverageBracketRequest -responseType []FuturesLeverageBracketSymbol
type FuturesGetLeverageBracketRequest struct {
	client requestgen.AuthenticatedAPIClient

	symbol *string `param:"symbol"`
}

func (c *FuturesRestClient) NewFuturesGetLeverageBracketRequest() *FuturesGetLeverageBracketRequest {
	return &FuturesGetLeverageBracketRequest{client: c}
}
