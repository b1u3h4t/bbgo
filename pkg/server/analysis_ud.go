package server

import (
	"context"
	"sync"
	"time"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/strategy/udbox"
	"github.com/c9s/bbgo/pkg/types"
)

const (
	udBreakBuffer   = 0.001
	udMinBoxWidth   = 0.015
	udMaxBoxWidth4h = 0.05
	udMaxBoxWidthHT = 0.08
	udBuyZone       = 0.15
	udSellZone      = 0.15
)

// UD nested-box snapshot matching 优道: lock a tradable box, act at edges,
// cascade 4h break → daily 出空/出多 watch → weekly confirm. Time is 换线
// (next HTF close), not an EMA-duration alarm.

type udTFBoxView struct {
	Interval     string  `json:"interval"`
	Locked       bool    `json:"locked"`
	Window       int     `json:"window"`
	Top          float64 `json:"top"`
	Bottom       float64 `json:"bottom"`
	Mid          float64 `json:"mid"`
	WidthPct     float64 `json:"widthPct"`
	Last         float64 `json:"last"`
	PosPct       float64 `json:"posPct"`
	Zone         string  `json:"zone"` // lower | mid | upper | above | below
	Compressing  bool    `json:"compressing"`
	Phase        string  `json:"phase"` // range | break_up | break_down | no_box
	KeySupport   float64 `json:"keySupport"`
	KeyResist    float64 `json:"keyResist"`
	VolumeBias   string  `json:"volumeBias"` // 绿肥红瘦 | 红肥绿瘦 | 均衡 | 不足（币圈绿涨红跌）
	UpDownVol    float64 `json:"upDownVol"`
	Note         string  `json:"note"`
}

type udCascadeView struct {
	Signal      string  `json:"signal"`
	Label       string  `json:"label"`
	Trigger     string  `json:"trigger"`
	KeySupport  float64 `json:"keySupport"`
	KeyResist   float64 `json:"keyResist"`
	NestWeekly  string  `json:"nestWeekly"` // bull | bear | chop
	Action      string  `json:"action"`
	WatchDaily  bool    `json:"watchDaily"`
	WatchWeekly bool    `json:"watchWeekly"`
}

type udCascadeBT struct {
	Symbols      int     `json:"symbols"`
	BreaksDown   int     `json:"breaksDown"`
	Followed     int     `json:"followed"`
	Failed       int     `json:"failed"`
	FollowRate   float64 `json:"followRate"`
	BreaksUp     int     `json:"breaksUp"`
	FollowedUp   int     `json:"followedUp"`
	FollowRateUp float64 `json:"followRateUp"`
	HorizonBars  int     `json:"horizonBars"`
	Note         string  `json:"note"`
}

// 15m intraday trigger under 4h nest (professional speculation: HTF bias, LTF entry).
type udTwoBView struct {
	Side       string  `json:"side"` // long | short — trade after failed break
	Pierced    float64 `json:"pierced"`
	BarsAgo    int     `json:"barsAgo"`
	ClosedBack bool    `json:"closedBack"`
	Note       string  `json:"note"`
}

type udIntradayView struct {
	Box     udTFBoxView `json:"box"`
	Nest4h  string      `json:"nest4h"` // bull | bear | chop
	Aligned bool        `json:"aligned"`
	Setup   string      `json:"setup"` // wait | break_long | break_short | two_b_long | two_b_short | ignore
	Label   string      `json:"label"`
	Action  string      `json:"action"`
	Stop    float64     `json:"stop"`
	Target  float64     `json:"target"`
	TwoB    *udTwoBView `json:"twoB,omitempty"`
	Next15m *regimeClock `json:"next15m,omitempty"`
}

type analysisUDView struct {
	Boxes    []udTFBoxView   `json:"boxes"`
	Cascade  udCascadeView   `json:"cascade"`
	Intraday *udIntradayView `json:"intraday,omitempty"`
	Backtest udCascadeBT     `json:"backtest"`
	Rules    []string        `json:"rules"`
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

func pickUDBox(klines []types.KLine, windows []int, minW, maxW float64) (udbox.Box, int, bool) {
	hist := closedHist(klines)
	for _, w := range windows {
		if b, ok := udbox.DetectBox(hist, w, minW, maxW); ok {
			return b, w, true
		}
	}
	if len(windows) == 0 || len(hist) < windows[0] {
		return udbox.Box{}, 0, false
	}
	raw, _ := udbox.DetectBox(hist, windows[0], 0, 10)
	return raw, windows[0], false
}

func volumeBias(ks []types.KLine, look int) (string, float64) {
	hist := closedHist(ks)
	n := len(hist)
	if n < 8 {
		return "不足", 0
	}
	if look > n {
		look = n
	}
	var up, down float64
	for i := n - look; i < n; i++ {
		vol := hist[i].Volume.Float64()
		if hist[i].Close.Float64() >= hist[i].Open.Float64() {
			up += vol
		} else {
			down += vol
		}
	}
	if down <= 0 {
		return "绿肥红瘦", 99
	}
	ratio := up / down
	switch {
	case ratio >= 1.25:
		return "绿肥红瘦", roundFloat(ratio, 2)
	case ratio <= 0.8:
		return "红肥绿瘦", roundFloat(ratio, 2)
	default:
		return "均衡", roundFloat(ratio, 2)
	}
}

func classifyUDBox(iv types.Interval, ks []types.KLine) udTFBoxView {
	v := udTFBoxView{Interval: string(iv)}
	if len(ks) == 0 {
		v.Phase = "no_box"
		v.Note = "无K线"
		return v
	}
	last := ks[len(ks)-1].Close.Float64()
	v.Last = roundFloat(last, 8)

	windows := []int{12, 20}
	minW, maxW := udMinBoxWidth, udMaxBoxWidth4h
	look := 10
	zonePct := udBuyZone
	switch iv {
	case types.Interval15m:
		// match config/udbox-backtest-15m.yaml: longer box, wider min, tighter zones
		windows = []int{40, 48}
		minW = 0.025
		maxW = 0.06
		look = 20
		zonePct = 0.10
	case types.Interval1d:
		windows = []int{10, 20}
		maxW = udMaxBoxWidthHT
		look = 10
	case types.Interval1w:
		windows = []int{8, 16}
		maxW = udMaxBoxWidthHT
		look = 8
	case types.Interval1h:
		windows = []int{20, 40}
		look = 20
	}

	box, win, locked := pickUDBox(ks, windows, minW, maxW)
	v.Window = win
	v.Locked = locked
	v.Compressing = udbox.VolatilityCompressing(closedHist(ks), look)
	v.VolumeBias, v.UpDownVol = volumeBias(ks, 20)

	if box.Valid() {
		v.Top = roundFloat(box.Top, 8)
		v.Bottom = roundFloat(box.Bottom, 8)
		v.Mid = roundFloat(box.Mid(), 8)
		v.WidthPct = roundFloat(box.WidthPct()*100, 2)
		v.KeySupport = v.Bottom
		v.KeyResist = v.Top
		if box.Width() > 0 {
			v.PosPct = roundFloat((last-box.Bottom)/box.Width()*100, 1)
		}
		switch {
		case box.BreakLong(last, udBreakBuffer):
			v.Zone = "above"
			v.Phase = "break_up"
		case box.BreakShort(last, udBreakBuffer):
			v.Zone = "below"
			v.Phase = "break_down"
		case box.InLowerZone(last, zonePct):
			v.Zone = "lower"
			v.Phase = "range"
		case box.InUpperZone(last, zonePct):
			v.Zone = "upper"
			v.Phase = "range"
		default:
			v.Zone = "mid"
			v.Phase = "range"
		}
		if !locked {
			v.Phase = "no_box"
			v.Note = "箱过宽或过窄，不锁（优道：不是可交易箱）"
		} else if !v.Compressing && v.Phase == "range" {
			v.Note = "已锁箱但未收敛，不做沿、等起涨点"
		} else if v.Phase == "range" {
			v.Note = "锁箱震荡"
		} else if v.Phase == "break_down" {
			v.Note = "收盘跌破箱底"
		} else if v.Phase == "break_up" {
			v.Note = "收盘升破箱顶"
		}
		return v
	}

	hiIdx, lowIdx, hi, lo := findRecentSwing(ks)
	v.Phase = "no_box"
	v.KeySupport = roundFloat(lo, 8)
	v.KeyResist = roundFloat(hi, 8)
	v.Bottom, v.Top = v.KeySupport, v.KeyResist
	if v.Top > v.Bottom {
		v.Mid = roundFloat((v.Top+v.Bottom)/2, 8)
		v.WidthPct = roundFloat((v.Top-v.Bottom)/v.Bottom*100, 2)
		v.PosPct = roundFloat((last-v.Bottom)/(v.Top-v.Bottom)*100, 1)
	}
	_ = hiIdx
	_ = lowIdx
	v.Note = "无合格箱，仅标摆动高低作关键支撑/压力"
	return v
}

func nestBiasFromBox(b udTFBoxView) string {
	switch b.Phase {
	case "break_up":
		return "bull"
	case "break_down":
		return "bear"
	default:
		if b.Zone == "above" {
			return "bull"
		}
		if b.Zone == "below" {
			return "bear"
		}
		return "chop"
	}
}

func buildUDCascade(h4, d, w udTFBoxView) udCascadeView {
	c := udCascadeView{
		Signal:     "unchanged",
		Label:      "未变盘",
		NestWeekly: nestBiasFromBox(w),
		KeySupport: h4.KeySupport,
		KeyResist:  h4.KeyResist,
	}

	h4Down := h4.Phase == "break_down" || h4.Zone == "below"
	h4Up := h4.Phase == "break_up" || h4.Zone == "above"
	dDown := d.Phase == "break_down" || d.Zone == "below"
	dUp := d.Phase == "break_up" || d.Zone == "above"
	wDown := w.Phase == "break_down" || w.Zone == "below"
	wUp := w.Phase == "break_up" || w.Zone == "above"

	switch {
	case dDown && wDown:
		c.Signal = "weekly_short"
		c.Label = "周线出空"
		c.Trigger = "日线收破关键支撑后，周线亦在箱下/破位"
		c.WatchWeekly = true
		c.KeySupport = w.KeySupport
	case h4Down && dDown:
		c.Signal = "daily_short"
		c.Label = "日线出空"
		c.Trigger = "4h 收破关键支撑后，日线跟随出空"
		c.WatchWeekly = true
		c.KeySupport = d.KeySupport
	case h4Down:
		c.Signal = "daily_short_watch"
		c.Label = "4h出空 → 日线出空预警"
		c.Trigger = "4h 收盘跌破关键支撑，盯日线是否跟随"
		c.WatchDaily = true
		c.KeySupport = h4.KeySupport
	case dUp && wUp:
		c.Signal = "weekly_long"
		c.Label = "周线出多"
		c.Trigger = "日线收破箱顶后，周线亦在箱上"
		c.WatchWeekly = true
		c.KeyResist = w.KeyResist
	case h4Up && dUp:
		c.Signal = "daily_long"
		c.Label = "日线出多"
		c.Trigger = "4h 收破箱顶后，日线跟随出多"
		c.WatchWeekly = true
		c.KeyResist = d.KeyResist
	case h4Up:
		c.Signal = "daily_long_watch"
		c.Label = "4h出多 → 日线出多预警"
		c.Trigger = "4h 收盘升破箱顶，盯日线是否跟随"
		c.WatchDaily = true
		c.KeyResist = h4.KeyResist
	}

	switch {
	case c.Signal == "unchanged" && h4.Locked && h4.Zone == "lower" && h4.Compressing:
		c.Action = "箱下沿：震荡试多，止损收盘破箱底；仓小于趋势仓"
	case c.Signal == "unchanged" && h4.Locked && h4.Zone == "upper" && h4.Compressing:
		c.Action = "箱上沿：震荡试空，止损收盘破箱顶；若周线嵌套仍多则空仓宁缺"
	case c.Signal == "unchanged" && h4.Locked && h4.Zone == "mid":
		c.Action = "中腰不追。等下沿/上沿，或等 4h 收盘出箱"
	case c.Signal == "unchanged" && !h4.Locked:
		c.Action = "箱过宽不锁：空仓等收敛出新箱，或等收盘明确出结构"
	case c.Signal == "daily_short_watch" && c.NestWeekly == "bull":
		c.Action = "只当中级回调。降杠杆；长空等日线出空且周线未破才加重。盯 " +
			formatLevel(d.KeySupport) + " 日线支撑"
	case c.Signal == "daily_short_watch":
		c.Action = "4h 已出空，等日线收盘确认。未确认前不按已变盘加空"
	case c.Signal == "daily_short" && c.NestWeekly != "bear":
		c.Action = "日线出空、周线未出空：中期偏空，长周期仍按嵌套。破周线支撑才算长空"
	case c.Signal == "daily_long_watch" && c.NestWeekly == "bear":
		c.Action = "4h 反抽，周线嵌套仍空：当反弹，不按长多加仓"
	case c.Signal == "weekly_short":
		c.Action = "长周期出空：趋势跟随，箱边/新箱移动停损"
	case c.Signal == "weekly_long":
		c.Action = "长周期出多：趋势跟随，箱边/新箱移动停损"
	default:
		if c.Action == "" {
			c.Action = "未收盘出箱前按震荡。变盘只认收盘，不认插针"
		}
	}
	return c
}

func formatLevel(v float64) string {
	if v >= 1000 {
		return trimFloat(roundFloat(v, 0))
	}
	return trimFloat(roundFloat(v, 4))
}

// detectTwoB finds the most recent Sperandeo-style 2B: pierce box edge then close back inside.
func detectTwoB(ks []types.KLine, box udbox.Box, lookback int) *udTwoBView {
	hist := closedHist(ks)
	n := len(hist)
	if !box.Valid() || n < 4 {
		return nil
	}
	if lookback < 3 {
		lookback = 8
	}
	start := n - lookback
	if start < 0 {
		start = 0
	}
	var best *udTwoBView
	bestAge := 1 << 30
	for pierce := start; pierce < n; pierce++ {
		hi := hist[pierce].High.Float64()
		lo := hist[pierce].Low.Float64()
		if hi > box.Top*(1+udBreakBuffer) {
			for j := pierce; j < n; j++ {
				c := hist[j].Close.Float64()
				if c < box.Top && c > box.Bottom {
					age := n - 1 - j
					if age < bestAge {
						bestAge = age
						best = &udTwoBView{
							Side:       "short",
							Pierced:    roundFloat(hi, 8),
							BarsAgo:    age,
							ClosedBack: true,
							Note:       "假上破后收回箱内 → 2B 试空（须与 4h 嵌套同向）",
						}
					}
					break
				}
			}
		}
		if lo < box.Bottom*(1-udBreakBuffer) {
			for j := pierce; j < n; j++ {
				c := hist[j].Close.Float64()
				if c > box.Bottom && c < box.Top {
					age := n - 1 - j
					if age < bestAge {
						bestAge = age
						best = &udTwoBView{
							Side:       "long",
							Pierced:    roundFloat(lo, 8),
							BarsAgo:    age,
							ClosedBack: true,
							Note:       "假下破后收回箱内 → 2B 试多（须与 4h 嵌套同向）",
						}
					}
					break
				}
			}
		}
	}
	return best
}

func h4TradeNest(h4 udTFBoxView) string {
	// mid-waist locked range = no intraday main line
	if h4.Locked && h4.Phase == "range" && h4.Zone == "mid" {
		return "chop"
	}
	b := nestBiasFromBox(h4)
	if h4.Locked && h4.Phase == "range" {
		switch h4.Zone {
		case "lower":
			return "bull" // only long-side probes at HTF support
		case "upper":
			return "bear"
		}
	}
	return b
}

func buildUDIntraday(m15ks, h4ks []types.KLine, now time.Time) *udIntradayView {
	m15 := classifyUDBox(types.Interval15m, m15ks)
	h4 := classifyUDBox(types.Interval4h, h4ks)
	nest := h4TradeNest(h4)

	hist := closedHist(m15ks)
	var box udbox.Box
	if m15.Locked && m15.Top > m15.Bottom {
		box = udbox.Box{Top: m15.Top, Bottom: m15.Bottom}
	} else {
		raw, _, ok := pickUDBox(m15ks, []int{40, 48}, 0.025, 0.06)
		if ok {
			box = raw
		}
	}
	twoB := detectTwoB(m15ks, box, 12)

	clk := makeClock(now, types.Interval15m, "15m 换线：下一根 15m 收盘")
	out := &udIntradayView{
		Box:     m15,
		Nest4h:  nest,
		TwoB:    twoB,
		Next15m: &clk,
		Setup:   "wait",
		Label:   "等待",
	}

	width := m15.Top - m15.Bottom
	if width < 0 {
		width = 0
	}

	switch {
	case nest == "chop":
		out.Label = "无主线"
		out.Action = "4h 中腰/未出箱：15m 空仓。等 4h 到沿或收盘出箱后再找 15m 扳机"
		return out
	case !m15.Locked:
		out.Label = "15m 未锁箱"
		out.Action = "等 15m 收敛锁箱（窗约 40 根、宽 2.5%～6%）后再做突破或 2B"
		return out
	case m15.Phase == "break_up" && nest == "bull":
		out.Setup = "break_long"
		out.Aligned = true
		out.Label = "顺势上破"
		out.Stop = m15.Bottom
		out.Target = roundFloat(m15.Top+width*0.5, 8)
		out.Action = "4h 嵌套偏多 + 15m 收盘破顶：趋势多，停损箱底，目标约半箱延申"
	case m15.Phase == "break_down" && nest == "bear":
		out.Setup = "break_short"
		out.Aligned = true
		out.Label = "顺势下破"
		out.Stop = m15.Top
		out.Target = roundFloat(m15.Bottom-width*0.5, 8)
		out.Action = "4h 嵌套偏空 + 15m 收盘破底：趋势空，停损箱顶"
	case m15.Phase == "break_up" && nest == "bear":
		out.Setup = "ignore"
		out.Label = "逆嵌套上破"
		out.Action = "4h 偏空时的 15m 上破当诱饵；优先等收回后的 2B 空，不追多"
	case m15.Phase == "break_down" && nest == "bull":
		out.Setup = "ignore"
		out.Label = "逆嵌套下破"
		out.Action = "4h 偏多时的 15m 下破当诱饵；优先等收回后的 2B 多，不追空"
	case twoB != nil && twoB.Side == "long" && nest == "bull" && twoB.BarsAgo <= 4:
		out.Setup = "two_b_long"
		out.Aligned = true
		out.Label = "2B 试多"
		out.Stop = roundFloat(twoB.Pierced*(1-udBreakBuffer), 8)
		if out.Stop > m15.Bottom || out.Stop <= 0 {
			out.Stop = roundFloat(m15.Bottom*(1-udBreakBuffer), 8)
		}
		out.Target = m15.Mid
		if m15.Zone == "lower" || m15.PosPct < 40 {
			out.Target = m15.Top
		}
		out.Action = "假跌破收回 + 4h 偏多：试多，止损刺穿极值外，目标箱中/对侧"
	case twoB != nil && twoB.Side == "short" && nest == "bear" && twoB.BarsAgo <= 4:
		out.Setup = "two_b_short"
		out.Aligned = true
		out.Label = "2B 试空"
		out.Stop = roundFloat(twoB.Pierced*(1+udBreakBuffer), 8)
		if out.Stop < m15.Top {
			out.Stop = roundFloat(m15.Top*(1+udBreakBuffer), 8)
		}
		out.Target = m15.Mid
		if m15.Zone == "upper" || m15.PosPct > 60 {
			out.Target = m15.Bottom
		}
		out.Action = "假升破收回 + 4h 偏空：试空，止损刺穿极值外，目标箱中/对侧"
	case m15.Locked && m15.Compressing && m15.Zone == "mid":
		out.Label = "15m 中腰等沿"
		out.Action = "已锁且收敛，人在中腰：不追。等收到上下沿或收盘出箱；方向跟 4h=" + nest
	case m15.Locked && !m15.Compressing:
		out.Label = "等起涨点"
		out.Action = "15m 已锁但未收敛：不做沿、不追突破，等波动压后再扳机"
	default:
		out.Label = "观望"
		out.Action = "无合格 15m 扳机。仓位≤趋势仓 1/3，单笔按箱宽控险"
	}
	_ = hist
	return out
}

func buildUDView(h4ks, dks, wks, m15ks []types.KLine, now time.Time) analysisUDView {
	h4 := classifyUDBox(types.Interval4h, h4ks)
	d := classifyUDBox(types.Interval1d, dks)
	w := classifyUDBox(types.Interval1w, wks)
	intraday := buildUDIntraday(m15ks, h4ks, now)
	return analysisUDView{
		Boxes:    []udTFBoxView{h4, d, w},
		Cascade:  buildUDCascade(h4, d, w),
		Intraday: intraday,
		Rules: []string{
			"优道嵌套：4h 破关键支撑 → 日线出空预警；日线再破 → 周线出空。反向同理。",
			"箱体用近期高低锁定，过宽不锁；锁住后不再用滚动窗口把箱拉开。",
			"变盘 = 收盘出箱，不是 EMA 也不是反弹满 N 小时。",
			"箱内只做上下沿（约 15% 区）且最好先收敛（起涨点）；中腰空仓。",
			"大周期管小周期：周线未出空时，4h 出空只当中级回调。",
			"15m 只做扳机：4h 定多空，15m 只做同向突破或 2B；4h 中腰则 15m 空仓。",
			"2B：刺穿箱沿后收盘收回箱内，且须与 4h 嵌套同向；逆嵌套突破当诱饵。",
			"换线时刻 = 下一根 15m / 4h / 日 / 周 / 月 收盘，用来重新判定，不是闹钟。",
		},
	}
}

func backtestUDCascade(klines []types.KLine, horizonBars int) (downN, followDown, failDown, upN, followUp int) {
	hist := closedHist(klines)
	n := len(hist)
	if n < 80 || horizonBars < 2 {
		return
	}
	windows := []int{12, 20}
	for i := 40; i < n-horizonBars; i++ {
		prefix := hist[:i+1]
		box, _, locked := pickUDBox(prefix, windows, udMinBoxWidth, udMaxBoxWidthHT)
		if !locked {
			continue
		}
		c := hist[i].Close.Float64()
		end := i + horizonBars
		if box.BreakShort(c, udBreakBuffer) {
			downN++
			reclaimed := false
			extended := false
			for j := i + 1; j <= end && j < n; j++ {
				if hist[j].Close.Float64() > box.Top {
					reclaimed = true
					break
				}
				if hist[j].Low.Float64() < box.Bottom*(1-0.01) {
					extended = true
				}
			}
			if extended && !reclaimed {
				followDown++
			} else if reclaimed {
				failDown++
			}
		} else if box.BreakLong(c, udBreakBuffer) {
			upN++
			reclaimed := false
			extended := false
			for j := i + 1; j <= end && j < n; j++ {
				if hist[j].Close.Float64() < box.Bottom {
					reclaimed = true
					break
				}
				if hist[j].High.Float64() > box.Top*(1+0.01) {
					extended = true
				}
			}
			if extended && !reclaimed {
				followUp++
			}
		}
	}
	return
}

func (s *Server) udCascadeBacktestPool(
	ctx context.Context,
	session *bbgo.ExchangeSession,
	symbols []string,
) udCascadeBT {
	bt := udCascadeBT{
		HorizonBars: 18, // 3 days of 4h
		Note:        "4h 收盘破合格箱后 18 根内：续破 1% 且不收回箱顶 = 日线级跟随",
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	ok := 0
	for _, sy := range symbols {
		wg.Add(1)
		go func(sy string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ks, _, err := s.queryAnalysisKLines(ctx, session, sy, types.Interval4h, 300)
			if err != nil || len(ks) < 80 {
				return
			}
			dN, fD, xD, uN, fU := backtestUDCascade(ks, 18)
			mu.Lock()
			defer mu.Unlock()
			ok++
			bt.BreaksDown += dN
			bt.Followed += fD
			bt.Failed += xD
			bt.BreaksUp += uN
			bt.FollowedUp += fU
		}(sy)
	}
	wg.Wait()
	bt.Symbols = ok
	if bt.BreaksDown > 0 {
		bt.FollowRate = roundFloat(100*float64(bt.Followed)/float64(bt.BreaksDown), 1)
	}
	if bt.BreaksUp > 0 {
		bt.FollowRateUp = roundFloat(100*float64(bt.FollowedUp)/float64(bt.BreaksUp), 1)
	}
	return bt
}
