package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

func obiFP(v float64) fixedpoint.Value { return fixedpoint.NewFromFloat(v) }

func TestComputeOBI(t *testing.T) {
	bids := types.PriceVolumeSlice{
		{Price: obiFP(100), Volume: obiFP(10)},
		{Price: obiFP(99), Volume: obiFP(10)},
	}
	asks := types.PriceVolumeSlice{
		{Price: obiFP(101), Volume: obiFP(5)},
		{Price: obiFP(102), Volume: obiFP(5)},
	}
	obi, bidVol, askVol, ok := computeOBI(bids, asks, 2)
	assert.True(t, ok)
	assert.InDelta(t, 20.0, bidVol, 1e-9)
	assert.InDelta(t, 10.0, askVol, 1e-9)
	assert.InDelta(t, (20.0-10.0)/30.0, obi, 1e-9)
}

func TestContOFIBest_BidSizeIncrease(t *testing.T) {
	prev := obiBookState{bestBidP: 100, bestBidQ: 5, bestAskP: 101, bestAskQ: 5}
	cur := obiBookState{bestBidP: 100, bestBidQ: 8, bestAskP: 101, bestAskQ: 5}
	// +3 bid size, ask unchanged → e = +3
	assert.InDelta(t, 3.0, contOFIBest(prev, cur), 1e-9)
}

func TestContOFIBest_AskPriceDown(t *testing.T) {
	prev := obiBookState{bestBidP: 100, bestBidQ: 5, bestAskP: 101, bestAskQ: 5}
	cur := obiBookState{bestBidP: 100, bestBidQ: 5, bestAskP: 100.5, bestAskQ: 7}
	// ask improved (price down) → −q_a^n = -7; bid unchanged → 0
	assert.InDelta(t, -7.0, contOFIBest(prev, cur), 1e-9)
}

func TestContOFIBest_BidPriceUp(t *testing.T) {
	prev := obiBookState{bestBidP: 100, bestBidQ: 5, bestAskP: 101, bestAskQ: 5}
	cur := obiBookState{bestBidP: 100.5, bestBidQ: 4, bestAskP: 101, bestAskQ: 5}
	assert.InDelta(t, 4.0, contOFIBest(prev, cur), 1e-9)
}

func TestIngestAndBuildOBI(t *testing.T) {
	// isolate store key
	sym := "TESTOBIUSDT"
	obiStoreMu.Lock()
	delete(obiStore, sym)
	obiStoreMu.Unlock()

	mkBook := func(bidP, bidQ, askP, askQ float64, ts time.Time) types.SliceOrderBook {
		return types.SliceOrderBook{
			Symbol: sym,
			Time:   ts,
			Bids:   types.PriceVolumeSlice{{Price: obiFP(bidP), Volume: obiFP(bidQ)}},
			Asks:   types.PriceVolumeSlice{{Price: obiFP(askP), Volume: obiFP(askQ)}},
		}
	}
	t0 := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	s1 := ingestOBIBook(sym, mkBook(100, 10, 101, 5, t0), 5, true)
	assert.InDelta(t, (10-5)/15.0, s1.OBI, 1e-9)
	assert.Equal(t, 0.0, s1.OFIDelta) // first sample

	s2 := ingestOBIBook(sym, mkBook(100, 15, 101, 5, t0.Add(2*time.Second)), 5, true)
	assert.InDelta(t, 5.0, s2.OFIDelta, 1e-9) // bid size +5
	assert.InDelta(t, 5.0, s2.CumOFI, 1e-9)

	samples := snapshotOBISamples(sym)
	resp := buildOBIAnalysisFromSamples(samples, s2, 5, nil, true)
	assert.True(t, resp.Available)
	assert.Equal(t, "bid", resp.ImbalanceLabel)
	assert.GreaterOrEqual(t, resp.SampleCount, 2)
	assert.NotEmpty(t, resp.Note)
}

func TestAlignOBIToKlines(t *testing.T) {
	base := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	klines := []types.KLine{
		{
			StartTime: types.Time(base),
			EndTime:   types.Time(base.Add(time.Hour)),
			Close:     obiFP(100),
		},
		{
			StartTime: types.Time(base.Add(time.Hour)),
			EndTime:   types.Time(base.Add(2 * time.Hour)),
			Close:     obiFP(101),
		},
	}
	samples := []obiSample{
		{T: base.Add(10 * time.Minute), OBI: 0.2, OFIDelta: 1, CumOFI: 1},
		{T: base.Add(70 * time.Minute), OBI: -0.1, OFIDelta: -2, CumOFI: -1},
	}
	bars := alignOBIToKlines(samples, klines)
	assert.Len(t, bars, 2)
	assert.True(t, bars[0].Valid)
	assert.InDelta(t, 0.2, bars[0].OBI, 1e-9)
	assert.InDelta(t, 1.0, bars[0].OFIDelta, 1e-9)
	assert.True(t, bars[1].Valid)
	assert.InDelta(t, -0.1, bars[1].OBI, 1e-9)
	assert.InDelta(t, -2.0, bars[1].OFIDelta, 1e-9)
}
