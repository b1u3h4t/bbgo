package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

func cvdFP(v float64) fixedpoint.Value { return fixedpoint.NewFromFloat(v) }

func synthCVDKlines(n int, priceFn, tbRatioFn func(i int) float64) []types.KLine {
	out := make([]types.KLine, n)
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		c := priceFn(i)
		vol := 100.0
		tb := vol * tbRatioFn(i)
		out[i] = types.KLine{
			StartTime:               types.Time(base.Add(time.Duration(i) * time.Hour)),
			EndTime:                 types.Time(base.Add(time.Duration(i+1) * time.Hour)),
			Open:                    cvdFP(c),
			High:                    cvdFP(c * 1.001),
			Low:                     cvdFP(c * 0.999),
			Close:                   cvdFP(c),
			Volume:                  cvdFP(vol),
			TakerBuyBaseAssetVolume: cvdFP(tb),
		}
	}
	return out
}

func TestBuildCVDAnalysis_Rising(t *testing.T) {
	ks := synthCVDKlines(30, func(i int) float64 { return 100 + float64(i)*0.1 }, func(i int) float64 { return 0.65 })
	a := buildCVDAnalysis(ks, true)
	assert.True(t, a.Available)
	assert.Equal(t, "rising", a.SlopeLabel)
	assert.Greater(t, a.BuyPctAvg, 0.6)
	assert.Len(t, a.Bars, 30)
	assert.Greater(t, a.Bars[len(a.Bars)-1].CVD, a.Bars[0].CVD)
}

func TestBuildCVDAnalysis_NoTakerBuy(t *testing.T) {
	ks := synthCVDKlines(20, func(i int) float64 { return 100 }, func(i int) float64 { return 0 })
	a := buildCVDAnalysis(ks, false)
	assert.False(t, a.Available)
	assert.Contains(t, a.Note, "taker buy")
}

func TestDetectCVDDivergence_Bullish(t *testing.T) {
	// Craft two price lows: first higher, second lower; CVD opposite.
	n := 40
	price := make([]float64, n)
	cvd := make([]float64, n)
	for i := 0; i < n; i++ {
		price[i] = 100
		cvd[i] = float64(i)
	}
	// swing low around i=15 at 90, then i=28 at 85; CVD higher at second low
	for i := 13; i <= 17; i++ {
		price[i] = 90 + float64(absInt(i-15))
	}
	price[15] = 90
	for i := 26; i <= 30; i++ {
		price[i] = 85 + float64(absInt(i-28))*0.5
	}
	price[28] = 85
	cvd[15] = 10
	cvd[28] = 40

	div, note := detectCVDDivergence(price, cvd, 40)
	assert.Equal(t, "bullish", div)
	assert.NotEmpty(t, note)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
