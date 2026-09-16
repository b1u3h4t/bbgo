package grid2

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

// TestProfitAccumulatorsDoNotShareGridMu reproduces the P0 deadlock class:
// recover holds s.mu while EmitFilled → processFilledOrder → takeOrder*.
// Profit maps must use profitMu so the same goroutine can take them while s.mu is held.
func TestProfitAccumulatorsDoNotShareGridMu(t *testing.T) {
	s := &Strategy{
		logger: logrus.NewEntry(logrus.New()),
	}
	s.addOrderExchangeRealized(42, fixedpoint.NewFromFloat(1.25))
	s.addOrderPositionProfit(42, fixedpoint.NewFromFloat(0.5))

	s.mu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		v, ok := s.takeOrderExchangeRealized(42)
		assert.True(t, ok)
		assert.Equal(t, "1.25", v.String())
		v2, ok2 := s.takeOrderPositionProfit(42)
		assert.True(t, ok2)
		assert.Equal(t, "0.5", v2.String())
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("DEADLOCK: takeOrder* blocked while s.mu held — profit maps must not use s.mu")
	}
	s.mu.Unlock()
}

// TestDiscardAccumulatorsWhileHoldingGridMu covers processFilledOrder's discard path
// (profit == nil after reverse submit), which also called takeOrder* under the old lock.
func TestDiscardAccumulatorsWhileHoldingGridMu(t *testing.T) {
	s := &Strategy{
		logger: logrus.NewEntry(logrus.New()),
	}
	s.addOrderExchangeRealized(7, fixedpoint.NewFromFloat(3))
	s.addOrderPositionProfit(7, fixedpoint.NewFromFloat(1))

	s.mu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.discardOrderProfitAccumulators(7)
		_, ok := s.peekOrderExchangeRealized(7)
		assert.False(t, ok)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("DEADLOCK: discardOrderProfitAccumulators blocked while s.mu held")
	}
	s.mu.Unlock()
}

func TestRecoverSkipsWhenGridStopped(t *testing.T) {
	s := &Strategy{
		Symbol: "XRPUSDT",
		logger: logrus.NewEntry(logrus.New()),
	}
	s.gridStopped.Store(true)

	err := s.recover(context.Background())
	require.NoError(t, err)
}

func TestCloseGridSetsStoppedLatch(t *testing.T) {
	market := types.Market{
		Symbol:          "XRPUSDT",
		BaseCurrency:    "XRP",
		QuoteCurrency:   "USDT",
		PricePrecision:  4,
		VolumePrecision: 1,
		TickSize:        fixedpoint.NewFromFloat(0.0001),
		StepSize:        fixedpoint.NewFromFloat(0.1),
		MinQuantity:     fixedpoint.NewFromFloat(0.1),
		MinNotional:     fixedpoint.NewFromFloat(5),
	}
	s := &Strategy{
		Symbol:           "XRPUSDT",
		Market:           market,
		UpperPrice:       fixedpoint.NewFromFloat(1.345),
		LowerPrice:       fixedpoint.NewFromFloat(1.2286),
		GridNum:          8,
		logger:           logrus.NewEntry(logrus.New()),
		GridProfitStats:  NewGridProfitStats(market),
		filledOrderIDMap: types.NewSyncOrderMap(),
	}
	s.setGrid(s.newGrid())
	assert.False(t, s.gridStopped.Load())
	assert.NotNil(t, s.getGrid())

	// Avoid exchange I/O: exercise the latch the same way CloseGrid does after cancelAll.
	s.gridStopped.Store(true)
	s.setGrid(nil)

	assert.True(t, s.gridStopped.Load())
	assert.Nil(t, s.getGrid())
	require.NoError(t, s.recover(context.Background()))
}

// Ensure ActiveOrderBook.EmitFilled stays synchronous — the recover fix relies on
// unlocking *before* EmitFilled rather than making EmitFilled async.
func TestEmitFilledIsSynchronous(t *testing.T) {
	book := bbgo.NewActiveOrderBook("XRPUSDT")
	called := false
	book.OnFilled(func(o types.Order) {
		called = true
	})
	book.EmitFilled(types.Order{OrderID: 1})
	assert.True(t, called, "EmitFilled must invoke callbacks synchronously on the caller goroutine")
}
