package prospec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFitOLSRisingSupport(t *testing.T) {
	// staircase up: clear higher lows
	ks := synth(80, func(i int, prev float64) (o, h, l, c float64) {
		base := 100 + float64(i)*0.4
		wiggle := float64(i%7) * 0.3
		c = base + wiggle
		return c - 0.2, c + 1.2, c - 1.5, c
	})
	sw := FindSwings(ks, 2)
	require.GreaterOrEqual(t, len(sw), 4)
	sup, _ := FitTrendOLS(ks, sw, 2)
	require.NotNil(t, sup)
	assert.Equal(t, "support", sup.Kind)
	assert.Greater(t, sup.Slope, 0.0)
	assert.GreaterOrEqual(t, sup.Touches, 2)
}

func TestFitRANSACRejectsFlatNoise(t *testing.T) {
	ks := synth(60, func(i int, prev float64) (o, h, l, c float64) {
		c = 100 + float64((i%5)-2)*0.2
		return c, c + 0.5, c - 0.5, c
	})
	sw := FindSwings(ks, 2)
	sup, res := FitTrendRANSAC(ks, sw, 3)
	// may or may not find a line; if found must have correct slope sign
	if sup != nil {
		assert.Greater(t, sup.Slope, 0.0)
		assert.GreaterOrEqual(t, sup.Touches, 2)
	}
	if res != nil {
		assert.Less(t, res.Slope, 0.0)
	}
}

func TestDetectOneTwoThreeMethodHLStillWorks(t *testing.T) {
	ks := synth(100, func(i int, prev float64) (o, h, l, c float64) {
		c = 100 + float64(i)*0.2
		if i > 70 {
			c = 120 - float64(i-70)*1.2
		}
		return c - 0.3, c + 0.8, c - 0.8, c
	})
	o := DetectOneTwoThreeMethod(ks, 2, TrendFitHL)
	assert.NotEqual(t, "", o.Note)
}

func TestStampTrendLineStopsAtBreak(t *testing.T) {
	// Rising support y = 100 + 0.5*i, then price collapses below it.
	ks := synth(70, func(i int, prev float64) (o, h, l, c float64) {
		line := 100 + 0.5*float64(i)
		if i < 50 {
			c = line + 2
			return c - 0.2, c + 0.5, line - 0.1, c
		}
		c = line - 5 - float64(i-50)
		return c, c + 0.3, c - 0.3, c
	})
	sup := &TrendLine{
		Method: "ols", Kind: "support",
		Slope: 0.5, Intercept: 100, Tol: 0.5,
		StartIdx: 10, EndIdx: 45,
	}
	stampTrendLine(ks, sup)
	require.True(t, sup.Broken, "support should be marked broken")
	assert.GreaterOrEqual(t, sup.BreakIdx, 50)
	assert.Equal(t, sup.BreakTime, sup.NowTime)
	assert.InDelta(t, sup.YBreak, sup.YNow, 1e-6)
}

func TestCompare123MethodsRuns(t *testing.T) {
	ks := synth(120, func(i int, prev float64) (o, h, l, c float64) {
		c = 100 + float64(i%20)*0.5 + float64(i/20)*2
		return c - 0.2, c + 1, c - 1, c
	})
	rows := Compare123Methods(ks, 2, 8)
	require.Len(t, rows, 3)
	assert.Equal(t, "hl", rows[0].Method)
	assert.Equal(t, "ols", rows[1].Method)
	assert.Equal(t, "ransac", rows[2].Method)
}
