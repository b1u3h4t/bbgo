package server

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/c9s/bbgo/pkg/types"
)

const (
	obiDefaultLevels   = 10
	obiMaxSamples      = 2000
	obiMinSampleGap    = 800 * time.Millisecond // avoid hammering on rapid refresh
)

// depthQueryService is satisfied by Binance (and others) QueryDepth.
type depthQueryService interface {
	QueryDepth(ctx context.Context, symbol string) (types.SliceOrderBook, int64, error)
}

type obiBookState struct {
	bestBidP, bestBidQ float64
	bestAskP, bestAskQ float64
	bidVol, askVol     float64
	obi                float64
	ts                 time.Time
}

type obiSample struct {
	T        time.Time
	OBI      float64
	OFIDelta float64
	CumOFI   float64
	BidVol   float64
	AskVol   float64
}

type obiTracker struct {
	prev    *obiBookState
	cumOFI  float64
	samples []obiSample
	lastAt  time.Time
}

var (
	obiStoreMu sync.Mutex
	obiStore   = map[string]*obiTracker{}
)

type obiBar struct {
	T        string  `json:"t"`
	OBI      float64 `json:"obi"`
	OFIDelta float64 `json:"ofiDelta"`
	OFI      float64 `json:"ofi"`
	Close    float64 `json:"close,omitempty"`
	Valid    bool    `json:"valid"` // false = no depth sample yet for this window
}

// obiAnalysisResp summarizes L2 OBI + Cont-style approximate OFI.
type obiAnalysisResp struct {
	Available      bool     `json:"available"`
	Method         string   `json:"method"`
	Note           string   `json:"note"`
	Levels         int      `json:"levels"`
	OBI            float64  `json:"obi"`            // [-1,1]
	BidVol         float64  `json:"bidVol"`
	AskVol         float64  `json:"askVol"`
	BestBid        float64  `json:"bestBid"`
	BestAsk        float64  `json:"bestAsk"`
	OFIDelta       float64  `json:"ofiDelta"`       // last sample Cont e_n
	CumOFI         float64  `json:"cumOFI"`         // process-local cumulative OFI
	SampleCount    int      `json:"sampleCount"`
	SlopeLabel     string   `json:"slopeLabel"` // rising | falling | flat (on cum OFI)
	SlopeBars      int      `json:"slopeBars"`
	SlopeDelta     float64  `json:"slopeDelta"`
	ImbalanceLabel string   `json:"imbalanceLabel"` // bid | ask | neutral
	Bars           []obiBar `json:"bars,omitempty"`
}

func sumSideVol(side types.PriceVolumeSlice, levels int) float64 {
	if levels <= 0 {
		levels = obiDefaultLevels
	}
	n := len(side)
	if n > levels {
		n = levels
	}
	var sum float64
	for i := 0; i < n; i++ {
		sum += side[i].Volume.Float64()
	}
	return sum
}

// computeOBI returns (bid−ask)/(bid+ask) over the top levels. Empty book → 0, false.
func computeOBI(bids, asks types.PriceVolumeSlice, levels int) (obi, bidVol, askVol float64, ok bool) {
	bidVol = sumSideVol(bids, levels)
	askVol = sumSideVol(asks, levels)
	den := bidVol + askVol
	if den <= 0 {
		return 0, bidVol, askVol, false
	}
	return (bidVol - askVol) / den, bidVol, askVol, true
}

// contOFIBest computes Cont et al. order-flow imbalance contribution from consecutive
// best bid/ask quotes (L2 approximation; not true MBO).
func contOFIBest(prev, cur obiBookState) float64 {
	var e float64
	// Bid contribution
	switch {
	case cur.bestBidP > prev.bestBidP:
		e += cur.bestBidQ
	case cur.bestBidP == prev.bestBidP:
		e += cur.bestBidQ - prev.bestBidQ
	default: // bid price down → previous size canceled / traded through
		e -= prev.bestBidQ
	}
	// Ask contribution
	switch {
	case cur.bestAskP < prev.bestAskP:
		e -= cur.bestAskQ
	case cur.bestAskP == prev.bestAskP:
		e -= cur.bestAskQ - prev.bestAskQ
	default: // ask price up → previous ask withdrawn
		e += prev.bestAskQ
	}
	return e
}

func bookStateFrom(book types.SliceOrderBook, levels int) (obiBookState, bool) {
	bb, okB := book.BestBid()
	ba, okA := book.BestAsk()
	if !okB || !okA {
		return obiBookState{}, false
	}
	obi, bidVol, askVol, ok := computeOBI(book.Bids, book.Asks, levels)
	if !ok {
		return obiBookState{}, false
	}
	ts := book.Time
	if ts.IsZero() {
		ts = time.Now()
	}
	return obiBookState{
		bestBidP: bb.Price.Float64(),
		bestBidQ: bb.Volume.Float64(),
		bestAskP: ba.Price.Float64(),
		bestAskQ: ba.Volume.Float64(),
		bidVol:   bidVol,
		askVol:   askVol,
		obi:      obi,
		ts:       ts,
	}, true
}

func getOBITracker(symbol string) *obiTracker {
	obiStoreMu.Lock()
	defer obiStoreMu.Unlock()
	t := obiStore[symbol]
	if t == nil {
		t = &obiTracker{}
		obiStore[symbol] = t
	}
	return t
}

// ingestOBIBook updates the per-symbol tracker from a fresh L2 snapshot.
func ingestOBIBook(symbol string, book types.SliceOrderBook, levels int, force bool) obiSample {
	st, ok := bookStateFrom(book, levels)
	if !ok {
		return obiSample{}
	}
	tr := getOBITracker(symbol)
	obiStoreMu.Lock()
	defer obiStoreMu.Unlock()

	now := st.ts
	if !force && !tr.lastAt.IsZero() && now.Sub(tr.lastAt) < obiMinSampleGap {
		// Within debounce: refresh OBI readout in-place without a new OFI tick.
		if n := len(tr.samples); n > 0 {
			tr.samples[n-1].OBI = st.obi
			tr.samples[n-1].BidVol = st.bidVol
			tr.samples[n-1].AskVol = st.askVol
			// Keep Cont baseline current so the next accepted sample does not
			// invent a giant OFI jump across the skipped window.
			cp := st
			tr.prev = &cp
			return tr.samples[n-1]
		}
	}

	var delta float64
	if tr.prev != nil {
		delta = contOFIBest(*tr.prev, st)
		tr.cumOFI += delta
	}
	cp := st
	tr.prev = &cp
	tr.lastAt = now
	s := obiSample{
		T:        now,
		OBI:      st.obi,
		OFIDelta: delta,
		CumOFI:   tr.cumOFI,
		BidVol:   st.bidVol,
		AskVol:   st.askVol,
	}
	tr.samples = append(tr.samples, s)
	if len(tr.samples) > obiMaxSamples {
		tr.samples = tr.samples[len(tr.samples)-obiMaxSamples:]
	}
	return s
}

func snapshotOBISamples(symbol string) []obiSample {
	tr := getOBITracker(symbol)
	obiStoreMu.Lock()
	defer obiStoreMu.Unlock()
	out := make([]obiSample, len(tr.samples))
	copy(out, tr.samples)
	return out
}

func imbalanceLabel(obi float64) string {
	switch {
	case obi > 0.15:
		return "bid"
	case obi < -0.15:
		return "ask"
	default:
		return "neutral"
	}
}

func formatOBINote(a *obiAnalysisResp) string {
	imbCN := map[string]string{
		"bid":     "买盘厚",
		"ask":     "卖盘厚",
		"neutral": "多空接近",
	}[a.ImbalanceLabel]
	slopeCN := map[string]string{
		"rising":  "上升",
		"falling": "下降",
		"flat":    "走平",
	}[a.SlopeLabel]
	base := fmt.Sprintf("OBI(L%d)=%.2f（%s）；L2近似OFI近%d样%s",
		a.Levels, a.OBI, imbCN, a.SlopeBars, slopeCN)
	switch {
	case a.ImbalanceLabel == "bid" && a.SlopeLabel == "rising":
		return base + "；挂单买压+主动买流同向"
	case a.ImbalanceLabel == "ask" && a.SlopeLabel == "falling":
		return base + "；挂单卖压+主动卖流同向"
	case a.SampleCount < 3:
		return base + "；样本尚少，刷新页面可累积 Cont OFI"
	default:
		return base + "；非 MBO，Binance L2 近似"
	}
}

func buildOBIAnalysisFromSamples(
	samples []obiSample,
	last obiSample,
	levels int,
	klines []types.KLine,
	includeBars bool,
) obiAnalysisResp {
	resp := obiAnalysisResp{
		Method:         "binance_l2_depth_cont_ofi",
		Levels:         levels,
		SlopeLabel:     "flat",
		SlopeBars:      8,
		ImbalanceLabel: "neutral",
	}
	if last.T.IsZero() && len(samples) == 0 {
		resp.Note = "无深度快照，无法计算 OBI/OFI"
		return resp
	}
	if last.T.IsZero() && len(samples) > 0 {
		last = samples[len(samples)-1]
	}

	resp.Available = true
	resp.OBI = roundFloat(last.OBI, 4)
	resp.BidVol = roundFloat(last.BidVol, 4)
	resp.AskVol = roundFloat(last.AskVol, 4)
	resp.OFIDelta = roundFloat(last.OFIDelta, 4)
	resp.CumOFI = roundFloat(last.CumOFI, 4)
	resp.SampleCount = len(samples)
	resp.ImbalanceLabel = imbalanceLabel(last.OBI)

	// Slope on cumulative OFI over last slopeBars samples.
	n := len(samples)
	slopeN := resp.SlopeBars
	if slopeN > n-1 {
		slopeN = n - 1
	}
	if slopeN < 0 {
		slopeN = 0
	}
	resp.SlopeBars = slopeN
	if slopeN > 0 {
		resp.SlopeDelta = roundFloat(samples[n-1].CumOFI-samples[n-1-slopeN].CumOFI, 4)
		lo, hi := samples[n-1-slopeN].CumOFI, samples[n-1-slopeN].CumOFI
		for i := n - slopeN; i < n; i++ {
			v := samples[i].CumOFI
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
		rng := hi - lo
		if rng < 1e-12 {
			rng = math.Abs(samples[n-1].CumOFI) + 1
		}
		rel := resp.SlopeDelta / rng
		switch {
		case rel > 0.15:
			resp.SlopeLabel = "rising"
		case rel < -0.15:
			resp.SlopeLabel = "falling"
		}
	}

	resp.Note = formatOBINote(&resp)

	if includeBars {
		if len(klines) >= 2 {
			resp.Bars = alignOBIToKlines(samples, klines)
		} else {
			bars := make([]obiBar, 0, len(samples))
			for _, s := range samples {
				bars = append(bars, obiBar{
					T:        s.T.UTC().Format(time.RFC3339),
					OBI:      roundFloat(s.OBI, 4),
					OFIDelta: roundFloat(s.OFIDelta, 4),
					OFI:      roundFloat(s.CumOFI, 4),
					Valid:    true,
				})
			}
			resp.Bars = bars
		}
	}
	return resp
}

// alignOBIToKlines buckets samples into each kline window (mean OBI, sum ΔOFI, last cumOFI).
func alignOBIToKlines(samples []obiSample, klines []types.KLine) []obiBar {
	if len(klines) == 0 {
		return nil
	}
	bars := make([]obiBar, 0, len(klines))
	si := 0
	var lastOBI, lastOFI float64
	have := false
	for _, k := range klines {
		start := k.StartTime.Time()
		end := k.EndTime.Time()
		if end.Before(start) || end.Equal(start) {
			end = start.Add(time.Hour) // fallback
		}
		var sumOBI, sumDelta float64
		var cnt int
		for si < len(samples) && samples[si].T.Before(start) {
			si++
		}
		j := si
		for j < len(samples) && !samples[j].T.After(end) {
			sumOBI += samples[j].OBI
			sumDelta += samples[j].OFIDelta
			lastOFI = samples[j].CumOFI
			lastOBI = samples[j].OBI
			have = true
			cnt++
			j++
		}
		si = j
		bar := obiBar{
			T:     start.UTC().Format(time.RFC3339),
			Close: roundFloat(k.Close.Float64(), 8),
		}
		if cnt > 0 {
			bar.Valid = true
			bar.OBI = roundFloat(sumOBI/float64(cnt), 4)
			bar.OFIDelta = roundFloat(sumDelta, 4)
			bar.OFI = roundFloat(lastOFI, 4)
		} else if have {
			// carry forward last known OBI so the line is continuous; Δ=0
			bar.Valid = true
			bar.OBI = roundFloat(lastOBI, 4)
			bar.OFI = roundFloat(lastOFI, 4)
			bar.OFIDelta = 0
		}
		bars = append(bars, bar)
	}
	return bars
}

func parseOBILevels(raw string) int {
	levels := obiDefaultLevels
	if raw != "" {
		if v, err := parsePositiveInt(raw); err == nil && v > 0 {
			levels = v
			if levels > 50 {
				levels = 50
			}
		}
	}
	return levels
}

func parsePositiveInt(s string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("invalid int")
	}
	return v, nil
}

// sampleAndBuildOBIOnce queries depth at most once.
func (s *Server) sampleAndBuildOBIOnce(
	ctx context.Context,
	ex types.Exchange,
	symbol string,
	levels int,
	klines []types.KLine,
	includeBars bool,
) obiAnalysisResp {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	svc, ok := ex.(depthQueryService)
	if !ok {
		return obiAnalysisResp{
			Method: "binance_l2_depth_cont_ofi",
			Levels: levels,
			Note:   "当前交易所未实现 QueryDepth，无法计算 OBI/OFI",
		}
	}
	book, _, err := svc.QueryDepth(ctx, symbol)
	if err != nil {
		samples := snapshotOBISamples(symbol)
		resp := buildOBIAnalysisFromSamples(samples, obiSample{}, levels, klines, includeBars)
		if !resp.Available {
			resp.Note = "深度查询失败: " + err.Error()
		} else {
			resp.Note = "深度刷新失败，使用缓存样本；" + resp.Note
		}
		return resp
	}
	last := ingestOBIBook(symbol, book, levels, false)
	samples := snapshotOBISamples(symbol)
	resp := buildOBIAnalysisFromSamples(samples, last, levels, klines, includeBars)
	if bb, okB := book.BestBid(); okB {
		resp.BestBid = roundFloat(bb.Price.Float64(), 8)
	}
	if ba, okA := book.BestAsk(); okA {
		resp.BestAsk = roundFloat(ba.Price.Float64(), 8)
	}
	return resp
}

func (s *Server) analysisOBI(c *gin.Context) {
	session, ok := s.sessionOrAbort(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	symbol := strings.ToUpper(strings.TrimSpace(c.DefaultQuery("symbol", "BTCUSDT")))
	if symbol == "" {
		symbol = "BTCUSDT"
	}
	levels := parseOBILevels(c.DefaultQuery("levels", ""))
	interval, limit := parseChartInterval(c.DefaultQuery("interval", "1h"), c.DefaultQuery("limit", ""))

	klines, klineSource, err := s.queryAnalysisKLines(ctx, session, symbol, interval, limit)
	if err != nil {
		// still try depth-only
		obi := s.sampleAndBuildOBIOnce(ctx, session.Exchange, symbol, levels, nil, true)
		c.JSON(200, gin.H{
			"symbol": symbol,
			"obi":    obi,
			"error":  err.Error(),
		})
		return
	}
	obi := s.sampleAndBuildOBIOnce(ctx, session.Exchange, symbol, levels, klines, true)
	c.JSON(200, gin.H{
		"symbol":      symbol,
		"interval":    string(interval),
		"klineSource": klineSource,
		"levels":      levels,
		"obi":         obi,
	})
}
