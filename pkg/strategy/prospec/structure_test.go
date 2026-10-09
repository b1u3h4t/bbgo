package prospec

import (
	"testing"
	"time"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fp(v float64) fixedpoint.Value { return fixedpoint.NewFromFloat(v) }

func synth(n int, path func(i int, prev float64) (o, h, l, c float64)) []types.KLine {
	out := make([]types.KLine, n)
	prev := 100.0
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		o, h, l, c := path(i, prev)
		out[i] = types.KLine{
			StartTime: types.Time(t0.Add(time.Duration(i) * 15 * time.Minute)),
			EndTime:   types.Time(t0.Add(time.Duration(i+1) * 15 * time.Minute)),
			Open:      fp(o), High: fp(h), Low: fp(l), Close: fp(c),
			Closed: true, Interval: types.Interval15m,
		}
		prev = c
	}
	return out
}

func TestFindSwings(t *testing.T) {
	ks := synth(40, func(i int, prev float64) (o, h, l, c float64) {
		c = 100 + float64((i%8)-4)*0.8
		return c, c + 1, c - 1, c
	})
	sw := FindSwings(ks, 2)
	assert.NotEmpty(t, sw)
}

func TestDetectTwoBLong(t *testing.T) {
	ks := synth(50, func(i int, prev float64) (o, h, l, c float64) {
		c = 105
		if i == 40 {
			return 104, 105, 99, 104.5 // pierce low, close back
		}
		return c, c + 0.5, c - 0.5, c
	})
	// plant a swing low before pierce
	ks[30].Low = fp(100)
	ks[29].Low = fp(101)
	ks[31].Low = fp(101)
	tb := DetectTwoB(ks, 2, 20)
	require.NotNil(t, tb)
	assert.Equal(t, "long", tb.Side)
}

func TestBuildSetup123Bear(t *testing.T) {
	nest := NestState{Bias: "bear"}
	o := OneTwoThree{
		Direction: "to_bear", Stage: 3, Confirmed: true,
		Stage1Price: 100, Stage2Price: 108, PriorExtreme: 110,
	}
	s := BuildSetup(nest, o, nil, 98)
	assert.Equal(t, "short", s.Side)
	assert.True(t, s.Aligned)
	assert.Equal(t, "one_two_three", s.Kind)
}

func TestClassifyNestBullish(t *testing.T) {
	// rising staircase with clear swing highs/lows
	ks := synth(80, func(i int, prev float64) (o, h, l, c float64) {
		base := 100 + float64(i/10)*3
		phase := i % 10
		c = base + float64(phase)*0.5
		return c - 0.1, c + 0.8, c - 0.8, c
	})
	st := ClassifyNest(ks, "4h")
	assert.True(t, st.Bias == "bull" || st.Last > st.EMA50, "bias=%s last=%v ema=%v", st.Bias, st.Last, st.EMA50)
}
