package server

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

func synthTrendKlines(n int, start float64, up bool) []types.KLine {
	out := make([]types.KLine, n)
	p := start
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		drift := 0.0012
		if !up {
			drift = -0.0012
		}
		// Every 40 bars: sharp pullback then resume (to fire RSI / EMA-touch signals)
		phase := i % 40
		move := drift
		if up {
			if phase >= 30 && phase < 35 {
				move = -0.012 // dump
			} else if phase >= 35 {
				move = 0.006 // bounce
			}
		} else {
			if phase >= 30 && phase < 35 {
				move = 0.012
			} else if phase >= 35 {
				move = -0.006
			}
		}
		o := p
		c := p * (1 + move)
		h := math.Max(o, c) * 1.0015
		l := math.Min(o, c) * 0.9985
		out[i] = types.KLine{
			StartTime: types.Time(t0.Add(time.Duration(i) * 15 * time.Minute)),
			EndTime:   types.Time(t0.Add(time.Duration(i+1) * 15 * time.Minute)),
			Open:      fixedpoint.NewFromFloat(o),
			High:      fixedpoint.NewFromFloat(h),
			Low:       fixedpoint.NewFromFloat(l),
			Close:     fixedpoint.NewFromFloat(c),
			Interval:  types.Interval15m,
			Closed:    true,
		}
		p = c
	}
	return out
}

func TestBacktestTrendPullbackRuns(t *testing.T) {
	kl := synthTrendKlines(800, 100, true)
	bt := backtestTrendPullback(kl)
	if bt.Bars != 800 {
		t.Fatalf("bars=%d", bt.Bars)
	}
	// Should produce some trades on trending synthetic data
	total := bt.Long.Trades + bt.Short.Trades
	if total == 0 {
		t.Fatalf("expected some trades, long=%+v short=%+v", bt.Long, bt.Short)
	}
}

func TestIdealEntryFromBTRespectsSide(t *testing.T) {
	st := trendSideStats{MedianPullbackPct: 1.0}
	le, ls, lt := idealEntryFromBT(100, 100, 2, 1, st)
	// entry must stay at signal*(1-pb)=99 even if last is 100 — no chase clamp
	if le > 99.01 || le < 98.99 {
		t.Fatalf("long entry want ~99 got %v", le)
	}
	if ls >= le || lt <= le {
		t.Fatalf("long stops bad entry=%v sl=%v tp=%v", le, ls, lt)
	}
	// last moved up should not pull entry up
	le2, _, _ := idealEntryFromBT(110, 100, 2, 1, st)
	if le2 != le {
		t.Fatalf("entry chased last: %v vs %v", le2, le)
	}
	se, ss, sttp := idealEntryFromBT(100, 100, 2, -1, st)
	if se < 100.99 || se > 101.01 {
		t.Fatalf("short entry want ~101 got %v", se)
	}
	if ss <= se || sttp >= se {
		t.Fatalf("short stops bad entry=%v sl=%v tp=%v", se, ss, sttp)
	}
}

func TestRoundFloatFinite(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   float64
		want float64
	}{
		{name: "inf", in: math.Inf(1), want: 0},
		{name: "neg_inf", in: math.Inf(-1), want: 0},
		{name: "nan", in: math.NaN(), want: 0},
		{name: "normal", in: 1.2345, want: 1.23},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := roundFloat(tc.in, 2)
			if got != tc.want {
				t.Fatalf("roundFloat(%v)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestSummarizeSideJSONSafeWhenLossNearZero(t *testing.T) {
	t.Parallel()
	st := summarizeSide([]trendBTTrade{
		{Side: 1, PnLPct: 12, Win: true, PullbackPct: 0.4},
		{Side: 1, PnLPct: -1e-15, Win: false},
	}, 1)
	if math.IsNaN(st.ProfitFactor) || math.IsInf(st.ProfitFactor, 0) {
		t.Fatalf("profitFactor not finite: %v", st.ProfitFactor)
	}
	if _, err := json.Marshal(st); err != nil {
		t.Fatalf("marshal: %v", err)
	}
}

func TestSideEdgeOK(t *testing.T) {
	if sideEdgeOK(trendSideStats{Trades: 3, Expectancy: 1, ProfitFactor: 2}) {
		t.Fatal("too few trades")
	}
	if sideEdgeOK(trendSideStats{Trades: 20, Expectancy: -0.1, ProfitFactor: 0.9}) {
		t.Fatal("negative expectancy")
	}
	if !sideEdgeOK(trendSideStats{Trades: 20, Expectancy: 0.2, ProfitFactor: 1.2}) {
		t.Fatal("should pass")
	}
}
