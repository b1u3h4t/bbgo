//go:build !dnum

package grid2

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/backtest"
	"github.com/c9s/bbgo/pkg/types"
)

// TestCoinMBacktestRegression drives backtest matching with a Coin-M market and
// checks inverse round-trip profit after sell→buy fills (grid cover-short path).
func TestCoinMBacktestRegression(t *testing.T) {
	market := types.Market{
		Symbol:          "BNBUSD_PERP",
		BaseCurrency:    "BNB",
		QuoteCurrency:   "USD",
		PricePrecision:  2,
		VolumePrecision: 0,
		TickSize:        number(0.01),
		StepSize:        number(1),
		MinQuantity:     number(1),
		MinNotional:     number(10),
		MinAmount:       number(10),
		ContractValue:   number(10),
	}

	account := &types.Account{
		MakerFeeRate: number(0.0002),
		TakerFeeRate: number(0.0005),
	}
	account.UpdateBalances(types.BalanceMap{
		"BNB": {Currency: "BNB", Available: number(1.0)},
	})

	engine := backtest.NewMatching(account, market, number(720.0), number(3))

	var trades []types.Trade
	engine.OnTradeUpdate(func(trade types.Trade) {
		trades = append(trades, trade)
	})

	_, _, err := engine.PlaceOrder(types.SubmitOrder{
		Symbol:   market.Symbol,
		Side:     types.SideTypeSell,
		Type:     types.OrderTypeLimit,
		Quantity: number(1),
		Price:    number(720.80),
	})
	assert.NoError(t, err)

	_, _, err = engine.PlaceOrder(types.SubmitOrder{
		Symbol:   market.Symbol,
		Side:     types.SideTypeBuy,
		Type:     types.OrderTypeLimit,
		Quantity: number(1),
		Price:    number(720.13),
	})
	assert.NoError(t, err)

	bnb, ok := account.Balance("BNB")
	assert.True(t, ok)
	assert.True(t, bnb.Locked.Sign() > 0)

	now := time.Date(2026, 9, 1, 0, 1, 0, 0, time.UTC)
	engine.ProcessKLine(types.KLine{
		Symbol:    market.Symbol,
		Interval:  types.Interval1m,
		StartTime: types.Time(now),
		EndTime:   types.Time(now.Add(time.Minute - time.Millisecond)),
		Open:      number(720.0),
		High:      number(721.0),
		Low:       number(719.9),
		Close:     number(720.9),
	})

	now = now.Add(time.Minute)
	engine.ProcessKLine(types.KLine{
		Symbol:    market.Symbol,
		Interval:  types.Interval1m,
		StartTime: types.Time(now),
		EndTime:   types.Time(now.Add(time.Minute - time.Millisecond)),
		Open:      number(720.9),
		High:      number(720.9),
		Low:       number(720.0),
		Close:     number(720.2),
	})

	assert.GreaterOrEqual(t, len(trades), 2)

	bnb, ok = account.Balance("BNB")
	assert.True(t, ok)
	assert.True(t, bnb.Locked.IsZero())
	assert.InDelta(t, 1.0, bnb.Available.Float64(), 0.01)

	var sellPrice, buyPrice = number(0), number(0)
	for _, tr := range trades {
		assert.Equal(t, "BNB", tr.FeeCurrency)
		assert.True(t, tr.IsFutures)
		switch tr.Side {
		case types.SideTypeSell:
			sellPrice = tr.Price
		case types.SideTypeBuy:
			buyPrice = tr.Price
		}
	}
	assert.False(t, sellPrice.IsZero())
	assert.False(t, buyPrice.IsZero())

	s := &Strategy{
		logger: logrus.NewEntry(logrus.New()),
		Market: market,
		Symbol: market.Symbol,
	}
	profit := s.calculateCoinMRoundTripProfit(buyPrice, sellPrice, number(1), types.Order{
		SubmitOrder: types.SubmitOrder{Quantity: number(1)},
	})
	assert.NotNil(t, profit)
	assert.Equal(t, "BNB", profit.Currency)
	assert.True(t, profit.Profit.Sign() > 0, "sell high / buy low inverse profit: %+v", profit)
}
