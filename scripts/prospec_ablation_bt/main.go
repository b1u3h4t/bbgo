// Ablation backtest: community-style filters vs baseline 123/2B.
// Inspired by Casoon (min touches/combinatorial), LuxAlgo SFP/2B, Sperandeo nest.
// go run .   (stdlib only; run on bbgo with TMPDIR=$HOME/tmp)
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
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
	touches, viol     int
	start, end        int
	score             float64
}

type variant struct {
	name     string
	kind     string  // 123 | 2b
	method   string  // hl | ols | ransac | casoon
	rr       float64 // reward:risk
	nest     bool
	minTouch int
	feeR     float64 // fee drag in R units per trade
}

var symbols = []string{
	"BTCUSDT", "ETHUSDT", "NEARUSDT", "BNBUSDT", "AVAXUSDT", "LINKUSDT",
	"SOLUSDT", "ENAUSDT", "XRPUSDT", "HYPEUSDT", "ZECUSDT",
}

var variants = []variant{
	{name: "123_ransac_1R", kind: "123", method: "ransac", rr: 1, nest: false, minTouch: 2},
	{name: "123_ransac_1R_nest", kind: "123", method: "ransac", rr: 1, nest: true, minTouch: 2},
	{name: "123_casoon_1R_nest", kind: "123", method: "casoon", rr: 1, nest: true, minTouch: 3},
	{name: "123_casoon_2R_nest", kind: "123", method: "casoon", rr: 2, nest: true, minTouch: 3},
	{name: "123_ransac_2R_nest", kind: "123", method: "ransac", rr: 2, nest: true, minTouch: 2},
	{name: "2b_1R", kind: "2b", method: "hl", rr: 1, nest: false},
	{name: "2b_1R_nest", kind: "2b", method: "hl", rr: 1, nest: true},
	{name: "2b_2R_nest", kind: "2b", method: "hl", rr: 2, nest: true},
	{name: "2b_2R_nest_fee", kind: "2b", method: "hl", rr: 2, nest: true, feeR: 0.15},
	{name: "123_casoon_2R_nest_fee", kind: "123", method: "casoon", rr: 2, nest: true, minTouch: 3, feeR: 0.15},
}

type agg struct{ t, w int; sum float64 }

func main() {
	type key struct{ iv, name string }
	pool := map[key]*agg{}
	for _, v := range variants {
		pool[key{"4h", v.name}] = &agg{}
		pool[key{"1d", v.name}] = &agg{}
	}

	fmt.Println("symbol iv variant trades wr% avgR")
	for _, sy := range symbols {
		d1, err1 := fetch(sy, "1d", 260)
		h4, err4 := fetch(sy, "4h", 300)
		if err1 != nil || err4 != nil {
			fmt.Println(sy, "ERR", err1, err4)
			continue
		}
		for _, iv := range []string{"4h", "1d"} {
			var ks, nestKS []bar
			hor := 24
			if iv == "4h" {
				ks, nestKS, hor = h4, d1, 24
			} else {
				ks, nestKS, hor = d1, d1, 12
			}
			for _, v := range variants {
				t, w, sum := runVariant(ks, nestKS, iv, v, hor)
				wr, avg := 0.0, 0.0
				if t > 0 {
					wr = 100 * float64(w) / float64(t)
					avg = sum / float64(t)
				}
				fmt.Printf("%s %s %s %d %.1f %.3f\n", sy, iv, v.name, t, wr, avg)
				a := pool[key{iv, v.name}]
				a.t += t
				a.w += w
				a.sum += sum
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	for _, iv := range []string{"4h", "1d"} {
		fmt.Println("=== AGG", iv, "holdings+BTC/ETH ===")
		fmt.Printf("%-28s %6s %7s %8s\n", "variant", "trades", "wr%", "avgR")
		for _, v := range variants {
			a := pool[key{iv, v.name}]
			wr, avg := 0.0, 0.0
			if a.t > 0 {
				wr = 100 * float64(a.w) / float64(a.t)
				avg = a.sum / float64(a.t)
			}
			fmt.Printf("%-28s %6d %6.1f %8.3f\n", v.name, a.t, wr, avg)
		}
	}
}

func runVariant(ks, nestKS []bar, iv string, v variant, hor int) (trades, wins int, sum float64) {
	n := len(ks)
	if n < 80 {
		return
	}
	var lastKey string
	cool := -100
	for i := 55; i < n-hor; i++ {
		nestBias := "chop"
		if v.nest {
			if iv == "4h" {
				// map 4h bar time approx: use nestKS prefix by ratio
				ni := int(float64(i) / float64(n) * float64(len(nestKS)))
				if ni < 40 {
					ni = 40
				}
				if ni >= len(nestKS) {
					ni = len(nestKS) - 1
				}
				nestBias = classifyNest(nestKS[:ni+1])
			} else {
				nestBias = classifyNest(ks[:i+1])
			}
		}

		if v.kind == "2b" {
			tb := detect2B(ks[:i+1])
			if tb == nil || tb.barsAgo != 0 {
				continue
			}
			if i-cool < 4 {
				continue
			}
			long := tb.side == "long"
			if v.nest {
				if long && nestBias != "bull" {
					continue
				}
				if !long && nestBias != "bear" {
					continue
				}
			}
			entry := ks[i].C
			var stop, target float64
			if long {
				stop = tb.pierced * 0.998
				if stop >= entry {
					stop = entry * 0.99
				}
				risk := entry - stop
				target = entry + risk*v.rr
			} else {
				stop = tb.pierced * 1.002
				if stop <= entry {
					stop = entry * 1.01
				}
				risk := stop - entry
				target = entry - risk*v.rr
			}
			r := simulate(ks, i, long, entry, stop, target, hor) - v.feeR
			trades++
			sum += r
			if r > 0 {
				wins++
			}
			cool = i
			continue
		}

		// 123
		o := detect123(ks[:i+1], v.method, v.minTouch)
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
		long := o.dir == "to_bull"
		if v.nest {
			if long && nestBias != "bull" {
				continue
			}
			if !long && nestBias != "bear" {
				continue
			}
		}
		lastKey = key
		entry := ks[i].C
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
			target = entry + risk*v.rr
		} else {
			stop = o.s2
			if stop <= entry {
				stop = o.prior
			}
			if stop <= entry {
				stop = entry * 1.01
			}
			risk := stop - entry
			target = entry - risk*v.rr
		}
		r := simulate(ks, i, long, entry, stop, target, hor) - v.feeR
		trades++
		sum += r
		if r > 0 {
			wins++
		}
	}
	return
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
		out = append(out, bar{O: atof(r[1]), H: atof(r[2]), L: atof(r[3]), C: atof(r[4])})
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

func emaLast(ks []bar, period int) float64 {
	if len(ks) == 0 {
		return 0
	}
	k := 2.0 / (float64(period) + 1)
	e := ks[0].C
	for i := 1; i < len(ks); i++ {
		e = ks[i].C*k + e*(1-k)
	}
	return e
}

func classifyNest(ks []bar) string {
	if len(ks) < 60 {
		return "chop"
	}
	sw := findSwings(ks, 3)
	var highs, lows []swing
	for _, s := range sw {
		if s.kind == "h" {
			highs = append(highs, s)
		} else {
			lows = append(lows, s)
		}
	}
	last := ks[len(ks)-1].C
	ema := emaLast(ks, 50)
	if len(highs) < 2 || len(lows) < 2 {
		if last > ema {
			return "chop"
		}
		return "chop"
	}
	hhhl := highs[len(highs)-1].p > highs[len(highs)-2].p && lows[len(lows)-1].p > lows[len(lows)-2].p
	lhll := highs[len(highs)-1].p < highs[len(highs)-2].p && lows[len(lows)-1].p < lows[len(lows)-2].p
	if hhhl && last > ema {
		return "bull"
	}
	if lhll && last < ema {
		return "bear"
	}
	return "chop"
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

func scoreLine(ks []bar, pts [][2]float64, slope, icept, tol float64, support bool) (touches, viol int, sc float64) {
	for _, p := range pts {
		if math.Abs(p[1]-(icept+slope*p[0])) <= tol {
			touches++
		}
	}
	if len(pts) < 2 {
		return
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i][0] < pts[j][0] })
	a, b := int(pts[0][0]), int(pts[len(pts)-1][0])
	for i := a; i <= b && i < len(ks); i++ {
		y := icept + slope*float64(i)
		if support && ks[i].C < y-tol {
			viol++
		}
		if !support && ks[i].C > y+tol {
			viol++
		}
	}
	if (support && slope <= 0) || (!support && slope >= 0) {
		return touches, viol, -1
	}
	span := float64(b - a)
	sc = float64(touches)*3 + math.Log1p(span)*0.5 - float64(viol)*2.0
	return
}

func fitRANSAC(ks []bar, sw []swing, support bool, minTouch int, casoon bool) *line {
	tolMul := 0.3
	if casoon {
		tolMul = 0.25
	}
	tol := atr(ks, 14) * tolMul
	kind := "l"
	if !support {
		kind = "h"
	}
	pts := pivots(sw, kind, 12)
	n := len(pts)
	var best *line
	bestSc := -1.0
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
			if len(inl) < minTouch {
				continue
			}
			s2, a2, ok := ols(inl)
			if !ok {
				continue
			}
			touches, viol, sc := scoreLine(ks, inl, s2, a2, tol, support)
			if casoon && (touches < minTouch || viol > 2 || sc < 0) {
				continue
			}
			if sc > bestSc {
				bestSc = sc
				sort.Slice(inl, func(a, b int) bool { return inl[a][0] < inl[b][0] })
				best = &line{s2, a2, tol, touches, viol, int(inl[0][0]), int(inl[len(inl)-1][0]), sc}
			}
		}
	}
	return best
}

func firstBreak(ks []bar, ln *line, support bool) int {
	if ln == nil {
		return -1
	}
	for i := ln.end + 1; i < len(ks); i++ {
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
	confirmed bool
	s2, prior float64
	t3        int
}

func detect123(ks []bar, method string, minTouch int) sig {
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
	casoon := method == "casoon"
	m := method
	if casoon {
		m = "ransac"
	}
	if s := detectBear(ks, highs, lows, sw, m, minTouch, casoon); s.confirmed {
		return s
	}
	if s := detectBull(ks, highs, lows, sw, m, minTouch, casoon); s.confirmed {
		return s
	}
	return sig{}
}

func detectBear(ks []bar, highs, lows []swing, sw []swing, method string, minTouch int, casoon bool) sig {
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
	case "ols", "ransac":
		ln := fitRANSAC(ks, sw, true, minTouch, casoon)
		if method == "ols" {
			// reuse ransac engine with looser; ols = fit all lows
			pts := pivots(sw, "l", 8)
			if s, a, ok := ols(pts); ok {
				tol := atr(ks, 14) * 0.25
				touches, viol, sc := scoreLine(ks, pts, s, a, tol, true)
				if sc > 0 && touches >= minTouch {
					sort.Slice(pts, func(i, j int) bool { return pts[i][0] < pts[j][0] })
					ln = &line{s, a, tol, touches, viol, int(pts[0][0]), int(pts[len(pts)-1][0]), sc}
				}
			}
		}
		stage1 = firstBreak(ks, ln, true)
	default:
		for i := hl.i + 1; i < len(ks); i++ {
			if ks[i].C < hl.p {
				stage1 = i
				break
			}
		}
	}
	if stage1 < 0 {
		return sig{}
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
		return sig{}
	}
	react := bounceHi
	for i := stage1; i <= bounceIdx; i++ {
		if ks[i].L < react {
			react = ks[i].L
		}
	}
	for i := bounceIdx + 1; i < len(ks); i++ {
		if ks[i].C < react {
			return sig{dir: "to_bear", confirmed: true, s2: bounceHi, prior: prior, t3: i}
		}
	}
	if ks[len(ks)-1].C < hl.p && bounceIdx > stage1 {
		return sig{dir: "to_bear", confirmed: true, s2: bounceHi, prior: prior, t3: len(ks) - 1}
	}
	return sig{}
}

func detectBull(ks []bar, highs, lows []swing, sw []swing, method string, minTouch int, casoon bool) sig {
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
	case "ols", "ransac":
		ln := fitRANSAC(ks, sw, false, minTouch, casoon)
		if method == "ols" {
			pts := pivots(sw, "h", 8)
			if s, a, ok := ols(pts); ok {
				tol := atr(ks, 14) * 0.25
				touches, viol, sc := scoreLine(ks, pts, s, a, tol, false)
				if sc > 0 && touches >= minTouch {
					sort.Slice(pts, func(i, j int) bool { return pts[i][0] < pts[j][0] })
					ln = &line{s, a, tol, touches, viol, int(pts[0][0]), int(pts[len(pts)-1][0]), sc}
				}
			}
		}
		stage1 = firstBreak(ks, ln, false)
	default:
		for i := lh.i + 1; i < len(ks); i++ {
			if ks[i].C > lh.p {
				stage1 = i
				break
			}
		}
	}
	if stage1 < 0 {
		return sig{}
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
		return sig{}
	}
	react := bounceLo
	for i := stage1; i <= bounceIdx; i++ {
		if ks[i].H > react {
			react = ks[i].H
		}
	}
	for i := bounceIdx + 1; i < len(ks); i++ {
		if ks[i].C > react {
			return sig{dir: "to_bull", confirmed: true, s2: bounceLo, prior: prior, t3: i}
		}
	}
	if ks[len(ks)-1].C > lh.p && bounceIdx > stage1 {
		return sig{dir: "to_bull", confirmed: true, s2: bounceLo, prior: prior, t3: len(ks) - 1}
	}
	return sig{}
}

type twoB struct {
	side    string
	pierced float64
	barsAgo int
}

func detect2B(ks []bar) *twoB {
	sw := findSwings(ks, 3)
	n := len(ks)
	if n < 20 || len(sw) < 2 {
		return nil
	}
	start := n - 16
	if start < 1 {
		start = 1
	}
	var best *twoB
	bestAge := 1 << 30
	for i := start; i < n; i++ {
		var refH, refL float64
		hasH, hasL := false, false
		for _, s := range sw {
			if s.i >= i {
				break
			}
			if s.kind == "h" {
				refH, hasH = s.p, true
			} else {
				refL, hasL = s.p, true
			}
		}
		hi, lo, c := ks[i].H, ks[i].L, ks[i].C
		age := n - 1 - i
		if hasH && hi > refH && c < refH && age < bestAge {
			bestAge = age
			best = &twoB{"short", hi, age}
		}
		if hasL && lo < refL && c > refL && age < bestAge {
			bestAge = age
			best = &twoB{"long", lo, age}
		}
	}
	return best
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
