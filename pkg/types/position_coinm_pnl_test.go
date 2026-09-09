package types

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/fixedpoint"
)

func TestUnrealizedProfitInverseCoinM(t *testing.T) {
	market := Market{
		Symbol:        "BNBUSD_PERP",
		BaseCurrency:  "BNB",
		QuoteCurrency: "USD",
		ContractValue: fixedpoint.NewFromFloat(10),
	}
	p := NewPositionFromMarket(market)
	_ = p.ModifyBase(fixedpoint.NewFromFloat(-200))
	_ = p.ModifyAverageCost(fixedpoint.NewFromFloat(722.58312319))

	mark := fixedpoint.NewFromFloat(757.4)
	base := p.UnrealizedProfit(mark)
	quote := p.UnrealizedProfitInQuote(mark)

	// short: CV*qty*(1/mark - 1/avg) ≈ -0.127 BNB
	assert.InDelta(t, -0.1272, base.Float64(), 0.001)
	assert.InDelta(t, base.Mul(mark).Float64(), quote.Float64(), 1e-9)
	// Must NOT be the linear nonsense (~-6963)
	assert.Greater(t, quote.Float64(), -200.0)
	assert.Less(t, quote.Float64(), 0.0)
}
