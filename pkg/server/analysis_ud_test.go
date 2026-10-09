package server

import (
	"testing"

	"github.com/c9s/bbgo/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyUDBoxLocksCompressionRange(t *testing.T) {
	ks := synthKLines(40, 100, func(i int, prev float64) (o, h, l, c float64) {
		c = 100 + float64(i%6)*0.4
		return c, c + 0.5, c - 0.5, c
	})
	v := classifyUDBox(types.Interval4h, ks)
	assert.NotEmpty(t, v.Phase)
	assert.Greater(t, v.Last, 0.0)
}

func TestBuildUDCascadeH4BreakWatchesDaily(t *testing.T) {
	h4 := udTFBoxView{Phase: "break_down", Zone: "below", KeySupport: 80345, Locked: true}
	d := udTFBoxView{Phase: "range", Zone: "mid", KeySupport: 80000}
	w := udTFBoxView{Phase: "range", Zone: "mid"}
	c := buildUDCascade(h4, d, w)
	assert.Equal(t, "daily_short_watch", c.Signal)
	assert.True(t, c.WatchDaily)
	assert.Contains(t, c.Label, "日线出空预警")
}

func TestBuildUDCascadeDailyFollowsWeeklyWatch(t *testing.T) {
	h4 := udTFBoxView{Phase: "break_down", Zone: "below", KeySupport: 80345}
	d := udTFBoxView{Phase: "break_down", Zone: "below", KeySupport: 80000}
	w := udTFBoxView{Phase: "range", Zone: "mid"}
	c := buildUDCascade(h4, d, w)
	assert.Equal(t, "daily_short", c.Signal)
	assert.True(t, c.WatchWeekly)
}

func TestBacktestUDCascadeFindsDownBreaks(t *testing.T) {
	// tight range then dump through bottom
	ks := synthKLines(120, 100, func(i int, prev float64) (o, h, l, c float64) {
		switch {
		case i < 50:
			c = 100 + float64(i%5)*0.3
			return c, c + 0.4, c - 0.4, c
		default:
			c = prev * 0.985
			return prev, prev, c * 0.99, c
		}
	})
	dN, _, _, _, _ := backtestUDCascade(ks, 18)
	require.GreaterOrEqual(t, dN, 0)
}

func TestVolumeBiasUpBars(t *testing.T) {
	ks := synthKLines(30, 100, func(i int, prev float64) (o, h, l, c float64) {
		c = prev + 1
		return prev, c, prev, c
	})
	for i := range ks {
		ks[i].Volume = fp(10)
	}
	bias, ratio := volumeBias(ks, 20)
	assert.Equal(t, "红肥绿瘦", bias)
	assert.Greater(t, ratio, 1.0)
}
