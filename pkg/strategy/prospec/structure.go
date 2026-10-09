package prospec

import (
	"math"
	"time"

	"github.com/c9s/bbgo/pkg/types"
)

// Swing is a confirmed pivot high/low.
type Swing struct {
	Index int       `json:"index"`
	Price float64   `json:"price"`
	Time  time.Time `json:"time"`
	Kind  string    `json:"kind"` // high | low
}

// NestState is higher-timeframe bias for Sperandeo-style nesting.
type NestState struct {
	Interval string  `json:"interval"`
	Bias     string  `json:"bias"` // bull | bear | chop
	Last     float64 `json:"last"`
	EMA50    float64 `json:"ema50"`
	LastHH   float64 `json:"lastHH"`
	LastHL   float64 `json:"lastHL"`
	LastLH   float64 `json:"lastLH"`
	LastLL   float64 `json:"lastLL"`
	Note     string  `json:"note"`
}

// OneTwoThree is Sperandeo's 1-2-3 trend-change checklist.
type OneTwoThree struct {
	Direction    string    `json:"direction"` // to_bear | to_bull | none
	Stage        int       `json:"stage"`     // 0..3
	Confirmed    bool      `json:"confirmed"`
	Stage1Price  float64   `json:"stage1Price"`
	Stage2Price  float64   `json:"stage2Price"`
	Stage3Price  float64   `json:"stage3Price"`
	Stage1Time   time.Time `json:"stage1Time,omitempty"`
	Stage2Time   time.Time `json:"stage2Time,omitempty"`
	Stage3Time   time.Time `json:"stage3Time,omitempty"`
	PriorExtreme float64   `json:"priorExtreme"` // prior HH (to_bear) or LL (to_bull)
	Note         string    `json:"note"`
}

// TwoBSignal is a failed breakout (2B rule).
type TwoBSignal struct {
	Side       string    `json:"side"` // long | short
	Pierced    float64   `json:"pierced"`
	Level      float64   `json:"level"`
	Time       time.Time `json:"time,omitempty"`
	BarsAgo    int       `json:"barsAgo"`
	ClosedBack bool      `json:"closedBack"`
	Note       string    `json:"note"`
}

// Setup is an actionable plan under nest filter.
type Setup struct {
	Kind    string  `json:"kind"` // none | one_two_three | two_b | wait
	Side    string  `json:"side"` // long | short | flat
	Aligned bool    `json:"aligned"`
	Label   string  `json:"label"`
	Action  string  `json:"action"`
	Entry   float64 `json:"entry"`
	Stop    float64 `json:"stop"`
	Target  float64 `json:"target"`
}

func closedHist(ks []types.KLine) []types.KLine {
	if len(ks) == 0 {
		return ks
	}
	if !ks[len(ks)-1].Closed {
		return ks[:len(ks)-1]
	}
	return ks
}

func isSwingHigh(highs []float64, i, look int) bool {
	if i < look || i+look >= len(highs) {
		return false
	}
	for j := 1; j <= look; j++ {
		if highs[i] <= highs[i-j] || highs[i] < highs[i+j] {
			return false
		}
	}
	return true
}

func isSwingLow(lows []float64, i, look int) bool {
	if i < look || i+look >= len(lows) {
		return false
	}
	for j := 1; j <= look; j++ {
		if lows[i] >= lows[i-j] || lows[i] > lows[i+j] {
			return false
		}
	}
	return true
}

// FindSwings returns chronological pivots.
func FindSwings(ks []types.KLine, look int) []Swing {
	hist := closedHist(ks)
	n := len(hist)
	if look < 2 {
		look = 3
	}
	if n < look*2+3 {
		return nil
	}
	highs := make([]float64, n)
	lows := make([]float64, n)
	for i, k := range hist {
		highs[i] = k.High.Float64()
		lows[i] = k.Low.Float64()
	}
	var out []Swing
	for i := look; i < n-look; i++ {
		if isSwingHigh(highs, i, look) {
			out = append(out, Swing{
				Index: i, Price: highs[i], Time: hist[i].StartTime.Time().UTC(), Kind: "high",
			})
		}
		if isSwingLow(lows, i, look) {
			out = append(out, Swing{
				Index: i, Price: lows[i], Time: hist[i].StartTime.Time().UTC(), Kind: "low",
			})
		}
	}
	return out
}

func emaSeries(xs []float64, period int) []float64 {
	n := len(xs)
	out := make([]float64, n)
	if n == 0 || period < 1 {
		return out
	}
	out[0] = xs[0]
	k := 2.0 / (float64(period) + 1)
	for i := 1; i < n; i++ {
		out[i] = xs[i]*k + out[i-1]*(1-k)
	}
	return out
}

// ClassifyNest uses EMA50 + last two swing highs/lows (HH/HL vs LH/LL).
func ClassifyNest(ks []types.KLine, interval string) NestState {
	st := NestState{Interval: interval, Bias: "chop", Note: "数据不足"}
	hist := closedHist(ks)
	n := len(hist)
	if n < 60 {
		return st
	}
	closes := make([]float64, n)
	for i, k := range hist {
		closes[i] = k.Close.Float64()
	}
	ema := emaSeries(closes, 50)
	last := closes[n-1]
	st.Last = round8(last)
	st.EMA50 = round8(ema[n-1])

	sw := FindSwings(hist, 3)
	var highs, lows []Swing
	for _, s := range sw {
		if s.Kind == "high" {
			highs = append(highs, s)
		} else {
			lows = append(lows, s)
		}
	}
	if len(highs) >= 2 {
		st.LastHH = round8(highs[len(highs)-1].Price)
		st.LastLH = round8(highs[len(highs)-2].Price)
	}
	if len(lows) >= 2 {
		st.LastHL = round8(lows[len(lows)-1].Price)
		st.LastLL = round8(lows[len(lows)-2].Price)
	}

	hhhl := len(highs) >= 2 && len(lows) >= 2 &&
		highs[len(highs)-1].Price > highs[len(highs)-2].Price &&
		lows[len(lows)-1].Price > lows[len(lows)-2].Price
	lhll := len(highs) >= 2 && len(lows) >= 2 &&
		highs[len(highs)-1].Price < highs[len(highs)-2].Price &&
		lows[len(lows)-1].Price < lows[len(lows)-2].Price

	above := last > ema[n-1]
	switch {
	case hhhl && above:
		st.Bias = "bull"
		st.Note = "摆动 HH+HL 且收盘在 EMA50 上"
	case lhll && !above:
		st.Bias = "bear"
		st.Note = "摆动 LH+LL 且收盘在 EMA50 下"
	case above:
		st.Bias = "chop"
		st.Note = "价在 EMA50 上但摆动未形成清晰 HH/HL"
	case !above:
		st.Bias = "chop"
		st.Note = "价在 EMA50 下但摆动未形成清晰 LH/LL"
	}
	return st
}

// DetectOneTwoThree scans for progressive 1-2-3 stages on closed bars.
func DetectOneTwoThree(ks []types.KLine, look int) OneTwoThree {
	out := OneTwoThree{Direction: "none", Note: "未形成 1-2-3"}
	hist := closedHist(ks)
	n := len(hist)
	if n < 40 {
		out.Note = "K线不足"
		return out
	}
	sw := FindSwings(hist, look)
	if len(sw) < 4 {
		return out
	}

	// Prefer ending an uptrend → to_bear
	if o := detect123ToBear(hist, sw); o.Stage > out.Stage {
		out = o
	}
	if o := detect123ToBull(hist, sw); o.Stage > out.Stage || (o.Confirmed && !out.Confirmed) {
		if o.Confirmed || out.Direction == "none" || o.Stage >= out.Stage {
			out = o
		}
	}
	return out
}

func detect123ToBear(hist []types.KLine, sw []Swing) OneTwoThree {
	out := OneTwoThree{Direction: "to_bear", Note: "升势未破"}
	var highs, lows []Swing
	for _, s := range sw {
		if s.Kind == "high" {
			highs = append(highs, s)
		} else {
			lows = append(lows, s)
		}
	}
	if len(highs) < 2 || len(lows) < 2 {
		return OneTwoThree{Direction: "none", Note: "摆动不足"}
	}
	// last completed upswing: prior HL then HH
	hh := highs[len(highs)-1]
	hl := lows[len(lows)-1]
	if hl.Index > hh.Index {
		// need HL before last HH
		for i := len(lows) - 1; i >= 0; i-- {
			if lows[i].Index < hh.Index {
				hl = lows[i]
				break
			}
		}
	}
	priorHH := highs[len(highs)-2]
	out.PriorExtreme = priorHH.Price
	out.Stage1Price = hl.Price

	lastClose := hist[len(hist)-1].Close.Float64()
	// Stage 1: close below last HL
	stage1Idx := -1
	for i := hl.Index + 1; i < len(hist); i++ {
		if hist[i].Close.Float64() < hl.Price {
			stage1Idx = i
			out.Stage = 1
			out.Stage1Time = hist[i].StartTime.Time().UTC()
			out.Note = "① 收盘跌破上升结构低点 (HL)"
			break
		}
	}
	if stage1Idx < 0 {
		return out
	}

	// Stage 2: bounce high that fails to exceed prior HH / last HH
	failCap := hh.Price
	if priorHH.Price < failCap {
		failCap = priorHH.Price
	}
	bounceHi := 0.0
	bounceIdx := -1
	for i := stage1Idx + 1; i < len(hist); i++ {
		h := hist[i].High.Float64()
		if h > bounceHi {
			bounceHi = h
			bounceIdx = i
		}
		// if makes new high above hh, cancel to_bear path
		if hist[i].Close.Float64() > hh.Price {
			return OneTwoThree{Direction: "none", Note: "反抽创新高，1-2-3 空头作废"}
		}
	}
	if bounceIdx < 0 || bounceHi >= hh.Price {
		out.Note = "① 已破 HL，等失败反抽（不过前高）"
		return out
	}
	out.Stage = 2
	out.Stage2Price = bounceHi
	out.Stage2Time = hist[bounceIdx].StartTime.Time().UTC()
	out.Note = "② 反抽不过前高"

	// reaction low after bounce start
	reactLow := bounceHi
	reactIdx := bounceIdx
	for i := bounceIdx; i < len(hist); i++ {
		l := hist[i].Low.Float64()
		if l < reactLow {
			reactLow = l
			reactIdx = i
		}
	}
	out.Stage3Price = reactLow
	_ = reactIdx

	if lastClose < reactLow || (bounceIdx < len(hist)-1 && lastClose < out.Stage1Price && lastClose < bounceHi*0.995) {
		// Stage 3: break of reaction low (use min low after stage1 bounce peak)
		for i := bounceIdx + 1; i < len(hist); i++ {
			if hist[i].Close.Float64() < reactLow {
				out.Stage = 3
				out.Confirmed = true
				out.Stage3Time = hist[i].StartTime.Time().UTC()
				out.Stage3Price = reactLow
				out.Note = "③ 收盘跌破②的反弹低点 → 1-2-3 转空确认"
				return out
			}
		}
	}
	// also confirm if close breaks stage1 HL again after failed bounce
	if lastClose < hl.Price && bounceIdx > stage1Idx {
		out.Stage = 3
		out.Confirmed = true
		out.Stage3Price = hl.Price
		out.Stage3Time = hist[len(hist)-1].StartTime.Time().UTC()
		out.Note = "③ 失败反抽后再次收破 HL → 转空确认"
	} else {
		out.Note = "② 已现失败反抽，等收盘破反弹低点确认③"
	}
	return out
}

func detect123ToBull(hist []types.KLine, sw []Swing) OneTwoThree {
	out := OneTwoThree{Direction: "to_bull", Note: "跌势未破"}
	var highs, lows []Swing
	for _, s := range sw {
		if s.Kind == "high" {
			highs = append(highs, s)
		} else {
			lows = append(lows, s)
		}
	}
	if len(highs) < 2 || len(lows) < 2 {
		return OneTwoThree{Direction: "none", Note: "摆动不足"}
	}
	ll := lows[len(lows)-1]
	lh := highs[len(highs)-1]
	if lh.Index > ll.Index {
		for i := len(highs) - 1; i >= 0; i-- {
			if highs[i].Index < ll.Index {
				lh = highs[i]
				break
			}
		}
	}
	priorLL := lows[len(lows)-2]
	out.PriorExtreme = priorLL.Price
	out.Stage1Price = lh.Price

	stage1Idx := -1
	for i := lh.Index + 1; i < len(hist); i++ {
		if hist[i].Close.Float64() > lh.Price {
			stage1Idx = i
			out.Stage = 1
			out.Stage1Time = hist[i].StartTime.Time().UTC()
			out.Note = "① 收盘升破下降结构高点 (LH)"
			break
		}
	}
	if stage1Idx < 0 {
		return out
	}

	bounceLo := math.MaxFloat64
	bounceIdx := -1
	for i := stage1Idx + 1; i < len(hist); i++ {
		l := hist[i].Low.Float64()
		if l < bounceLo {
			bounceLo = l
			bounceIdx = i
		}
		if hist[i].Close.Float64() < ll.Price {
			return OneTwoThree{Direction: "none", Note: "回踩创新低，1-2-3 多头作废"}
		}
	}
	if bounceIdx < 0 || bounceLo <= ll.Price {
		out.Note = "① 已破 LH，等失败回踩（不破前低）"
		return out
	}
	out.Stage = 2
	out.Stage2Price = bounceLo
	out.Stage2Time = hist[bounceIdx].StartTime.Time().UTC()
	out.Note = "② 回踩不破前低"

	reactHi := bounceLo
	for i := bounceIdx; i < len(hist); i++ {
		h := hist[i].High.Float64()
		if h > reactHi {
			reactHi = h
		}
	}
	out.Stage3Price = reactHi
	lastClose := hist[len(hist)-1].Close.Float64()
	for i := bounceIdx + 1; i < len(hist); i++ {
		if hist[i].Close.Float64() > reactHi {
			out.Stage = 3
			out.Confirmed = true
			out.Stage3Time = hist[i].StartTime.Time().UTC()
			out.Note = "③ 收盘升破②的反弹高点 → 1-2-3 转多确认"
			return out
		}
	}
	if lastClose > lh.Price && bounceIdx > stage1Idx {
		out.Stage = 3
		out.Confirmed = true
		out.Stage3Price = lh.Price
		out.Stage3Time = hist[len(hist)-1].StartTime.Time().UTC()
		out.Note = "③ 失败回踩后再次收上 LH → 转多确认"
	} else {
		out.Note = "② 已现失败回踩，等收盘破反弹高点确认③"
	}
	return out
}

// DetectTwoB finds the most recent failed break of a swing high/low.
func DetectTwoB(ks []types.KLine, look int, lookbackBars int) *TwoBSignal {
	hist := closedHist(ks)
	n := len(hist)
	if n < 20 {
		return nil
	}
	sw := FindSwings(hist, look)
	if len(sw) < 2 {
		return nil
	}
	if lookbackBars < 4 {
		lookbackBars = 16
	}
	start := n - lookbackBars
	if start < 1 {
		start = 1
	}

	var best *TwoBSignal
	bestAge := 1 << 30

	// last swing high / low before each bar
	for i := start; i < n; i++ {
		var refHigh, refLow float64
		var hasH, hasL bool
		for _, s := range sw {
			if s.Index >= i {
				break
			}
			if s.Kind == "high" {
				refHigh, hasH = s.Price, true
			} else {
				refLow, hasL = s.Price, true
			}
		}
		hi := hist[i].High.Float64()
		lo := hist[i].Low.Float64()
		c := hist[i].Close.Float64()

		if hasH && hi > refHigh && c < refHigh {
			age := n - 1 - i
			if age < bestAge {
				bestAge = age
				best = &TwoBSignal{
					Side: "short", Pierced: round8(hi), Level: round8(refHigh),
					Time: hist[i].StartTime.Time().UTC(), BarsAgo: age, ClosedBack: true,
					Note: "假上破前高后收盘收回 → 2B 试空",
				}
			}
		}
		if hasL && lo < refLow && c > refLow {
			age := n - 1 - i
			if age < bestAge {
				bestAge = age
				best = &TwoBSignal{
					Side: "long", Pierced: round8(lo), Level: round8(refLow),
					Time: hist[i].StartTime.Time().UTC(), BarsAgo: age, ClosedBack: true,
					Note: "假下破前低后收盘收回 → 2B 试多",
				}
			}
		}
	}
	return best
}

// BuildSetup combines nest + 1-2-3 + 2B into one action plan.
func BuildSetup(nest NestState, o123 OneTwoThree, twoB *TwoBSignal, last float64) Setup {
	s := Setup{Kind: "wait", Side: "flat", Label: "等待", Action: "无合格信号"}

	// Confirmed 1-2-3 first
	if o123.Confirmed {
		if o123.Direction == "to_bear" && nest.Bias != "bull" {
			s.Kind = "one_two_three"
			s.Side = "short"
			s.Aligned = nest.Bias == "bear" || nest.Bias == "chop"
			s.Label = "1-2-3 转空"
			s.Entry = last
			s.Stop = o123.Stage2Price
			if s.Stop <= last {
				s.Stop = o123.PriorExtreme
			}
			s.Target = o123.Stage1Price - (s.Stop - o123.Stage1Price)
			s.Action = "趋势转空确认。停损②高点外；大周期仍多则仓位减半或只观望"
			if nest.Bias == "bull" {
				s.Aligned = false
				s.Action = "日/执行级 1-2-3 转空，但周/嵌套仍多：只当中级修正，不按熊市满仓空"
			}
			return s
		}
		if o123.Direction == "to_bull" && nest.Bias != "bear" {
			s.Kind = "one_two_three"
			s.Side = "long"
			s.Aligned = nest.Bias == "bull" || nest.Bias == "chop"
			s.Label = "1-2-3 转多"
			s.Entry = last
			s.Stop = o123.Stage2Price
			if s.Stop >= last {
				s.Stop = o123.PriorExtreme
			}
			s.Target = o123.Stage1Price + (o123.Stage1Price - s.Stop)
			s.Action = "趋势转多确认。停损②低点外"
			if nest.Bias == "bear" {
				s.Aligned = false
				s.Action = "执行级转多但嵌套仍空：当反弹，不按牛市满仓多"
			}
			return s
		}
	}

	if twoB != nil && twoB.BarsAgo <= 3 {
		if twoB.Side == "long" && nest.Bias != "bear" {
			s.Kind = "two_b"
			s.Side = "long"
			s.Aligned = nest.Bias == "bull"
			s.Label = "2B 试多"
			s.Entry = last
			s.Stop = twoB.Pierced * 0.998
			s.Target = twoB.Level + (twoB.Level - s.Stop)
			s.Action = twoB.Note + "；停损刺穿极值外，目标看反弹幅度"
			if nest.Bias == "chop" {
				s.Action += "；嵌套震荡，仓位宜小"
			}
			return s
		}
		if twoB.Side == "short" && nest.Bias != "bull" {
			s.Kind = "two_b"
			s.Side = "short"
			s.Aligned = nest.Bias == "bear"
			s.Label = "2B 试空"
			s.Entry = last
			s.Stop = twoB.Pierced * 1.002
			s.Target = twoB.Level - (s.Stop - twoB.Level)
			s.Action = twoB.Note + "；停损刺穿极值外"
			return s
		}
		if twoB != nil {
			s.Kind = "wait"
			s.Label = "2B 逆嵌套"
			s.Action = "出现 2B 但与大周期相反：忽略或极小仓试错"
			return s
		}
	}

	if o123.Stage >= 1 {
		s.Label = "1-2-3 进行中"
		s.Action = o123.Note + "（未完成前不提前开仓）"
		return s
	}

	s.Action = "等 1-2-3 完成或同向 2B。顺大周期、收盘确认、错了按停损离场"
	return s
}

func round8(v float64) float64 {
	return math.Round(v*1e8) / 1e8
}
