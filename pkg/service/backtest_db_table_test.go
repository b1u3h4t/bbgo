package service

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/types"
)

type stubFuturesExchange struct {
	types.Exchange
	settings types.FuturesSettings
}

func (s stubFuturesExchange) Name() types.ExchangeName { return types.ExchangeBinance }
func (s stubFuturesExchange) UseFutures()              {}
func (s stubFuturesExchange) UseIsolatedFutures(string) {}
func (s stubFuturesExchange) UseDelivery()              {}
func (s stubFuturesExchange) GetFuturesSettings() types.FuturesSettings {
	return s.settings
}

func TestTargetKlineTable(t *testing.T) {
	t.Run("spot", func(t *testing.T) {
		ex := stubFuturesExchange{settings: types.FuturesSettings{}}
		assert.Equal(t, "binance_klines", targetKlineTable(ex))
	})
	t.Run("usdt-m futures", func(t *testing.T) {
		ex := stubFuturesExchange{settings: types.FuturesSettings{IsFutures: true}}
		assert.Equal(t, "binance_futures_klines", targetKlineTable(ex))
	})
	t.Run("coin-m delivery", func(t *testing.T) {
		ex := stubFuturesExchange{settings: types.FuturesSettings{IsFutures: true, IsDelivery: true}}
		assert.Equal(t, "binance_delivery_klines", targetKlineTable(ex))
	})
}
