package grid2

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/strategy/grid2/grid2types"
	"github.com/c9s/bbgo/pkg/types"
)

func newCoinMTestStrategy() *Strategy {
	s := &Strategy{
		logger: logrus.NewEntry(logrus.New()),
		Market: types.Market{
			Symbol:          "BTCUSD_PERP",
			BaseCurrency:    "BTC",
			QuoteCurrency:   "USD",
			PricePrecision:  1,
			VolumePrecision: 0,
			TickSize:        number(0.1),
			StepSize:        number(1),
			MinQuantity:     number(1),
			MinNotional:     number(100),
			ContractValue:   number(100),
		},
		Symbol:      "BTCUSD_PERP",
		GridNum:     5,
		LowerPrice:  number(90000),
		UpperPrice:  number(110000),
		EarnBase:    true,
		Compound:    false,
		Leverage:    number(5),
		SkipSpreadCheck: true,
	}
	s.QuantityOrAmount.Quantity = number(10)
	s.session = &bbgo.ExchangeSession{
		ExchangeSessionConfig: bbgo.ExchangeSessionConfig{
			Delivery: true,
			Futures:  true,
		},
	}
	return s
}

func TestStrategy_coinMInitialMargin(t *testing.T) {
	s := newCoinMTestStrategy()
	// (10 * 100) / 100000 / 5 = 0.002 BTC
	m := s.coinMInitialMargin(number(10), number(100000))
	assert.InDelta(t, 0.002, m.Float64(), 1e-9)
}

func TestStrategy_calculateProfit_coinM(t *testing.T) {
	s := newCoinMTestStrategy()
	// buy 100000, sell 105000, qty 10, CV 100
	// profit = 100 * 10 * (1/100000 - 1/105000) ≈ 0.000047619 BTC
	profit := s.calculateProfit(types.Order{
		SubmitOrder: types.SubmitOrder{
			Quantity: number(10),
			Price:    number(105000),
		},
	}, number(100000), number(10))

	assert.Equal(t, "BTC", profit.Currency)
	expected := s.Market.ContractValue.Mul(number(10)).Mul(
		fixedpoint.One.Div(number(100000)).Sub(fixedpoint.One.Div(number(105000))),
	)
	assert.Equal(t, expected.String(), profit.Profit.String())
}

func TestStrategy_generateCoinMGridOrders(t *testing.T) {
	s := newCoinMTestStrategy()
	s.grid = grid2types.NewGrid(s.LowerPrice, s.UpperPrice, fixedpoint.NewFromInt(s.GridNum), s.Market.TickSize)
	s.grid.CalculateArithmeticPins()

	// Generous base margin so all pins can be placed
	orders, err := s.generateCoinMGridOrders(number(1.0), number(100000))
	assert.NoError(t, err)
	assert.NotEmpty(t, orders)

	for _, o := range orders {
		assert.Equal(t, number(10), o.Quantity, "coin-m quantity must stay in contracts")
		assert.Equal(t, types.OrderTypeLimit, o.Type)
	}
}

func TestStrategy_validateCoinM(t *testing.T) {
	s := newCoinMTestStrategy()
	assert.NoError(t, s.validateCoinM())

	s.QuantityOrAmount.Quantity = fixedpoint.Zero
	assert.Error(t, s.validateCoinM())

	s.QuantityOrAmount.Quantity = number(10)
	s.Compound = true
	assert.Error(t, s.validateCoinM())
}

func TestStrategy_checkRequiredInvestmentByQuantityCoinM(t *testing.T) {
	s := newCoinMTestStrategy()
	pins := []grid2types.Pin{
		grid2types.Pin(number(90000)),
		grid2types.Pin(number(95000)),
		grid2types.Pin(number(100000)),
		grid2types.Pin(number(105000)),
		grid2types.Pin(number(110000)),
	}

	_, err := s.checkRequiredInvestmentByQuantityCoinM(number(0.0001), number(10), number(100000), pins)
	assert.Error(t, err)

	_, err = s.checkRequiredInvestmentByQuantityCoinM(number(1.0), number(10), number(100000), pins)
	assert.NoError(t, err)
}
