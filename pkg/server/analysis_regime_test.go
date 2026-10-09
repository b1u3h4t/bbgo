package server

import (
	"testing"
	"time"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
	"github.com/stretchr/testify/assert"
)

func fp(v float64) fixedpoint.Value { return fixedpoint.NewFromFloat(v) }

func synthKLines(n int, start float64, path func(i int, prev float64) (o, h, l, c float64)) []types.KLine {
	out := make([]types.KLine, n)
	prev := start
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		o, h, l, c := path(i, prev)
		out[i] = types.KLine{
			StartTime: types.Time(t0.Add(time.Duration(i) * 4 * time.Hour)),
			EndTime:   types.Time(t0.Add(time.Duration(i+1) * 4 * time.Hour)),
			Open:      fp(o),
			High:      fp(h),
			Low:       fp(l),
			Close:     fp(c),
			Closed:    true,
		}
		prev = c
	}
	return out
}

func TestPercentileSorted(t *testing.T) {
	assert.Equal(t, 0.0, percentileSorted(nil, 50))
	assert.InDelta(t, 3.0, percentileSorted([]float64{1, 2, 3, 4, 5}, 50), 1e-9)
	assert.InDelta(t, 5.0, percentileSorted([]float64{1, 2, 3, 4, 5}, 100), 1e-9)
}

func TestClassifyTFBull(t *testing.T) {
	ks := synthKLines(80, 100, func(i int, prev float64) (o, h, l, c float64) {
		c = 100 + float64(i)*0.5
		return c - 0.2, c + 0.3, c - 0.4, c
	})
	st := classifyTF(ks)
	assert.Equal(t, "bull", st.Bias)
	assert.True(t, st.AboveEMA20)
	assert.True(t, st.AboveEMA50)
}

func TestBacktestRegimeFindsEpisodes(t *testing.T) {
	// ramp up, then drop 8%, then rebound above EMA20
	ks := synthKLines(120, 100, func(i int, prev float64) (o, h, l, c float64) {
		switch {
		case i < 70:
			c = 100 + float64(i)*0.3
		case i < 78:
			c = prev * 0.985
		default:
			c = prev * 1.012
		}
		h, l = c*1.002, c*0.998
		if i >= 70 && i < 78 {
			l = c * 0.99
		}
		return prev, h, l, c
	})
	pool, _ := backtestRegimeRebounds("TEST", ks, 4)
	assert.GreaterOrEqual(t, pool.Episodes, 1)
	s, f, all, sn, cn := regimeCollectBars(ks)
	assert.Equal(t, len(all), cn)
	assert.Equal(t, len(s)+len(f), cn)
	assert.LessOrEqual(t, sn, cn)
}

func TestNextIntervalClose(t *testing.T) {
	now := time.Date(2026, 3, 20, 6, 30, 0, 0, time.UTC) // mid 4h bar (04:00-08:00)
	next := nextIntervalClose(now, types.Interval4h)
	assert.Equal(t, time.Date(2026, 3, 20, 8, 0, 0, 0, time.UTC), next)
}
