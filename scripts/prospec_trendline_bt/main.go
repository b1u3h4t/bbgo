// Standalone walk-forward compare of hl / ols / ransac stage-① methods.
// go run .  (stdlib only)
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"time"
)

type bar struct{ O, H, L, C float64 }

type swing struct {
	i    int
	p    float64
	kind string
}

type line struct {
	slope, icept, tol float64
	touches           int
	start, end        int
}

var symbols = []string{
	"BTCUSDT", "ETHUSDT", "NEARUSDT", "BNBUSDT", "AVAXUSDT", "LINKUSDT",
	"SOLUSDT", "ENAUSDT", "XRPUSDT", "HYPEUSDT", "ZECUSDT",
}

type agg struct {
	t, w int
	sum  float64
}

func main() {
	a4 := map[string]*agg{"hl": {}, "ols": {}, "ransac": {}}
	a1 := map[string]*agg{"hl": {}, "ols": {}, "ransac": {}}
	fmt.Println("symbol iv method trades wr% avgR")
	for _, sy := range symbols {
		for _, iv := range []string{"4h", "1d"} {
			limit, hor := 300, 24
			if iv == "1d" {
				limit, hor = 260, 12
			}
			ks, err := fetch(sy, iv, limit)
			if err != nil {
				fmt.Println(sy, iv, "ERR", err)
				continue
			}
			for _, m := range []string{"hl", "ols", "ransac"} {
				t, w, sum := backtest123(ks, m, hor)
				wr, avg := 0.0, 0.0
				if t > 0 {
					wr = 100 * float64(w) / float64(t)
					avg = sum / float64(t)
				}
				fmt.Printf("%s %s %s %d %.1f %.3f\n", sy, iv, m, t, wr, avg)
				dst := a4
				if iv == "1d" {
					dst = a1
				}
				dst[m].t += t
				dst[m].w += w
				dst[m].sum += sum
			}
			time.Sleep(80 * time.Millisecond)
		}
	}
	printAgg("4h AGG holdings+BTC/ETH", a4)
	printAgg("1d AGG holdings+BTC/ETH", a1)
}

func printAgg(title string, a map[string]*agg) {
	fmt.Println("===", title, "===")
	for _, m := range []string{"hl", "ols", "ransac"} {
		x := a[m]
		wr, avg := 0.0, 0.0
		if x.t > 0 {
			wr = 100 * float64(x.w) / float64(x.t)
			avg = x.sum / float64(x.t)
		}
		fmt.Printf("%s trades=%d wr=%.2f%% avgR=%.4f\n", m, x.t, wr, avg)
	}
}

func fetch(symbol, interval string, limit int) ([]bar, error) {
	u := fmt.Sprintf("https://fapi.binance.com/fapi/v1/klines?symbol=%s&interval=%s&limit=%d", symbol, interval, limit)
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw [][]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]bar, 0, len(raw))
	for _, r := range raw {
		out = append(out, bar{
			O: atof(r[1]), H: atof(r[2]), L: atof(r[3]), C: atof(r[4]),
		})
	}
	return out, nil
}

func atof(v any) float64 {
	switch x := v.(type) {
	case string:
		var f float64
		fmt.Sscanf(x, "%f", &f)
		return f
	case float64:
		return x
	default:
		return 0
	}
}

func findSwings(ks []bar, look int) []swing {
	n := len(ks)
	var out []swing
	for i := look; i < n-look; i++ {
		okH, okL := true, true
		for j := 1; j <= look; j++ {
			if ks[i].H <= ks[i-j].H || ks[i].H < ks[i+j].H {
				okH = false
			}
			if ks[i].L >= ks[i-j].L || ks[i].L > ks[i+j].L {
				okL = false
			}
		}
		if okH {
			out = append(out, swing{i, ks[i].H, "h"})
		}
		if okL {
			out = append(out, swing{i, ks[i].L, "l"})
		}
	}
	return out
}

func atr(ks []bar, period int) float64 {
	if len(ks) < 3 {
		return 1
	}
	start := len(ks) - period - 1
	if start < 1 {
		start = 1
	}
	var sum float64
	var c int
	for i := start; i < len(ks); i++ {
		tr := ks[i].H - ks[i].L
		if d := math.Abs(ks[i].H - ks[i-1].C); d > tr {
			tr = d
		}
		if d := math.Abs(ks[i].L - ks[i-1].C); d > tr {
			tr = d
		}
		sum += tr
		c++
	}
	if c == 0 {
		return ks[len(ks)-1].C * 0.005
	}
	return sum / float64(c)
}

func pivots(sw []swing, kind string, max int) [][2]float64 {
	var pts [][2]float64
	for _, s := range sw {
		if s.kind == kind {
			pts = append(pts, [2]float64{float64(s.i), s.p})
		}
	}
	if len(pts) > max {
		pts = pts[len(pts)-max:]
	}
	return pts
}

func ols(pts [][2]float64) (slope, icept float64, ok bool) {
	n := len(pts)
	if n < 2 {
		return
	}
	var sx, sy, sxx, sxy float64
	for _, p := range pts {
		sx += p[0]
		sy += p[1]
		sxx += p[0] * p[0]
		sxy += p[0] * p[1]
	}
	nf := float64(n)
	den := nf*sxx - sx*sx
	if math.Abs(den) < 1e-12 {
		return
	}
	slope = (nf*sxy - sx*sy) / den
	icept = (sy - slope*sx) / nf
	return slope, icept, true
}

func makeLine(ks []bar, pts [][2]float64, slope, icept, tol float64, support bool) *line {
	if len(pts) < 2 {
		return nil
	}
	touches := 0
	for _, p := range pts {
		if math.Abs(p[1]-(icept+slope*p[0])) <= tol {
			touches++
		}
	}
	if touches < 2 {
		return nil
	}
	if (support && slope <= 0) || (!support && slope >= 0) {
		return nil
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i][0] < pts[j][0] })
	return &line{slope, icept, tol, touches, int(pts[0][0]), int(pts[len(pts)-1][0])}
}

func fitOLS(ks []bar, sw []swing, support bool) *line {
	tol := atr(ks, 14) * 0.25
	kind := "l"
	if !support {
		kind = "h"
	}
	pts := pivots(sw, kind, 8)
	s, a, ok := ols(pts)
	if !ok {
		return nil
	}
	return makeLine(ks, pts, s, a, tol, support)
}

func fitRANSAC(ks []bar, sw []swing, support bool) *line {
	tol := atr(ks, 14) * 0.3
	kind := "l"
	if !support {
		kind = "h"
	}
	pts := pivots(sw, kind, 12)
	minT := 3
	var best *line
	bestSc := -1.0
	n := len(pts)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			dx := pts[j][0] - pts[i][0]
			if math.Abs(dx) < 1 {
				continue
			}
			slope := (pts[j][1] - pts[i][1]) / dx
			if (support && slope <= 0) || (!support && slope >= 0) {
				continue
			}
			icept := pts[i][1] - slope*pts[i][0]
			var inl [][2]float64
			for _, p := range pts {
				if math.Abs(p[1]-(icept+slope*p[0])) <= tol {
					inl = append(inl, p)
				}
			}
			if len(inl) < minT {
				continue
			}
			s2, a2, ok := ols(inl)
			if !ok {
				continue
			}
			ln := makeLine(ks, inl, s2, a2, tol, support)
			if ln == nil {
				continue
			}
			sc := float64(ln.touches) * 3
			if sc > bestSc {
				bestSc = sc
				best = ln
			}
		}
	}
	if best == nil && minT > 2 {
		// retry with 2
		minT = 2
		_ = minT
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				dx := pts[j][0] - pts[i][0]
				if math.Abs(dx) < 1 {
					continue
				}
				slope := (pts[j][1] - pts[i][1]) / dx
				if (support && slope <= 0) || (!support && slope >= 0) {
					continue
				}
				icept := pts[i][1] - slope*pts[i][0]
				inl := [][2]float64{pts[i], pts[j]}
				for _, p := range pts {
					if p == pts[i] || p == pts[j] {
						continue
					}
					if math.Abs(p[1]-(icept+slope*p[0])) <= tol {
						inl = append(inl, p)
					}
				}
				s2, a2, ok := ols(inl)
				if !ok {
					continue
				}
				ln := makeLine(ks, inl, s2, a2, tol, support)
				if ln != nil && float64(ln.touches)*3 > bestSc {
					bestSc = float64(ln.touches) * 3
					best = ln
				}
			}
		}
	}
	return best
}

func firstBreak(ks []bar, ln *line, support bool) int {
	if ln == nil {
		return -1
	}
	start := ln.end + 1
	for i := start; i < len(ks); i++ {
		y := ln.icept + ln.slope*float64(i)
		if support && ks[i].C < y-ln.tol*0.5 {
			return i
		}
		if !support && ks[i].C > y+ln.tol*0.5 {
			return i
		}
	}
	return -1
}

type sig struct {
	dir       string
	stage     int
	confirmed bool
	s2, prior float64
	t3        int
}

func detect(ks []bar, method string) sig {
	sw := findSwings(ks, 3)
	var highs, lows []swing
	for _, s := range sw {
		if s.kind == "h" {
			highs = append(highs, s)
		} else {
			lows = append(lows, s)
		}
	}
	if len(highs) < 2 || len(lows) < 2 {
		return sig{}
	}
	// prefer bear path if confirmed
	if s := detectBear(ks, highs, lows, sw, method); s.confirmed {
		return s
	}
	if s := detectBull(ks, highs, lows, sw, method); s.confirmed || s.stage > 0 {
		if s.confirmed {
			return s
		}
	}
	if s := detectBear(ks, highs, lows, sw, method); s.stage > 0 {
		return s
	}
	return detectBull(ks, highs, lows, sw, method)
}

func detectBear(ks []bar, highs, lows []swing, sw []swing, method string) sig {
	hh := highs[len(highs)-1]
	hl := lows[len(lows)-1]
	if hl.i > hh.i {
		for i := len(lows) - 1; i >= 0; i-- {
			if lows[i].i < hh.i {
				hl = lows[i]
				break
			}
		}
	}
	prior := highs[len(highs)-2].p
	stage1 := -1
	switch method {
	case "ols":
		stage1 = firstBreak(ks, fitOLS(ks, sw, true), true)
	case "ransac":
		stage1 = firstBreak(ks, fitRANSAC(ks, sw, true), true)
	default:
		for i := hl.i + 1; i < len(ks); i++ {
			if ks[i].C < hl.p {
				stage1 = i
				break
			}
		}
	}
	if stage1 < 0 {
		return sig{dir: "to_bear"}
	}
	bounceHi, bounceIdx := 0.0, -1
	for i := stage1 + 1; i < len(ks); i++ {
		if ks[i].H > bounceHi {
			bounceHi = ks[i].H
			bounceIdx = i
		}
		if ks[i].C > hh.p {
			return sig{}
		}
	}
	if bounceIdx < 0 || bounceHi >= hh.p {
		return sig{dir: "to_bear", stage: 1}
	}
	react := bounceHi
	for i := stage1; i <= bounceIdx; i++ {
		if ks[i].L < react {
			react = ks[i].L
		}
	}
	for i := bounceIdx + 1; i < len(ks); i++ {
		if ks[i].C < react {
			return sig{dir: "to_bear", stage: 3, confirmed: true, s2: bounceHi, prior: prior, t3: i}
		}
	}
	last := ks[len(ks)-1].C
	if last < hl.p && bounceIdx > stage1 {
		return sig{dir: "to_bear", stage: 3, confirmed: true, s2: bounceHi, prior: prior, t3: len(ks) - 1}
	}
	return sig{dir: "to_bear", stage: 2, s2: bounceHi, prior: prior}
}

func detectBull(ks []bar, highs, lows []swing, sw []swing, method string) sig {
	ll := lows[len(lows)-1]
	lh := highs[len(highs)-1]
	if lh.i > ll.i {
		for i := len(highs) - 1; i >= 0; i-- {
			if highs[i].i < ll.i {
				lh = highs[i]
				break
			}
		}
	}
	prior := lows[len(lows)-2].p
	stage1 := -1
	switch method {
	case "ols":
		stage1 = firstBreak(ks, fitOLS(ks, sw, false), false)
	case "ransac":
		stage1 = firstBreak(ks, fitRANSAC(ks, sw, false), false)
	default:
		for i := lh.i + 1; i < len(ks); i++ {
			if ks[i].C > lh.p {
				stage1 = i
				break
			}
		}
	}
	if stage1 < 0 {
		return sig{dir: "to_bull"}
	}
	bounceLo, bounceIdx := math.MaxFloat64, -1
	for i := stage1 + 1; i < len(ks); i++ {
		if ks[i].L < bounceLo {
			bounceLo = ks[i].L
			bounceIdx = i
		}
		if ks[i].C < ll.p {
			return sig{}
		}
	}
	if bounceIdx < 0 || bounceLo <= ll.p {
		return sig{dir: "to_bull", stage: 1}
	}
	react := bounceLo
	for i := stage1; i <= bounceIdx; i++ {
		if ks[i].H > react {
			react = ks[i].H
		}
	}
	for i := bounceIdx + 1; i < len(ks); i++ {
		if ks[i].C > react {
			return sig{dir: "to_bull", stage: 3, confirmed: true, s2: bounceLo, prior: prior, t3: i}
		}
	}
	last := ks[len(ks)-1].C
	if last > lh.p && bounceIdx > stage1 {
		return sig{dir: "to_bull", stage: 3, confirmed: true, s2: bounceLo, prior: prior, t3: len(ks) - 1}
	}
	return sig{dir: "to_bull", stage: 2, s2: bounceLo, prior: prior}
}

func backtest123(ks []bar, method string, horizon int) (trades, wins int, sumR float64) {
	n := len(ks)
	if n < 80 {
		return
	}
	var lastKey string
	for i := 50; i < n-horizon; i++ {
		o := detect(ks[:i+1], method)
		if !o.confirmed {
			continue
		}
		if o.t3 != i && o.t3 != i-1 {
			continue
		}
		key := fmt.Sprintf("%s:%.4f:%.4f", o.dir, o.s2, o.prior)
		if key == lastKey {
			continue
		}
		lastKey = key
		entry := ks[i].C
		long := o.dir == "to_bull"
		var stop, target float64
		if long {
			stop = o.s2
			if stop >= entry {
				stop = o.prior
			}
			if stop >= entry {
				stop = entry * 0.99
			}
			risk := entry - stop
			target = entry + risk
		} else {
			stop = o.s2
			if stop <= entry {
				stop = o.prior
			}
			if stop <= entry {
				stop = entry * 1.01
			}
			risk := stop - entry
			target = entry - risk
		}
		r := simulate(ks, i, long, entry, stop, target, horizon)
		trades++
		sumR += r
		if r > 0 {
			wins++
		}
	}
	return
}

func simulate(ks []bar, i int, long bool, entry, stop, target float64, horizon int) float64 {
	risk := math.Abs(entry - stop)
	if risk <= 0 {
		return 0
	}
	end := i + horizon
	if end >= len(ks) {
		end = len(ks) - 1
	}
	for j := i + 1; j <= end; j++ {
		if long {
			if ks[j].L <= stop {
				return -1
			}
			if ks[j].H >= target {
				return (target - entry) / risk
			}
		} else {
			if ks[j].H >= stop {
				return -1
			}
			if ks[j].L <= target {
				return (entry - target) / risk
			}
		}
	}
	last := ks[end].C
	if long {
		return (last - entry) / risk
	}
	return (entry - last) / risk
}

func init() {
	if os.Getenv("HTTP_PROXY") != "" {
		// default transport picks it up
	}
}
