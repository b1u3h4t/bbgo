//go:build !dnum

package grid2

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/strategy/grid2/grid2types"
	"github.com/c9s/bbgo/pkg/types"
)

func newENAUSDTFuturesStrategy() *Strategy {
	market := types.Market{
		Symbol:          "ENAUSDT",
		BaseCurrency:    "ENA",
		QuoteCurrency:   "USDT",
		PricePrecision:  7,
		VolumePrecision: 0,
		TickSize:        number(0.00001),
		StepSize:        number(1),
		MinQuantity:     number(1),
		MinNotional:     number(5),
	}
	s := &Strategy{
		logger: logrus.NewEntry(logrus.New()),
		Market: market,
		Symbol: "ENAUSDT",
		session: &bbgo.ExchangeSession{
			ExchangeSessionConfig: bbgo.ExchangeSessionConfig{Futures: true},
		},
		Position:        types.NewPositionFromMarket(market),
		GridProfitStats: NewGridProfitStats(market),
	}
	s.Position.Market = market
	return s
}

// ENAUSDT live tape 2026-09-09: four sells then buys. Twin-pin Slack reported ~+5.17
// while Binance realizedPnl summed to ~0. Ensure our notify fields match that split.
func TestENAUSDT_LiveReplay_RealizedVsTwinPin(t *testing.T) {
	s := newENAUSDTFuturesStrategy()

	type fill struct {
		orderID uint64
		side    types.SideType
		qty     float64
		price   float64
		rpnl    float64
	}
	fills := []fill{
		{16481278317, types.SideTypeSell, 4909, 0.16744, 0},
		{16481278284, types.SideTypeSell, 4909, 0.16796, 0},
		{16481278196, types.SideTypeSell, 4909, 0.16849, 0},
		{16481278155, types.SideTypeSell, 4909, 0.16901, 0},
		{16481394625, types.SideTypeBuy, 3810, 0.16849, -1.00965},
		{16481394625, types.SideTypeBuy, 1114, 0.16849, -0.29521},
		{16481369904, types.SideTypeBuy, 2273, 0.16796, 0.602345},
		{16481369904, types.SideTypeBuy, 1288, 0.16796, 0.34132},
		{16481369904, types.SideTypeBuy, 1363, 0.16796, 0.361195},
	}

	var binanceSum float64
	for _, f := range fills {
		binanceSum += f.rpnl
		td := types.Trade{
			OrderID:       f.orderID,
			Symbol:        "ENAUSDT",
			Exchange:      types.ExchangeBinance,
			Side:          f.side,
			Price:         number(f.price),
			Quantity:      number(f.qty),
			QuoteQuantity: number(f.price * f.qty),
			IsFutures:     true,
			FeeCurrency:   "BNB",
			Time:          types.Time(time.Date(2026, 9, 9, 8, 0, 0, 0, time.UTC)),
		}
		profit, _, made := s.Position.AddTrade(td)
		if made {
			s.addOrderPositionProfit(f.orderID, profit)
		}
	}

	buyOrders := []struct {
		id      uint64
		buyPx   float64
		sellPin float64
		qty     float64
	}{
		{16481394625, 0.16849, 0.16901, 4924},
		{16481369904, 0.16796, 0.16849, 4924},
	}

	var sumRealized, sumTwin float64
	for _, bo := range buyOrders {
		o := types.Order{
			OrderID:    bo.id,
			UpdateTime: types.Time(time.Date(2026, 9, 9, 8, 6, 0, 0, time.UTC)),
			SubmitOrder: types.SubmitOrder{
				Symbol: "ENAUSDT", Side: types.SideTypeBuy,
				Price: number(bo.buyPx), Quantity: number(bo.qty),
			},
		}
		g := s.gridProfitFromPositionAvgCost(o, func() *GridProfit {
			return s.calculateProfit(types.Order{
				SubmitOrder: types.SubmitOrder{
					Price: number(bo.sellPin), Quantity: number(bo.qty), Side: types.SideTypeSell,
				},
				UpdateTime: o.UpdateTime,
			}, number(bo.buyPx), number(bo.qty))
		})
		require.NotNil(t, g)
		s.GridProfitStats.AddProfit(g)
		sumRealized += g.Profit.Float64()
		sumTwin += g.TwinPinProfit.Float64()

		t.Logf("round order=%d realized=%f twinPin=%f cumR=%f cumT=%f rounds=%d",
			bo.id, g.Profit.Float64(), g.TwinPinProfit.Float64(),
			g.CumulativeRealized.Float64(), g.CumulativeTwinPin.Float64(), g.ArbitrageCount)

		assert.Equal(t, s.GridProfitStats.ArbitrageCount, g.ArbitrageCount)
		assert.InDelta(t, s.GridProfitStats.TotalQuoteProfit.Float64(), g.CumulativeRealized.Float64(), 1e-6)
		assert.InDelta(t, s.GridProfitStats.TotalTwinPinProfit.Float64(), g.CumulativeTwinPin.Float64(), 1e-6)
		assert.InDelta(t, 2.56, g.TwinPinProfit.Float64(), 0.05)
		assert.NotEqual(t, g.Profit.Float64(), g.TwinPinProfit.Float64(), "realized must differ from twin-pin on this tape")
	}

	assert.InDelta(t, binanceSum, sumRealized, 0.05, "sum realized ≈ Binance realizedPnl")
	assert.InDelta(t, 0.0, sumRealized, 0.05)
	assert.Greater(t, sumTwin, 5.0)
	assert.InDelta(t, sumTwin, s.GridProfitStats.TotalTwinPinProfit.Float64(), 1e-6)
	assert.InDelta(t, sumRealized, s.GridProfitStats.TotalQuoteProfit.Float64(), 1e-6)
}

// Pin-walk using the live ENA grid geometry (same lower/upper/n as production yaml).
// Price path mimics kline oscillation inside the band: climb sell pins then fall back.
func TestENAUSDT_KlineStylePinWalk_RoundNotifyFields(t *testing.T) {
	s := newENAUSDTFuturesStrategy()
	lower, upper := number(0.16377), number(0.16954)
	gridNum := 12
	qty := number(4909)
	s.LowerPrice, s.UpperPrice, s.GridNum = lower, upper, int64(gridNum)
	s.QuantityOrAmount.Quantity = qty

	s.grid = grid2types.NewGrid(lower, upper, fixedpoint.NewFromInt(int64(gridNum)), s.Market.TickSize)
	s.grid.CalculateArithmeticPins()
	pins := s.grid.Pins
	require.GreaterOrEqual(t, len(pins), 8)

	spread := upper.Sub(lower).Div(fixedpoint.NewFromInt(int64(gridNum - 1)))
	prices := make([]fixedpoint.Value, len(pins))
	for i, p := range pins {
		prices[i] = fixedpoint.Value(p)
	}

	// Climb pins 5→8 (sells), then fall 8→6 (buys) — same shape as a 1h kline swing.
	path := []int{5, 6, 7, 8, 7, 6}
	var last fixedpoint.Value
	var orderID uint64 = 1000
	var closed int

	for _, idx := range path {
		px := prices[idx]
		if last.IsZero() {
			last = px
			continue
		}
		orderID++
		switch {
		case px.Compare(last) > 0:
			td := types.Trade{
				OrderID: orderID, Symbol: "ENAUSDT", Exchange: types.ExchangeBinance,
				Side: types.SideTypeSell, Price: px, Quantity: qty,
				QuoteQuantity: px.Mul(qty), IsFutures: true, FeeCurrency: "BNB",
				Time: types.Time(time.Now()),
			}
			profit, _, made := s.Position.AddTrade(td)
			if made {
				s.addOrderPositionProfit(orderID, profit)
			}
		case px.Compare(last) < 0:
			td := types.Trade{
				OrderID: orderID, Symbol: "ENAUSDT", Exchange: types.ExchangeBinance,
				Side: types.SideTypeBuy, Price: px, Quantity: qty,
				QuoteQuantity: px.Mul(qty), IsFutures: true, FeeCurrency: "BNB",
				Time: types.Time(time.Now()),
			}
			profit, _, made := s.Position.AddTrade(td)
			require.True(t, made, "buy should realize against short inventory")
			s.addOrderPositionProfit(orderID, profit)

			sellPin := px.Add(spread)
			o := types.Order{
				OrderID: orderID, UpdateTime: types.Time(time.Now()),
				SubmitOrder: types.SubmitOrder{Symbol: "ENAUSDT", Side: types.SideTypeBuy, Price: px, Quantity: qty},
			}
			g := s.gridProfitFromPositionAvgCost(o, func() *GridProfit {
				return s.calculateProfit(types.Order{
					SubmitOrder: types.SubmitOrder{Price: sellPin, Quantity: qty, Side: types.SideTypeSell},
					UpdateTime:  o.UpdateTime,
				}, px, qty)
			})
			require.NotNil(t, g)
			s.GridProfitStats.AddProfit(g)
			closed++

			assert.False(t, g.TwinPinProfit.IsZero())
			assert.False(t, g.CumulativeTwinPin.IsZero())
			assert.Equal(t, closed, g.ArbitrageCount)
			assert.InDelta(t, spread.Mul(qty).Float64(), g.TwinPinProfit.Float64(), 0.08)
			assert.InDelta(t, s.GridProfitStats.TotalQuoteProfit.Float64(), g.CumulativeRealized.Float64(), 1e-6)
			t.Logf("pin-walk round#%d px=%s realized=%s twin=%s cumR=%s cumT=%s",
				closed, px.String(), g.Profit.String(), g.TwinPinProfit.String(),
				g.CumulativeRealized.String(), g.CumulativeTwinPin.String())
		}
		last = px
	}

	require.GreaterOrEqual(t, closed, 2)
	assert.Equal(t, closed, s.GridProfitStats.ArbitrageCount)
	assert.False(t, s.GridProfitStats.TotalTwinPinProfit.IsZero())
}

// Loads production DB-exported ENAUSDT 1h klines (testdata/ena_usdt_1h.json),
// centers a frequent-style 12-level grid on the series mid, walks closes, and
// checks each completed round exposes realized + twin-pin + cumulative fields.
func TestENAUSDT_FixtureKlines_RoundProfitNotify(t *testing.T) {
	raw, err := os.ReadFile("testdata/ena_usdt_1h.json")
	require.NoError(t, err)

	var rows [][]interface{}
	require.NoError(t, json.Unmarshal(raw, &rows))
	require.GreaterOrEqual(t, len(rows), 20)

	closes := make([]float64, 0, len(rows))
	for _, r := range rows {
		require.GreaterOrEqual(t, len(r), 5)
		c, err := strconv.ParseFloat(fmt.Sprint(r[4]), 64)
		require.NoError(t, err)
		closes = append(closes, c)
	}
	loC, hiC := closes[0], closes[0]
	for _, c := range closes {
		if c < loC {
			loC = c
		}
		if c > hiC {
			hiC = c
		}
	}
	mid := (loC + hiC) / 2
	// ~3.5% band like the live frequent grids — inside observed range
	half := mid * 0.0175
	lower, upper := number(mid-half), number(mid+half)
	gridNum := 12
	qty := number(4909)

	s := newENAUSDTFuturesStrategy()
	s.LowerPrice, s.UpperPrice, s.GridNum = lower, upper, int64(gridNum)
	s.QuantityOrAmount.Quantity = qty
	s.grid = grid2types.NewGrid(lower, upper, fixedpoint.NewFromInt(int64(gridNum)), s.Market.TickSize)
	s.grid.CalculateArithmeticPins()
	pins := make([]float64, len(s.grid.Pins))
	for i, p := range s.grid.Pins {
		pins[i] = fixedpoint.Value(p).Float64()
	}
	spread := (upper.Float64() - lower.Float64()) / float64(gridNum-1)

	var last float64
	var lastPinIdx = -1
	var orderID uint64 = 5000
	var closed int

	nearestPin := func(px float64) int {
		best := 0
		bestDist := math.Abs(pins[0] - px)
		for i := 1; i < len(pins); i++ {
			d := math.Abs(pins[i] - px)
			if d < bestDist {
				bestDist = d
				best = i
			}
		}
		return best
	}

	for _, c := range closes {
		if c < lower.Float64() || c > upper.Float64() {
			last = c
			continue
		}
		idx := nearestPin(c)
		if lastPinIdx < 0 {
			lastPinIdx = idx
			last = c
			continue
		}
		if idx == lastPinIdx {
			last = c
			continue
		}
		// step across each pin between lastPinIdx and idx
		step := 1
		if idx < lastPinIdx {
			step = -1
		}
		for i := lastPinIdx + step; ; i += step {
			orderID++
			px := number(pins[i])
			if step > 0 {
				td := types.Trade{
					OrderID: orderID, Symbol: "ENAUSDT", Exchange: types.ExchangeBinance,
					Side: types.SideTypeSell, Price: px, Quantity: qty,
					QuoteQuantity: px.Mul(qty), IsFutures: true, FeeCurrency: "BNB",
					Time: types.Time(time.Now()),
				}
				profit, _, made := s.Position.AddTrade(td)
				if made {
					s.addOrderPositionProfit(orderID, profit)
				}
			} else {
				// only close if we actually have a short
				if s.Position.GetBase().Sign() >= 0 {
					if i == idx {
						break
					}
					continue
				}
				td := types.Trade{
					OrderID: orderID, Symbol: "ENAUSDT", Exchange: types.ExchangeBinance,
					Side: types.SideTypeBuy, Price: px, Quantity: qty,
					QuoteQuantity: px.Mul(qty), IsFutures: true, FeeCurrency: "BNB",
					Time: types.Time(time.Now()),
				}
				profit, _, made := s.Position.AddTrade(td)
				if !made {
					if i == idx {
						break
					}
					continue
				}
				s.addOrderPositionProfit(orderID, profit)
				sellPin := px.Add(number(spread))
				o := types.Order{
					OrderID: orderID, UpdateTime: types.Time(time.Now()),
					SubmitOrder: types.SubmitOrder{Symbol: "ENAUSDT", Side: types.SideTypeBuy, Price: px, Quantity: qty},
				}
				g := s.gridProfitFromPositionAvgCost(o, func() *GridProfit {
					return s.calculateProfit(types.Order{
						SubmitOrder: types.SubmitOrder{Price: sellPin, Quantity: qty, Side: types.SideTypeSell},
						UpdateTime:  o.UpdateTime,
					}, px, qty)
				})
				require.NotNil(t, g)
				s.GridProfitStats.AddProfit(g)
				closed++
				assert.Equal(t, closed, g.ArbitrageCount)
				assert.False(t, g.TwinPinProfit.IsZero())
				assert.InDelta(t, spread*qty.Float64(), g.TwinPinProfit.Float64(), 0.15)
				assert.InDelta(t, s.GridProfitStats.TotalQuoteProfit.Float64(), g.CumulativeRealized.Float64(), 1e-6)
				assert.InDelta(t, s.GridProfitStats.TotalTwinPinProfit.Float64(), g.CumulativeTwinPin.Float64(), 1e-6)
			}
			if i == idx {
				break
			}
		}
		lastPinIdx = idx
		last = c
		_ = last
	}

	t.Logf("fixture klines=%d band=[%s,%s] closed_rounds=%d realizedCum=%s twinCum=%s",
		len(closes), lower.String(), upper.String(), closed,
		s.GridProfitStats.TotalQuoteProfit.String(), s.GridProfitStats.TotalTwinPinProfit.String())
	require.GreaterOrEqual(t, closed, 1, "expected at least one round inside kline fixture")
	assert.Equal(t, closed, s.GridProfitStats.ArbitrageCount)
}
