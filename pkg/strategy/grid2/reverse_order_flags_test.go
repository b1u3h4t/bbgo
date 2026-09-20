//go:build !dnum

package grid2

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/types"
)

func TestApplyUSDTMReverseOrderFlags_TwoSidedNoReduceOnly(t *testing.T) {
	s := newUSDTMTestStrategy()
	s.LongOnly = false
	s.ShortOnly = false
	s.Position = types.NewPositionFromMarket(s.Market)
	s.Position.Base = number(2226.2)

	order := types.SubmitOrder{
		Side:     types.SideTypeSell,
		Price:    number(1.106),
		Quantity: number(1100),
	}
	got := s.applyUSDTMReverseOrderFlags(order)
	assert.False(t, got.ReduceOnly, "two-sided grids must not force ReduceOnly")
	assert.Equal(t, "1100", got.Quantity.String())
}

func TestApplyUSDTMReverseOrderFlags_LongOnlyReduceWhenClosing(t *testing.T) {
	s := newUSDTMTestStrategy()
	s.LongOnly = true
	s.Position = types.NewPositionFromMarket(s.Market)
	s.Position.Base = number(2226.2)

	order := types.SubmitOrder{
		Side:     types.SideTypeSell,
		Price:    number(1.106),
		Quantity: number(1100),
	}
	got := s.applyUSDTMReverseOrderFlags(order)
	assert.True(t, got.ReduceOnly)
	assert.Equal(t, "1100", got.Quantity.String())

	order.Quantity = number(5000)
	got = s.applyUSDTMReverseOrderFlags(order)
	assert.True(t, got.ReduceOnly)
	assert.True(t, got.Quantity.Compare(number(2226.2)) <= 0)
}

func TestApplyUSDTMReverseOrderFlags_NoReduceWhenOpeningShort(t *testing.T) {
	s := newUSDTMTestStrategy()
	s.LongOnly = true
	s.Position = types.NewPositionFromMarket(s.Market)
	s.Position.Base = number(-500) // already short
	order := types.SubmitOrder{
		Side:     types.SideTypeSell,
		Price:    number(1.13),
		Quantity: number(1100),
	}
	got := s.applyUSDTMReverseOrderFlags(order)
	assert.False(t, got.ReduceOnly)
	assert.Equal(t, "1100", got.Quantity.String())
}
