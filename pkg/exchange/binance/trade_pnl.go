package binance

import (
	"database/sql"
	"strconv"
	"strings"
)

// parseExchangeRealizedPnL maps Binance futures realizedPnl / ORDER_TRADE_UPDATE "rp"
// into types.Trade.PnL. Empty strings are treated as missing (Valid=false).
func parseExchangeRealizedPnL(s string) sql.NullFloat64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return sql.NullFloat64{}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: v, Valid: true}
}
