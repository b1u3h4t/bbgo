package bbgo

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/testing/testhelper"
)

func TestPositionExposure(t *testing.T) {
	pe := NewPositionExposure("BTCUSDT")

	// initial value
	assert.Equal(t, testhelper.Number(0), pe.GetNet())
	assert.Equal(t, testhelper.Number(0), pe.GetPending())
	assert.Equal(t, testhelper.Number(0), pe.GetUncovered())

	// open position (maker orders are filled)
	pe.Open(testhelper.Number(2))
	assert.Equal(t, testhelper.Number(2), pe.GetNet())
	assert.Equal(t, testhelper.Number(0), pe.GetPending())
	assert.Equal(t, testhelper.Number(2), pe.GetUncovered())

	// cover 1 (hedge order is placed, and pending is updated)
	pe.Cover(testhelper.Number(1))
	assert.Equal(t, testhelper.Number(2), pe.GetNet())
	assert.Equal(t, testhelper.Number(1), pe.GetPending())
	assert.Equal(t, testhelper.Number(1), pe.GetUncovered())

	// close 1 (hedge order is filled, pending is updated)
	pe.Close(testhelper.Number(-1))
	assert.Equal(t, testhelper.Number(1), pe.GetNet())
	assert.Equal(t, testhelper.Number(0), pe.GetPending())
	assert.Equal(t, testhelper.Number(1), pe.GetUncovered())

	// open 1 (another maker order is filled, pending is updated)
	pe.Open(testhelper.Number(-1))
	assert.Equal(t, testhelper.Number(0), pe.GetNet())
	assert.Equal(t, testhelper.Number(0), pe.GetPending())
	assert.Equal(t, testhelper.Number(0), pe.GetUncovered())
}

// TestPositionExposureCloseGetUncoveredRace guards against a torn read where
// Close updates pending before net, briefly making GetUncovered look non-zero
// and triggering a duplicate hedge from the ticker path.
func TestPositionExposureCloseGetUncoveredRace(t *testing.T) {
	pe := NewPositionExposure("BTCUSDT")
	pe.Open(testhelper.Number(1))
	pe.Cover(testhelper.Number(1))

	var wg sync.WaitGroup
	const goroutines = 8
	const iterations = 1000

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Simulate ticker: only hedge when exposure is not closed.
				if !pe.IsClosed() {
					_ = pe.GetUncovered()
				}
			}
		}()
	}

	// Concurrent close/re-open cycles like trade fills + new exposure.
	for i := 0; i < iterations; i++ {
		pe.Close(testhelper.Number(-1))
		assert.True(t, pe.IsClosed())
		assert.True(t, pe.GetUncovered().IsZero())

		pe.Open(testhelper.Number(1))
		pe.Cover(testhelper.Number(1))
	}

	wg.Wait()
	pe.Close(testhelper.Number(-1))
	assert.True(t, pe.IsClosed())
	assert.True(t, pe.GetUncovered().IsZero())
}
