package server

import (
	"context"
	"sync"

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
	VolumeBias   string  `json:"volumeBias"` // 红肥绿瘦 | 绿肥红瘦 | 均衡 | 不足
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

type analysisUDView struct {
	Boxes    []udTFBoxView  `json:"boxes"`
	Cascade  udCascadeView  `json:"cascade"`
	Backtest udCascadeBT    `json:"backtest"`
	Rules    []string       `json:"rules"`
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
		return "红肥绿瘦", 99
	}
	ratio := up / down
	switch {
	case ratio >= 1.25:
		return "红肥绿瘦", roundFloat(ratio, 2)
	case ratio <= 0.8:
		return "绿肥红瘦", roundFloat(ratio, 2)
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
	switch iv {
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
		case box.InLowerZone(last, udBuyZone):
			v.Zone = "lower"
			v.Phase = "range"
		case box.InUpperZone(last, udSellZone):
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

func buildUDView(h4ks, dks, wks []types.KLine) analysisUDView {
	h4 := classifyUDBox(types.Interval4h, h4ks)
	d := classifyUDBox(types.Interval1d, dks)
	w := classifyUDBox(types.Interval1w, wks)
	return analysisUDView{
		Boxes:   []udTFBoxView{h4, d, w},
		Cascade: buildUDCascade(h4, d, w),
		Rules: []string{
			"优道嵌套：4h 破关键支撑 → 日线出空预警；日线再破 → 周线出空。反向同理。",
			"箱体用近期高低锁定，过宽不锁；锁住后不再用滚动窗口把箱拉开。",
			"变盘 = 收盘出箱，不是 EMA 也不是反弹满 N 小时。",
			"箱内只做上下沿（约 15% 区）且最好先收敛（起涨点）；中腰空仓。",
			"大周期管小周期：周线未出空时，4h 出空只当中级回调。",
			"换线时刻 = 下一根 4h / 日 / 周 / 月 收盘，用来重新判定，不是闹钟。",
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
		box, _, locked := pickUDBox(prefix, windows, udMinBoxWidth, udMaxBoxWidth4h)
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
