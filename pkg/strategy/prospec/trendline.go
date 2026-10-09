package prospec

import (
	"math"
	"sort"
	"time"

	"github.com/c9s/bbgo/pkg/types"
)

// TrendFitMethod selects how stage-① trendline is built.
type TrendFitMethod string

const (
	// TrendFitHL: legacy proxy — close breaks last swing HL/LH (no diagonal line).
	TrendFitHL TrendFitMethod = "hl"
	// TrendFitOLS: least-squares fit through recent pivots, require ≥minTouches.
	TrendFitOLS TrendFitMethod = "ols"
	// TrendFitRANSAC: exhaustive pair consensus (deterministic RANSAC) then OLS on inliers.
	TrendFitRANSAC TrendFitMethod = "ransac"
)

// TrendLine is y = Intercept + Slope * barIndex (absolute hist index).
type TrendLine struct {
	Method     string    `json:"method"`
	Kind       string    `json:"kind"` // support | resistance
	Slope      float64   `json:"slope"`
	Intercept  float64   `json:"intercept"`
	StartIdx   int       `json:"startIdx"`
	EndIdx     int       `json:"endIdx"`
	Touches    int       `json:"touches"`
	Violations int       `json:"violations"`
	Score      float64   `json:"score"`
	Tol        float64   `json:"tol"`
	Y1         float64   `json:"y1"`
	Y2         float64   `json:"y2"`
	YNow       float64   `json:"yNow"` // draw-to price (last bar, or break if broken)
	StartTime  time.Time `json:"startTime,omitempty"`
	EndTime    time.Time `json:"endTime,omitempty"`
	NowTime    time.Time `json:"nowTime,omitempty"`
	Broken     bool      `json:"broken"`
	BreakIdx   int       `json:"breakIdx,omitempty"`
	BreakTime  time.Time `json:"breakTime,omitempty"`
	YBreak     float64   `json:"yBreak,omitempty"`
	Note       string    `json:"note"`
}

// PriceAt returns the line value at bar index i.
func (t TrendLine) PriceAt(i int) float64 {
	return t.Intercept + t.Slope*float64(i)
}

type pivotPt struct {
	idx   int
	price float64
}

// atrApprox is a simple TR average for touch tolerance.
func atrApprox(hist []types.KLine, period int) float64 {
	n := len(hist)
	if n < 2 {
		return 0
	}
	if period < 2 {
		period = 14
	}
	start := n - period - 1
	if start < 1 {
		start = 1
	}
	var sum float64
	var cnt int
	for i := start; i < n; i++ {
		h := hist[i].High.Float64()
		l := hist[i].Low.Float64()
		pc := hist[i-1].Close.Float64()
		tr := h - l
		if d := math.Abs(h - pc); d > tr {
			tr = d
		}
		if d := math.Abs(l - pc); d > tr {
			tr = d
		}
		sum += tr
		cnt++
	}
	if cnt == 0 {
		return hist[n-1].Close.Float64() * 0.005
	}
	return sum / float64(cnt)
}

func pivotsOfKind(sw []Swing, kind string) []pivotPt {
	var out []pivotPt
	for _, s := range sw {
		if s.Kind == kind {
			out = append(out, pivotPt{idx: s.Index, price: s.Price})
		}
	}
	return out
}

func fitOLS(pts []pivotPt) (slope, intercept float64, ok bool) {
	n := len(pts)
	if n < 2 {
		return 0, 0, false
	}
	var sx, sy, sxx, sxy float64
	for _, p := range pts {
		x := float64(p.idx)
		y := p.price
		sx += x
		sy += y
		sxx += x * x
		sxy += x * y
	}
	nf := float64(n)
	den := nf*sxx - sx*sx
	if math.Abs(den) < 1e-12 {
		return 0, 0, false
	}
	slope = (nf*sxy - sx*sy) / den
	intercept = (sy - slope*sx) / nf
	return slope, intercept, true
}

func countTouchesViolations(hist []types.KLine, pts []pivotPt, slope, intercept, tol float64, support bool) (touches, violations int, avgRes float64) {
	var resSum float64
	for _, p := range pts {
		yhat := intercept + slope*float64(p.idx)
		res := math.Abs(p.price - yhat)
		resSum += res
		if res <= tol {
			touches++
		}
	}
	if len(pts) > 0 {
		avgRes = resSum / float64(len(pts))
	}
	if len(hist) == 0 || len(pts) < 2 {
		return
	}
	start, end := pts[0].idx, pts[len(pts)-1].idx
	if start > end {
		start, end = end, start
	}
	for i := start; i <= end && i < len(hist); i++ {
		yhat := intercept + slope*float64(i)
		c := hist[i].Close.Float64()
		if support {
			if c < yhat-tol {
				violations++
			}
		} else {
			if c > yhat+tol {
				violations++
			}
		}
	}
	return
}

func scoreLine(touches, violations int, span int, avgRes, tol, slope float64, support bool) float64 {
	if touches < 2 {
		return -1
	}
	dirOK := (support && slope > 0) || (!support && slope < 0)
	if !dirOK {
		return -1
	}
	// steepness: slope vs price — reject absurd angles
	steepPen := math.Abs(slope) / (tol + 1e-9)
	if steepPen > 3 {
		return -1
	}
	fit := 1.0 / (1.0 + avgRes/(tol+1e-9))
	spanBonus := math.Log1p(float64(span))
	return float64(touches)*3.0 + spanBonus*0.5 + fit*2.0 - float64(violations)*1.5 - steepPen*0.3
}

func makeLine(method TrendFitMethod, support bool, hist []types.KLine, pts []pivotPt, slope, intercept, tol float64) *TrendLine {
	if len(pts) < 2 {
		return nil
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].idx < pts[j].idx })
	touches, viol, avgRes := countTouchesViolations(hist, pts, slope, intercept, tol, support)
	span := pts[len(pts)-1].idx - pts[0].idx
	sc := scoreLine(touches, viol, span, avgRes, tol, slope, support)
	if sc < 0 {
		return nil
	}
	kind := "support"
	if !support {
		kind = "resistance"
	}
	return &TrendLine{
		Method: string(method), Kind: kind,
		Slope: slope, Intercept: intercept,
		StartIdx: pts[0].idx, EndIdx: pts[len(pts)-1].idx,
		Touches: touches, Violations: viol, Score: round8(sc), Tol: round8(tol),
		Y1: round8(intercept + slope*float64(pts[0].idx)),
		Y2: round8(intercept + slope*float64(pts[len(pts)-1].idx)),
		Note: kind + " " + string(method) + " touches=" + itoa(touches),
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [16]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// FitTrendOLS fits one support (lows) and one resistance (highs) via OLS on all recent pivots.
func FitTrendOLS(hist []types.KLine, sw []Swing, minTouches int) (support, resistance *TrendLine) {
	if minTouches < 2 {
		minTouches = 2
	}
	tol := atrApprox(hist, 14) * 0.25
	if tol <= 0 {
		tol = hist[len(hist)-1].Close.Float64() * 0.003
	}
	lows := pivotsOfKind(sw, "low")
	highs := pivotsOfKind(sw, "high")
	// use last up to 8 pivots
	lows = tailPivots(lows, 8)
	highs = tailPivots(highs, 8)

	if slope, icept, ok := fitOLS(lows); ok {
		if ln := makeLine(TrendFitOLS, true, hist, lows, slope, icept, tol); ln != nil && ln.Touches >= minTouches {
			support = ln
		}
	}
	if slope, icept, ok := fitOLS(highs); ok {
		if ln := makeLine(TrendFitOLS, false, hist, highs, slope, icept, tol); ln != nil && ln.Touches >= minTouches {
			resistance = ln
		}
	}
	return
}

func tailPivots(pts []pivotPt, max int) []pivotPt {
	if len(pts) <= max {
		return pts
	}
	return pts[len(pts)-max:]
}

// MaxTrendLinesPerKind caps how many support/resistance segments we keep for charts.
const MaxTrendLinesPerKind = 3

// FitTrendRANSAC exhaustive pair scan (deterministic RANSAC) + OLS refit on inliers.
// Returns the primary line of each kind: prefer recent unbroken, then recent break.
func FitTrendRANSAC(hist []types.KLine, sw []Swing, minTouches int) (support, resistance *TrendLine) {
	sups, ress := FitTrendRANSACMulti(hist, sw, minTouches, 1)
	if len(sups) > 0 {
		support = sups[0]
	}
	if len(ress) > 0 {
		resistance = ress[0]
	}
	return
}

// FitTrendRANSACMulti returns up to maxN distinct lines per kind, ranked for trading relevance.
func FitTrendRANSACMulti(hist []types.KLine, sw []Swing, minTouches, maxN int) (supports, resistances []*TrendLine) {
	if maxN <= 0 {
		maxN = MaxTrendLinesPerKind
	}
	if minTouches < 2 {
		minTouches = 3
	}
	tol := atrApprox(hist, 14) * 0.3
	if tol <= 0 && len(hist) > 0 {
		tol = hist[len(hist)-1].Close.Float64() * 0.004
	}
	lows := tailPivots(pivotsOfKind(sw, "low"), 14)
	highs := tailPivots(pivotsOfKind(sw, "high"), 14)
	// Collect with minTouches, fall back to 2 so short recent legs (V-bounce) can appear.
	supports = selectConsensusLines(hist, lows, tol, minTouches, true, maxN)
	if len(supports) == 0 && minTouches > 2 {
		supports = selectConsensusLines(hist, lows, tol, 2, true, maxN)
	}
	resistances = selectConsensusLines(hist, highs, tol, minTouches, false, maxN)
	if len(resistances) == 0 && minTouches > 2 {
		resistances = selectConsensusLines(hist, highs, tol, 2, false, maxN)
	}
	return
}

func bestConsensusLine(hist []types.KLine, pts []pivotPt, tol float64, minTouches int, support bool) *TrendLine {
	lines := selectConsensusLines(hist, pts, tol, minTouches, support, 1)
	if len(lines) == 0 {
		return nil
	}
	return lines[0]
}

// collectConsensusCandidates enumerates RANSAC pair hypotheses (unstamped).
func collectConsensusCandidates(hist []types.KLine, pts []pivotPt, tol float64, minTouches int, support bool) []*TrendLine {
	n := len(pts)
	if n < 2 {
		return nil
	}
	var out []*TrendLine
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			dx := float64(pts[j].idx - pts[i].idx)
			if math.Abs(dx) < 1 {
				continue
			}
			slope := (pts[j].price - pts[i].price) / dx
			if support && slope <= 0 {
				continue
			}
			if !support && slope >= 0 {
				continue
			}
			intercept := pts[i].price - slope*float64(pts[i].idx)
			var inliers []pivotPt
			for _, p := range pts {
				yhat := intercept + slope*float64(p.idx)
				if math.Abs(p.price-yhat) <= tol {
					inliers = append(inliers, p)
				}
			}
			if len(inliers) < minTouches {
				continue
			}
			s2, a2, ok := fitOLS(inliers)
			if !ok {
				continue
			}
			if (support && s2 <= 0) || (!support && s2 >= 0) {
				continue
			}
			ln := makeLine(TrendFitRANSAC, support, hist, inliers, s2, a2, tol)
			if ln == nil {
				continue
			}
			out = append(out, ln)
		}
	}
	return out
}

// rankTrendLine prefers unbroken + recent last touch/break, then geometric score.
func rankTrendLine(ln *TrendLine, nBars int) float64 {
	if ln == nil || nBars <= 0 {
		return -1
	}
	r := ln.Score
	if !ln.Broken {
		r += 50
		// fresher last pivot touch
		r += 35 * float64(ln.EndIdx) / float64(nBars)
	} else {
		// recently broken still useful for Stage①; ancient breaks sink
		r += 12
		bi := ln.BreakIdx
		if bi <= 0 {
			bi = ln.EndIdx
		}
		r += 25 * float64(bi) / float64(nBars)
		age := nBars - 1 - bi
		if age > 0 {
			r -= float64(age) * 0.08
		}
	}
	return r
}

func linesSimilar(a, b *TrendLine, tol float64) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Kind != b.Kind {
		return false
	}
	mid := (a.StartIdx + a.EndIdx) / 2
	if mid < 0 {
		mid = 0
	}
	end := a.EndIdx
	if b.EndIdx > end {
		end = b.EndIdx
	}
	if end < mid {
		end = mid
	}
	da := math.Abs(a.PriceAt(mid) - b.PriceAt(mid))
	db := math.Abs(a.PriceAt(end) - b.PriceAt(end))
	thr := tol * 2.5
	if thr < 1e-9 {
		thr = 1
	}
	if da > thr || db > thr {
		return false
	}
	den := math.Abs(a.Slope) + math.Abs(b.Slope) + 1e-9
	return math.Abs(a.Slope-b.Slope)/den < 0.35
}

// selectConsensusLines stamps candidates, ranks (recent unbroken first), keeps distinct top maxN.
func selectConsensusLines(hist []types.KLine, pts []pivotPt, tol float64, minTouches int, support bool, maxN int) []*TrendLine {
	cands := collectConsensusCandidates(hist, pts, tol, minTouches, support)
	if len(cands) == 0 {
		return nil
	}
	nBars := len(hist)
	for _, ln := range cands {
		stampTrendLine(hist, ln)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		return rankTrendLine(cands[i], nBars) > rankTrendLine(cands[j], nBars)
	})
	var out []*TrendLine
	for _, ln := range cands {
		dup := false
		for _, kept := range out {
			if linesSimilar(ln, kept, tol) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		out = append(out, ln)
		if len(out) >= maxN {
			break
		}
	}
	return out
}

// TrendLineSet is the multi-segment chart payload (OLS primary + RANSAC multi).
type TrendLineSet struct {
	OLSSupport        *TrendLine   `json:"olsSupport,omitempty"`
	OLSResistance     *TrendLine   `json:"olsResistance,omitempty"`
	RANSACSupport     *TrendLine   `json:"ransacSupport,omitempty"` // primary (= ransacSupports[0])
	RANSACResistance  *TrendLine   `json:"ransacResistance,omitempty"`
	RANSACSupports    []*TrendLine `json:"ransacSupports,omitempty"`
	RANSACResistances []*TrendLine `json:"ransacResistances,omitempty"`
}

// DetectTrendLines returns primary OLS + RANSAC lines (backward compatible).
func DetectTrendLines(ks []types.KLine, look int) (olsSup, olsRes, ransacSup, ransacRes *TrendLine) {
	set := DetectTrendLineSet(ks, look, 1)
	return set.OLSSupport, set.OLSResistance, set.RANSACSupport, set.RANSACResistance
}

// DetectTrendLineSet fits OLS primaries and up to maxPerKind RANSAC segments per side.
func DetectTrendLineSet(ks []types.KLine, look, maxPerKind int) TrendLineSet {
	var set TrendLineSet
	hist := closedHist(ks)
	if len(hist) < 40 {
		return set
	}
	if look < 2 {
		look = 3
	}
	if maxPerKind <= 0 {
		maxPerKind = MaxTrendLinesPerKind
	}
	sw := FindSwings(hist, look)
	olsSup, olsRes := FitTrendOLS(hist, sw, 2)
	stampTrendLine(hist, olsSup)
	stampTrendLine(hist, olsRes)
	sups, ress := FitTrendRANSACMulti(hist, sw, 3, maxPerKind)
	set.OLSSupport = olsSup
	set.OLSResistance = olsRes
	set.RANSACSupports = sups
	set.RANSACResistances = ress
	if len(sups) > 0 {
		set.RANSACSupport = sups[0]
	}
	if len(ress) > 0 {
		set.RANSACResistance = ress[0]
	}
	return set
}

func stampTrendLine(hist []types.KLine, ln *TrendLine) {
	if ln == nil || len(hist) == 0 {
		return
	}
	n := len(hist)
	if ln.StartIdx < 0 {
		ln.StartIdx = 0
	}
	if ln.EndIdx >= n {
		ln.EndIdx = n - 1
	}
	if ln.StartIdx >= n {
		ln.StartIdx = n - 1
	}
	ln.StartTime = hist[ln.StartIdx].StartTime.Time().UTC()
	ln.EndTime = hist[ln.EndIdx].StartTime.Time().UTC()
	ln.Y1 = round8(ln.PriceAt(ln.StartIdx))
	ln.Y2 = round8(ln.PriceAt(ln.EndIdx))

	support := ln.Kind == "support"
	if idx, ok := firstCloseBreak(hist, ln, support); ok {
		// Broken: stop drawing at the break bar (do not shoot into empty air).
		ln.Broken = true
		ln.BreakIdx = idx
		ln.BreakTime = hist[idx].StartTime.Time().UTC()
		ln.YBreak = round8(ln.PriceAt(idx))
		ln.NowTime = ln.BreakTime
		ln.YNow = ln.YBreak
		ln.Note = ln.Note + " ·已破线"
		return
	}
	ln.Broken = false
	ln.NowTime = hist[n-1].StartTime.Time().UTC()
	ln.YNow = round8(ln.PriceAt(n - 1))
}

// firstCloseBreak returns first bar index after line.EndIdx where close breaks the line.
func firstCloseBreak(hist []types.KLine, line *TrendLine, support bool) (idx int, ok bool) {
	if line == nil || len(hist) == 0 {
		return -1, false
	}
	start := line.EndIdx + 1
	if start < line.StartIdx+2 {
		start = line.StartIdx + 2
	}
	for i := start; i < len(hist); i++ {
		yhat := line.PriceAt(i)
		c := hist[i].Close.Float64()
		if support {
			if c < yhat-line.Tol*0.5 {
				return i, true
			}
		} else {
			if c > yhat+line.Tol*0.5 {
				return i, true
			}
		}
	}
	return -1, false
}
