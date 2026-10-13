package server

import (
	"math"
	"testing"
	"testing/quick"
	"time"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

// Property / fuzz coverage for L2 OBI + Cont OFI:
// - testing/quick ≈ gopter-style properties
// - native Fuzz* replaces legacy go-fuzz for these pure helpers

func TestQuick_ComputeOBI_Bounded(t *testing.T) {
	f := func(b0, b1, a0, a1 float64) bool {
		clamp := func(v float64) float64 {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return 0
			}
			v = math.Abs(v)
			if v > 1e9 {
				v = 1e9
			}
			return v
		}
		bv0, bv1 := clamp(b0), clamp(b1)
		av0, av1 := clamp(a0), clamp(a1)
		if bv0+bv1+av0+av1 == 0 {
			return true
		}
		bids := types.PriceVolumeSlice{
			{Price: fixedpoint.NewFromFloat(100), Volume: fixedpoint.NewFromFloat(bv0)},
			{Price: fixedpoint.NewFromFloat(99), Volume: fixedpoint.NewFromFloat(bv1)},
		}
		asks := types.PriceVolumeSlice{
			{Price: fixedpoint.NewFromFloat(101), Volume: fixedpoint.NewFromFloat(av0)},
			{Price: fixedpoint.NewFromFloat(102), Volume: fixedpoint.NewFromFloat(av1)},
		}
		obi, bidVol, askVol, ok := computeOBI(bids, asks, 2)
		if !ok {
			return bidVol+askVol <= 0
		}
		if obi < -1.0000001 || obi > 1.0000001 {
			return false
		}
		if math.Abs((bidVol-askVol)/(bidVol+askVol)-obi) > 1e-9 {
			return false
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestQuick_ContOFI_Finite(t *testing.T) {
	f := func(pbp, pbq, pap, paq, cbp, cbq, cap_, caq float64) bool {
		norm := func(v float64) float64 {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return 1
			}
			if v < 0 {
				v = -v
			}
			if v > 1e6 {
				v = 1e6
			}
			return v + 1e-6
		}
		prev := obiBookState{
			bestBidP: norm(pbp), bestBidQ: norm(pbq),
			bestAskP: norm(pap) + norm(pbp), bestAskQ: norm(paq),
		}
		cur := obiBookState{
			bestBidP: norm(cbp), bestBidQ: norm(cbq),
			bestAskP: norm(cap_) + norm(cbp), bestAskQ: norm(caq),
		}
		e := contOFIBest(prev, cur)
		return !math.IsNaN(e) && !math.IsInf(e, 0)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func FuzzComputeOBI(f *testing.F) {
	f.Add(10.0, 5.0, 4.0, 3.0)
	f.Add(0.0, 0.0, 1.0, 1.0)
	f.Add(1e8, 1e8, 1e-8, 1e-8)
	f.Fuzz(func(t *testing.T, b0, b1, a0, a1 float64) {
		for _, v := range []float64{b0, b1, a0, a1} {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1e12 {
				t.Skip()
			}
		}
		bids := types.PriceVolumeSlice{
			{Price: fixedpoint.NewFromFloat(100), Volume: fixedpoint.NewFromFloat(b0)},
			{Price: fixedpoint.NewFromFloat(99), Volume: fixedpoint.NewFromFloat(b1)},
		}
		asks := types.PriceVolumeSlice{
			{Price: fixedpoint.NewFromFloat(101), Volume: fixedpoint.NewFromFloat(a0)},
			{Price: fixedpoint.NewFromFloat(102), Volume: fixedpoint.NewFromFloat(a1)},
		}
		obi, _, _, ok := computeOBI(bids, asks, 2)
		if !ok {
			return
		}
		if obi < -1.0000001 || obi > 1.0000001 {
			t.Fatalf("obi out of range: %v", obi)
		}
	})
}

func FuzzContOFIBest(f *testing.F) {
	f.Add(100.0, 5.0, 101.0, 5.0, 100.0, 8.0, 101.0, 5.0)
	f.Fuzz(func(t *testing.T, pbp, pbq, pap, paq, cbp, cbq, cap_, caq float64) {
		for _, v := range []float64{pbp, pbq, pap, paq, cbp, cbq, cap_, caq} {
			if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > 1e9 {
				t.Skip()
			}
		}
		if pap <= pbp || cap_ <= cbp {
			t.Skip()
		}
		e := contOFIBest(
			obiBookState{bestBidP: pbp, bestBidQ: pbq, bestAskP: pap, bestAskQ: paq},
			obiBookState{bestBidP: cbp, bestBidQ: cbq, bestAskP: cap_, bestAskQ: caq},
		)
		if math.IsNaN(e) || math.IsInf(e, 0) {
			t.Fatalf("non-finite ofi: %v", e)
		}
	})
}

func TestIngestDebounceWritesBack(t *testing.T) {
	sym := "DEBOUNCEOBIUSDT"
	obiStoreMu.Lock()
	delete(obiStore, sym)
	obiStoreMu.Unlock()

	t0 := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	mk := func(bidQ float64, at time.Time) types.SliceOrderBook {
		return types.SliceOrderBook{
			Symbol: sym,
			Time:   at,
			Bids:   types.PriceVolumeSlice{{Price: fixedpoint.NewFromFloat(100), Volume: fixedpoint.NewFromFloat(bidQ)}},
			Asks:   types.PriceVolumeSlice{{Price: fixedpoint.NewFromFloat(101), Volume: fixedpoint.NewFromFloat(5)}},
		}
	}
	ingestOBIBook(sym, mk(10, t0), 5, true)
	got := ingestOBIBook(sym, mk(20, t0.Add(100*time.Millisecond)), 5, false)
	samples := snapshotOBISamples(sym)
	if len(samples) != 1 {
		t.Fatalf("expected 1 sample after debounce, got %d", len(samples))
	}
	if samples[0].OBI != got.OBI {
		t.Fatalf("store OBI %v != returned %v", samples[0].OBI, got.OBI)
	}
	want := (20.0 - 5.0) / 25.0
	if math.Abs(samples[0].OBI-want) > 1e-9 {
		t.Fatalf("expected in-place OBI refresh to %v, got %v", want, samples[0].OBI)
	}
}
