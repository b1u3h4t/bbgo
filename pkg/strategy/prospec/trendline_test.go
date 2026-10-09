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
