package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"

	"github.com/c9s/bbgo/pkg/types"
)

type trendPickRow struct {
	Symbol         string  `json:"symbol"`
	Last           float64 `json:"last"`
	QuoteVolume24h float64 `json:"quoteVolume24h"`
	VolumeRank     int     `json:"volumeRank"`
	Chg24hPct      float64 `json:"chg24hPct"`
	KlineSource    string  `json:"klineSource"`
	Bars           int     `json:"bars"`

	Bias     string `json:"bias"` // long | long_wait | short | short_wait | range | neutral
	BiasNote string `json:"biasNote"`
	Action   string `json:"action"`

	SignalClose float64 `json:"signalClose,omitempty"` // 锚定信号价（入场不跟 last 跑）
	SignalAgo   int     `json:"signalAgo,omitempty"`   // 信号距今几根 15m
	LongEntry   float64 `json:"longEntry"`
	ShortEntry  float64 `json:"shortEntry"`
	LongStop    float64 `json:"longStop"`
	ShortStop   float64 `json:"shortStop"`
	LongTP      float64 `json:"longTP"`
	ShortTP     float64 `json:"shortTP"`

	LongBT  trendSideStats `json:"longBT"`
	ShortBT trendSideStats `json:"shortBT"`
}

func isTrendScreenerSymbol(sym string) bool {
	if !strings.HasSuffix(sym, "USDT") {
		return false
	}
	u := strings.ToUpper(sym)
	for _, b := range []string{"UPUSDT", "DOWNUSDT", "BULLUSDT", "BEARUSDT"} {
		if strings.HasSuffix(u, b) {
			return false
		}
	}
	return true
}

func formatPriceHint(p float64) string {
	switch {
	case p >= 100:
		return strconv.FormatFloat(p, 'f', 2, 64)
	case p >= 1:
		return strconv.FormatFloat(p, 'f', 4, 64)
	default:
		return strconv.FormatFloat(p, 'f', 6, 64)
	}
}

func classifyFromBacktest(last float64, bt trendBTResult) (
	bias, note, action string,
	longEntry, shortEntry, longStop, shortStop, longTP, shortTP float64,
) {
	longOK := sideEdgeOK(bt.Long)
	shortOK := sideEdgeOK(bt.Short)
	side := bt.LiveSide
	if bt.LiveExpired {
		side = 0
	}

	// Only the live side gets a fixed limit from signalClose; the other side stays 0
	// so the UI does not show a chasing last-based fake entry.
	if side > 0 {
		longEntry, longStop, longTP = idealEntryFromBT(last, bt.LiveSignalClose, bt.LiveATR, 1, bt.Long)
	} else if side < 0 {
		shortEntry, shortStop, shortTP = idealEntryFromBT(last, bt.LiveSignalClose, bt.LiveATR, -1, bt.Short)
	}

	const nearEps = 0.003 // 0.3%: last at/through fixed limit → actionable
	nearLong := longEntry > 0 && last <= longEntry*(1+nearEps)
	nearShort := shortEntry > 0 && last >= shortEntry*(1-nearEps)
	// already tagged the zone on a prior bar this setup
	if bt.LiveTouched && side > 0 && longEntry > 0 && last <= longEntry*(1+0.01) {
		nearLong = true
	}
	if bt.LiveTouched && side < 0 && shortEntry > 0 && last >= shortEntry*(1-0.01) {
		nearShort = true
	}

	agoNote := ""
	if side != 0 {
		agoNote = "，信号锚定@" + formatPriceHint(bt.LiveSignalClose) + "（" + strconv.Itoa(bt.LiveBarsAgo) + "根前）"
	}

	switch {
	case longOK && side > 0:
		if nearLong {
			bias, note = "long", "回测多头有优势，现价已到锚定入场区"+agoNote
			action = "回测可多 @" + formatPriceHint(longEntry) + " WR" + strconv.FormatFloat(bt.Long.WinRate, 'f', 0, 64) + "% Exp" + strconv.FormatFloat(bt.Long.Expectancy, 'f', 2, 64) + "%"
		} else {
			bias, note = "long_wait", "回测多头有优势，等回踩到固定限价（不跟 last 漂移）"+agoNote
			action = "挂多限价 @" + formatPriceHint(longEntry) + "（信号价 " + formatPriceHint(bt.LiveSignalClose) + "）"
		}
	case shortOK && side < 0:
		if nearShort {
			bias, note = "short", "回测空头有优势，现价已到锚定入场区"+agoNote
			action = "回测可空 @" + formatPriceHint(shortEntry) + " WR" + strconv.FormatFloat(bt.Short.WinRate, 'f', 0, 64) + "% Exp" + strconv.FormatFloat(bt.Short.Expectancy, 'f', 2, 64) + "%"
		} else {
			bias, note = "short_wait", "回测空头有优势，等反抽到固定限价（不跟 last 漂移）"+agoNote
			action = "挂空限价 @" + formatPriceHint(shortEntry) + "（信号价 " + formatPriceHint(bt.LiveSignalClose) + "）"
		}
	case longOK && shortOK && side == 0:
		bias, note = "range", "多空两侧回测期望均为正，当前无未过期信号"
		action = "观望等信号（入场价仅在出信号后锚定，避免跟价漂移）"
	case longOK && side == 0:
		bias, note = "neutral", "回测多头有优势，但近 "+strconv.Itoa(trendMaxHold)+" 根内无有效信号"
		action = "关注多头优势，等新信号再给固定入场"
	case shortOK && side == 0:
		bias, note = "neutral", "回测空头有优势，但近 "+strconv.Itoa(trendMaxHold)+" 根内无有效信号"
		action = "关注空头优势，等新信号再给固定入场"
	case side > 0 && !longOK:
		bias, note = "neutral", "有多头信号但历史期望不足"+agoNote
		action = "观望（多头回测无优势）"
		longEntry, longStop, longTP = 0, 0, 0
	case side < 0 && !shortOK:
		bias, note = "neutral", "有空头信号但历史期望不足"+agoNote
		action = "观望（空头回测无优势）"
		shortEntry, shortStop, shortTP = 0, 0, 0
	default:
		bias, note = "neutral", "两侧回测期望均不足"
		action = "观望"
	}

	return bias, note, action, longEntry, shortEntry, longStop, shortStop, longTP, shortTP
}

// analysisTrend screens high quote-volume symbols and ranks by walk-forward backtest edge.
// Query: top (default 25), minQuoteVol (USDT, default 5e7), bars (default 1920 ≈ 20d of 15m), session
func (s *Server) analysisTrend(c *gin.Context) {
	session, ok := s.sessionOrAbort(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	topN, _ := strconv.Atoi(c.DefaultQuery("top", "25"))
	if topN < 5 {
		topN = 5
	}
	if topN > 50 {
		topN = 50
	}
	minQv, _ := strconv.ParseFloat(c.DefaultQuery("minQuoteVol", "50000000"), 64)
	bars, _ := strconv.Atoi(c.DefaultQuery("bars", "1920"))
	if bars < 300 {
		bars = 300
	}
	if bars > 5000 {
		bars = 5000
	}

	tickers, err := session.Exchange.QueryTickers(ctx)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query tickers: " + err.Error()})
		return
	}

	type volRow struct {
		Symbol string
		Last   float64
		Open   float64
		QV     float64
	}
	cands := make([]volRow, 0, 128)
	for sym, t := range tickers {
		if !isTrendScreenerSymbol(sym) {
			continue
		}
		last := t.Last.Float64()
		if last <= 0 {
			continue
		}
		qv := t.Volume.Float64() * last
		if qv < minQv {
			continue
		}
		cands = append(cands, volRow{Symbol: sym, Last: last, Open: t.Open.Float64(), QV: qv})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].QV > cands[j].QV })
	if len(cands) > topN {
		cands = cands[:topN]
	}

	out := make([]trendPickRow, len(cands))
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	for i, cand := range cands {
		i, cand := i, cand
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			klines, src, err := s.queryAnalysisKLines(gctx, session, cand.Symbol, types.Interval15m, bars)
			row := trendPickRow{
				Symbol:         cand.Symbol,
				Last:           roundFloat(cand.Last, 8),
				QuoteVolume24h: roundFloat(cand.QV, 0),
				VolumeRank:     i + 1,
				KlineSource:    src,
			}
			if cand.Open > 0 {
				row.Chg24hPct = roundFloat((cand.Last/cand.Open-1)*100, 2)
			}
			if err != nil || len(klines) < trendBTWarmup+trendMaxHold {
				row.Bias = "neutral"
				row.BiasNote = "insufficient klines"
				if err != nil {
					row.BiasNote = err.Error()
				}
				row.Action = "观望"
				row.Bars = len(klines)
				mu.Lock()
				out[i] = row
				mu.Unlock()
				return nil
			}
			bt := backtestTrendPullback(klines)
			last := cand.Last
			if n := len(klines); n > 0 {
				last = klines[n-1].Close.Float64()
			}
			bias, note, action, le, se, ls, ss, lt, st := classifyFromBacktest(last, bt)
			row.Last = roundFloat(last, 8)
			row.Bars = bt.Bars
			row.Bias = bias
			row.BiasNote = note
			row.Action = action
			if bt.LiveSide != 0 && !bt.LiveExpired {
				row.SignalClose = roundFloat(bt.LiveSignalClose, 8)
				row.SignalAgo = bt.LiveBarsAgo
			}
			row.LongEntry = le
			row.ShortEntry = se
			row.LongStop = ls
			row.ShortStop = ss
			row.LongTP = lt
			row.ShortTP = st
			row.LongBT = bt.Long
			row.ShortBT = bt.Short
			mu.Lock()
			out[i] = row
			mu.Unlock()
			return nil
		})
	}
	if waitErr := g.Wait(); waitErr != nil && ctx.Err() != nil {
		c.JSON(http.StatusRequestTimeout, gin.H{"error": "cancelled"})
		return
	}

	biasRank := map[string]int{
		"long": 0, "short": 1, "long_wait": 2, "short_wait": 3, "range": 4, "neutral": 5,
	}
	bestExp := func(r trendPickRow) float64 {
		a, b := r.LongBT.Expectancy, r.ShortBT.Expectancy
		if a > b {
			return a
		}
		return b
	}
	sort.SliceStable(out, func(i, j int) bool {
		bi, bj := biasRank[out[i].Bias], biasRank[out[j].Bias]
		if bi != bj {
			return bi < bj
		}
		ei, ej := bestExp(out[i]), bestExp(out[j])
		if ei != ej {
			return ei > ej
		}
		return out[i].VolumeRank < out[j].VolumeRank
	})

	longs, shorts, waits := 0, 0, 0
	for _, r := range out {
		switch r.Bias {
		case "long":
			longs++
		case "short":
			shorts++
		case "long_wait", "short_wait":
			waits++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"session":     session.Name,
		"asOf":        time.Now().UTC().Format(time.RFC3339),
		"top":         topN,
		"minQuoteVol": minQv,
		"interval":    "15m",
		"bars":        bars,
		"summary": gin.H{
			"scanned":      len(out),
			"longNow":      longs,
			"shortNow":     shorts,
			"waitPullback": waits,
		},
		"symbols": out,
		"rules": gin.H{
			"purpose": "按 24h 成交额筛活跃 USDT 合约，对每标的用本地 K 线做 walk-forward 回测；仅当该侧历史期望>0 且样本≥" + strconv.Itoa(trendMinTrades) + " 才给多空建议。入场=信号价按胜局中位回踩/反抽深度。",
			"volume":  "Futures ticker，*USDT；minQuoteVol 默认 5e7；取 top N",
			"strategy": []string{
				"周期: 15m × bars（默认 1920 ≈ 20 天）",
				"多: EMA20>EMA50 且 (RSI 自≤35 拐头 或 触及 EMA20)",
				"空: EMA20<EMA50 且 (RSI 自≥65 拐头 或 触及 EMA20)",
				"入场: 信号下一根开盘；止损 1.5×ATR14；止盈 2.5×ATR14；最长持有 48 根",
				"费用: 往返 8bps；同根止损+止盈同时触发按止损计",
			},
			"bias": []string{
				"long/short: 该侧回测有优势，且现价已触及锚定限价（相对信号价固定，不跟 last 跑）",
				"long_wait/short_wait: 有未过期信号，挂固定限价等回踩/反抽；到价才变 long/short",
				"range/neutral: 无未过期信号或期望不足——不再用 last 伪造入场价",
			},
			"entries": []string{
				"信号锚定：取近 "+strconv.Itoa(trendMaxHold)+" 根内最新同向信号簇的最早一根收盘价",
				"理想入场 = 信号收盘价 × (1 ± 胜局中位回踩深度%)，随后不再随 last 改写",
				"现价到入场±0.3%（或已触碰过入场区）→ long/short；否则 wait；超 "+strconv.Itoa(trendMaxHold)+" 根未到价则信号失效",
				"止损/止盈：1.5 / 2.5 × 信号时 ATR14",
			},
			"metrics": []string{
				"WR（Win Rate）: 该侧回测胜率 = 盈利笔数 / 总笔数 × 100%",
				"Exp（Expectancy）: 平均每笔净盈亏%（已扣往返 8bps）；>0 表示历史有正期望",
				"n: 回测样本笔数；本页要求 n≥" + strconv.Itoa(trendMinTrades) + " 且 Exp>0 且 PF≥1 才算有优势",
				"PF（Profit Factor）: 总盈利 / 总亏损绝对值；≥1 表示赚的盖过亏的",
			},
		},
	})
}
