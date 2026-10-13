package server

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/c9s/bbgo/pkg/types"
)

// cvdBar is one period of approximate CVD from Binance-style kline taker buy.
type cvdBar struct {
	T      string  `json:"t"`
	Delta  float64 `json:"delta"`
	CVD    float64 `json:"cvd"`
	BuyPct float64 `json:"buyPct"` // taker buy / volume in [0,1]
	Close  float64 `json:"close"`
}

// cvdAnalysisResp summarizes slope / divergence for the dashboard.
type cvdAnalysisResp struct {
	Available      bool     `json:"available"`
	Method         string   `json:"method"`
	Note           string   `json:"note"`
	SlopeLabel     string   `json:"slopeLabel"` // rising | falling | flat
	SlopeBars      int      `json:"slopeBars"`
	SlopeDelta     float64  `json:"slopeDelta"` // CVD change over slope window
	BuyPctLast     float64  `json:"buyPctLast"` // last bar taker-buy share
	BuyPctAvg      float64  `json:"buyPctAvg"`  // window average
	Divergence     string   `json:"divergence"` // none | bullish | bearish
	DivergenceNote string   `json:"divergenceNote"`
	Bars           []cvdBar `json:"bars,omitempty"`
}

func serializeKlinesWithTaker(klines []types.KLine) []gin.H {
	out := make([]gin.H, 0, len(klines))
	for _, k := range klines {
		out = append(out, gin.H{
			"t":  k.StartTime.Time().UTC().Format(time.RFC3339),
			"o":  roundFloat(k.Open.Float64(), 8),
			"h":  roundFloat(k.High.Float64(), 8),
			"l":  roundFloat(k.Low.Float64(), 8),
			"c":  roundFloat(k.Close.Float64(), 8),
			"v":  roundFloat(k.Volume.Float64(), 8),
			"tb": roundFloat(k.TakerBuyBaseAssetVolume.Float64(), 8),
		})
	}
	return out
}

// buildCVDAnalysis computes approximate CVD = cumsum(takerBuy - takerSell)
// where takerSell ≈ volume - takerBuy (Binance kline fields).
func buildCVDAnalysis(klines []types.KLine, includeBars bool) cvdAnalysisResp {
	resp := cvdAnalysisResp{
		Method:     "binance_kline_taker_buy",
		Divergence: "none",
		SlopeLabel: "flat",
		SlopeBars:  8,
	}
	if len(klines) < 3 {
		resp.Note = "K 线不足，无法计算 CVD"
		return resp
	}

	n := len(klines)
	deltas := make([]float64, n)
	cvds := make([]float64, n)
	buyPcts := make([]float64, n)
	closes := make([]float64, n)
	var sumTB, sumVol float64
	var cvd float64
	nonzeroTB := 0

	for i, k := range klines {
		vol := k.Volume.Float64()
		tb := k.TakerBuyBaseAssetVolume.Float64()
		if tb < 0 {
			tb = 0
		}
		if vol > 0 && tb > vol {
			tb = vol
		}
		ts := vol - tb
		delta := tb - ts // == 2*tb - vol
		deltas[i] = delta
		cvd += delta
		cvds[i] = cvd
		closes[i] = k.Close.Float64()
		if vol > 0 {
			buyPcts[i] = tb / vol
			sumTB += tb
			sumVol += vol
		}
		if tb > 0 {
			nonzeroTB++
		}
	}

	if nonzeroTB == 0 {
		resp.Note = "无 taker buy 数据（库内可能为 0）；请用交易所实时 K 线或重新同步"
		return resp
	}
	resp.Available = true
	resp.BuyPctLast = roundFloat(buyPcts[n-1], 4)
	if sumVol > 0 {
		resp.BuyPctAvg = roundFloat(sumTB/sumVol, 4)
	}

	slopeN := resp.SlopeBars
	if slopeN > n-1 {
		slopeN = n - 1
	}
	resp.SlopeBars = slopeN
	resp.SlopeDelta = roundFloat(cvds[n-1]-cvds[n-1-slopeN], 8)

	// Normalize slope against recent |CVD| range so labels are stable across symbols.
	lo, hi := cvds[n-1-slopeN], cvds[n-1-slopeN]
	for i := n - slopeN; i < n; i++ {
		if cvds[i] < lo {
			lo = cvds[i]
		}
		if cvds[i] > hi {
			hi = cvds[i]
		}
	}
	rng := hi - lo
	if rng < 1e-12 {
		// fall back to abs volume scale
		rng = math.Abs(cvds[n-1]) + 1
	}
	rel := resp.SlopeDelta / rng
	switch {
	case rel > 0.15:
		resp.SlopeLabel = "rising"
	case rel < -0.15:
		resp.SlopeLabel = "falling"
	default:
		resp.SlopeLabel = "flat"
	}

	div, divNote := detectCVDDivergence(closes, cvds, 24)
	resp.Divergence = div
	resp.DivergenceNote = divNote

	resp.Note = formatCVDNote(&resp)

	if includeBars {
		bars := make([]cvdBar, 0, n)
		for i, k := range klines {
			bars = append(bars, cvdBar{
				T:      k.StartTime.Time().UTC().Format(time.RFC3339),
				Delta:  roundFloat(deltas[i], 8),
				CVD:    roundFloat(cvds[i], 8),
				BuyPct: roundFloat(buyPcts[i], 4),
				Close:  roundFloat(closes[i], 8),
			})
		}
		resp.Bars = bars
	}
	return resp
}

func formatCVDNote(a *cvdAnalysisResp) string {
	slopeCN := map[string]string{
		"rising":  "上升",
		"falling": "下降",
		"flat":    "走平",
	}[a.SlopeLabel]
	if slopeCN == "" {
		slopeCN = a.SlopeLabel
	}
	buy := fmt.Sprintf("近窗主动买占比 %.0f%%", a.BuyPctAvg*100)
	base := fmt.Sprintf("CVD 近 %d 根%s；%s", a.SlopeBars, slopeCN, buy)
	switch a.Divergence {
	case "bullish":
		return base + "；价格创新低而 CVD 抬高 → 底背离（偏吸筹，需价格确认）"
	case "bearish":
		return base + "；价格创新高而 CVD 走低 → 顶背离（偏出货，需价格确认）"
	default:
		if a.SlopeLabel == "rising" {
			return base + "；主动买净流入占优"
		}
		if a.SlopeLabel == "falling" {
			return base + "；主动卖净流出占优"
		}
		return base + "；多空力量接近"
	}
}

// detectCVDDivergence looks for simple swing divergence in the last lookback bars.
func detectCVDDivergence(price, cvd []float64, lookback int) (string, string) {
	n := len(price)
	if n < 10 || len(cvd) != n {
		return "none", ""
	}
	if lookback > n {
		lookback = n
	}
	start := n - lookback

	// Find two swing lows / highs via local extrema (window=2).
	type swing struct {
		i int
		v float64
	}
	var priceLows, priceHighs []swing
	for i := start + 2; i < n-2; i++ {
		// Strict extrema so flat stretches do not spam fake swings.
		if price[i] < price[i-1] && price[i] < price[i-2] &&
			price[i] < price[i+1] && price[i] < price[i+2] {
			priceLows = append(priceLows, swing{i, price[i]})
		}
		if price[i] > price[i-1] && price[i] > price[i-2] &&
			price[i] > price[i+1] && price[i] > price[i+2] {
			priceHighs = append(priceHighs, swing{i, price[i]})
		}
	}

	if len(priceLows) >= 2 {
		a := priceLows[len(priceLows)-2]
		b := priceLows[len(priceLows)-1]
		if b.v < a.v*(1-0.0005) && cvd[b.i] > cvd[a.i] {
			return "bullish", "价格低点下移、CVD 低点抬高"
		}
	}
	if len(priceHighs) >= 2 {
		a := priceHighs[len(priceHighs)-2]
		b := priceHighs[len(priceHighs)-1]
		if b.v > a.v*(1+0.0005) && cvd[b.i] < cvd[a.i] {
			return "bearish", "价格高点上移、CVD 高点降低"
		}
	}
	return "none", ""
}

func (s *Server) analysisCVD(c *gin.Context) {
	session, ok := s.sessionOrAbort(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	symbol := strings.ToUpper(strings.TrimSpace(c.DefaultQuery("symbol", "BTCUSDT")))
	if symbol == "" {
		symbol = "BTCUSDT"
	}
	interval, limit := parseChartInterval(c.DefaultQuery("interval", "1h"), c.DefaultQuery("limit", ""))

	klines, klineSource, err := s.queryAnalysisKLines(ctx, session, symbol, interval, limit)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	cvd := buildCVDAnalysis(klines, true)
	c.JSON(200, gin.H{
		"symbol":      symbol,
		"interval":    string(interval),
		"klineSource": klineSource,
		"cvd":         cvd,
	})
}
