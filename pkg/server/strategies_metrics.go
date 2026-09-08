package server

import (
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/strategy/grid2"
	"github.com/c9s/bbgo/pkg/types"
)

// strategyMetric is the payload expected by the dashboard /strategies page.
type strategyMetric struct {
	ID         string `json:"id"`
	InstanceID string `json:"instanceID"`
	Strategy   string `json:"strategy"`
	Grid       struct {
		Symbol string `json:"symbol"`
	} `json:"grid"`
	Stats struct {
		OneDayArbs   int     `json:"oneDayArbs"`
		TotalArbs    int     `json:"totalArbs"`
		Investment   float64 `json:"investment"`
		TotalProfits float64 `json:"totalProfits"`
		GridProfits  float64 `json:"gridProfits"`
		FloatingPNL  float64 `json:"floatingPNL"`
		CurrentPrice float64 `json:"currentPrice"`
		LowestPrice  float64 `json:"lowestPrice"`
		HighestPrice float64 `json:"highestPrice"`
	} `json:"stats"`
	Status    string `json:"status"`
	StartTime int64  `json:"startTime"`
}

func (s *Server) listStrategiesMetrics(c *gin.Context) {
	metrics := make([]strategyMetric, 0)

	if s.Trader == nil {
		c.JSON(http.StatusOK, gin.H{"data": metrics})
		return
	}

	_ = s.Trader.IterateStrategies(func(st types.StrategyID) error {
		switch g := st.(type) {
		case *grid2.Strategy:
			if m, ok := s.grid2Metric(g); ok {
				metrics = append(metrics, m)
			}
		}
		return nil
	})

	c.JSON(http.StatusOK, gin.H{"data": metrics})
}

func (s *Server) grid2Metric(g *grid2.Strategy) (strategyMetric, bool) {
	var m strategyMetric
	if g == nil || g.Symbol == "" {
		return m, false
	}

	instanceID := g.InstanceID()
	m.ID = instanceID
	m.InstanceID = instanceID
	m.Strategy = "grid2"
	m.Grid.Symbol = g.Symbol
	m.Status = string(types.StrategyStatusRunning)
	m.Stats.LowestPrice = roundFloat(g.LowerPrice.Float64(), 8)
	m.Stats.HighestPrice = roundFloat(g.UpperPrice.Float64(), 8)

	currentPrice := s.resolveLastPrice(g)
	m.Stats.CurrentPrice = roundFloat(currentPrice.Float64(), 8)

	if g.GridProfitStats != nil {
		ps := g.GridProfitStats
		m.Stats.TotalArbs = ps.ArbitrageCount
		m.Stats.OneDayArbs = oneDayArbitrageCount(ps)
		m.Stats.GridProfits = roundFloat(ps.TotalQuoteProfit.Float64(), 4)
		if ps.TotalQuoteProfit.IsZero() && !ps.TotalBaseProfit.IsZero() && !currentPrice.IsZero() {
			m.Stats.GridProfits = roundFloat(ps.TotalBaseProfit.Mul(currentPrice).Float64(), 4)
		}
		if ps.Since != nil {
			m.StartTime = ps.Since.UnixMilli()
		}
	}

	if m.StartTime == 0 {
		m.StartTime = time.Now().UnixMilli()
	}

	m.Stats.Investment = roundFloat(estimateGridInvestment(g, currentPrice), 2)

	if g.Position != nil && !currentPrice.IsZero() {
		m.Stats.FloatingPNL = roundFloat(g.Position.UnrealizedProfit(currentPrice).Float64(), 4)
		m.Stats.TotalProfits = roundFloat(m.Stats.GridProfits+m.Stats.FloatingPNL, 4)
	} else {
		m.Stats.TotalProfits = m.Stats.GridProfits
	}

	return m, true
}

func (s *Server) resolveLastPrice(g *grid2.Strategy) fixedpoint.Value {
	session := g.ExchangeSession
	if session == nil && s.Environ != nil {
		// fall back to the first futures/session that knows this market
		for _, sess := range s.Environ.Sessions() {
			if price, ok := sess.LastPrice(g.Symbol); ok && !price.IsZero() {
				return price
			}
		}
	}
	if session != nil {
		if price, ok := session.LastPrice(g.Symbol); ok && !price.IsZero() {
			return price
		}
	}

	// mid of the configured band
	if !g.UpperPrice.IsZero() && !g.LowerPrice.IsZero() {
		return g.UpperPrice.Add(g.LowerPrice).Div(fixedpoint.NewFromInt(2))
	}
	return fixedpoint.Zero
}

func estimateGridInvestment(g *grid2.Strategy, lastPrice fixedpoint.Value) float64 {
	if !g.QuoteInvestment.IsZero() {
		return g.QuoteInvestment.Float64()
	}

	price := lastPrice
	if price.IsZero() {
		price = g.UpperPrice.Add(g.LowerPrice).Div(fixedpoint.NewFromInt(2))
	}
	if price.IsZero() || g.Quantity.IsZero() || g.GridNum <= 1 {
		return 0
	}

	// approximate notional across (gridNum-1) intervals; for leveraged futures divide by leverage
	notional := g.Quantity.Mul(price).Mul(fixedpoint.NewFromInt(g.GridNum - 1))
	if !g.Leverage.IsZero() && g.Leverage.Compare(fixedpoint.One) > 0 {
		notional = notional.Div(g.Leverage)
	}
	return notional.Float64()
}

func oneDayArbitrageCount(ps *grid2.GridProfitStats) int {
	if ps == nil || ps.DailyNumOfArbitrage == nil {
		return 0
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if n, ok := ps.DailyNumOfArbitrage[today]; ok {
		return n
	}
	// JSON round-trip / map key skew: match by date string
	for t, n := range ps.DailyNumOfArbitrage {
		if t.UTC().Truncate(24 * time.Hour).Equal(today) {
			return n
		}
	}
	return 0
}

func roundFloat(v float64, prec int) float64 {
	if prec < 0 {
		return v
	}
	pow := math.Pow(10, float64(prec))
	return math.Round(v*pow) / pow
}
