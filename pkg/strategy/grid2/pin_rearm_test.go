package grid2

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

func TestShouldSkipReverseAfterPinConsumed(t *testing.T) {
	s := &Strategy{Symbol: "NEARUSDT"}
	price := fixedpoint.NewFromFloat(2.469)
	sellFilledAt := time.Date(2026, 9, 9, 8, 56, 6, 0, time.UTC)
	s.notePinSideFilled(types.SideTypeSell, price, sellFilledAt)

	// Duplicate buy created BEFORE the sell pin was consumed → skip reverse.
	dupBuy := types.Order{
		SubmitOrder: types.SubmitOrder{
			Side:  types.SideTypeBuy,
			Price: fixedpoint.NewFromFloat(2.454),
		},
		CreationTime: types.Time(sellFilledAt.Add(-time.Minute)),
	}
	assert.True(t, s.shouldSkipReverseAfterPinConsumed(dupBuy, types.SideTypeSell, price))

	// Legitimate reverse buy created AFTER sell consumed → allow.
	legitBuy := types.Order{
		SubmitOrder: types.SubmitOrder{
			Side:  types.SideTypeBuy,
			Price: fixedpoint.NewFromFloat(2.454),
		},
		CreationTime: types.Time(sellFilledAt.Add(time.Second)),
	}
	assert.False(t, s.shouldSkipReverseAfterPinConsumed(legitBuy, types.SideTypeSell, price))

	// Cleared pin → allow.
	s.clearPinSideFilled(types.SideTypeSell, price)
	assert.False(t, s.shouldSkipReverseAfterPinConsumed(dupBuy, types.SideTypeSell, price))
}

func TestIsPinSideCooling(t *testing.T) {
	s := &Strategy{Symbol: "AVAXUSDT", pinSideRearmTTL: time.Minute}
	price := fixedpoint.NewFromFloat(7.336)
	assert.False(t, s.isPinSideCooling(types.SideTypeSell, price))

	s.notePinSideFilled(types.SideTypeSell, price, time.Now())
	assert.True(t, s.isPinSideCooling(types.SideTypeSell, price))
	assert.False(t, s.isPinSideCooling(types.SideTypeBuy, price), "opposite side not cooling")

	s.clearPinSideFilled(types.SideTypeSell, price)
	assert.False(t, s.isPinSideCooling(types.SideTypeSell, price))
}
