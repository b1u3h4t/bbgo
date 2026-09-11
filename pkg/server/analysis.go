package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/exchange/binance"
	"github.com/c9s/bbgo/pkg/exchange/binance/binanceapi"
	"github.com/c9s/bbgo/pkg/strategy/grid2"
	"github.com/c9s/bbgo/pkg/types"
)

func (s *Server) registerAnalysisRoutes(r *gin.Engine) {
	r.GET("/api/analysis/margin", s.analysisMargin)
	r.GET("/api/analysis/market", s.analysisMarket)
	r.GET("/api/analysis/grid-calc", s.analysisGridCalc)
	r.GET("/api/analysis/pnl/today", s.analysisTodayPnL)
	r.GET("/api/analysis/klines", s.analysisKlines)
	r.GET("/api/analysis/avg-down", s.analysisAvgDown)
	s.startAnalysisKlineSync()
}

func (s *Server) sessionOrAbort(c *gin.Context) (*bbgo.ExchangeSession, bool) {
	if s.Environ == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "environment not ready"})
		return nil, false
	}
	name := c.DefaultQuery("session", "binance")
	session, ok := s.Environ.Session(name)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session not found: " + name})
		return nil, false
	}
	return session, true
}

type marginPositionRow struct {
	Symbol            string  `json:"symbol"`
	PositionAmt       float64 `json:"positionAmt"`
	EntryPrice        float64 `json:"entryPrice"`
	MarkPrice         float64 `json:"markPrice"`
	LiquidationPrice  float64 `json:"liquidationPrice"`
	BreakEvenPrice    float64 `json:"breakEvenPrice"`
	Notional          float64 `json:"notional"`
	UnrealizedPnL     float64 `json:"unrealizedPnL"`
	ROEPct            float64 `json:"roePct"`
	Leverage          float64 `json:"leverage"`
	InitialMarginEst  float64 `json:"initialMarginEst"`
	MaintMargin       float64 `json:"maintMargin"`
	Side              string  `json:"side"`
	MarginType        string  `json:"marginType"`
}

type marginOrderRow struct {
	Symbol       string  `json:"symbol"`
	Count        int     `json:"count"`
	BuyNotional  float64 `json:"buyNotional"`
	SellNotional float64 `json:"sellNotional"`
	BuyCount     int     `json:"buyCount"`
	SellCount    int     `json:"sellCount"`
}

type positionRiskService interface {
	QueryPositionRisk(ctx context.Context, symbol ...string) ([]types.PositionRisk, error)
}

func collectPositions(ctx context.Context, session *bbgo.ExchangeSession) []marginPositionRow {
	positions := make([]marginPositionRow, 0)
	risker, ok := session.Exchange.(positionRiskService)
	if !ok {
		return positions
	}
	risks, err := risker.QueryPositionRisk(ctx)
	if err != nil {
		return positions
	}
	for _, r := range risks {
		amt := r.PositionAmount.Float64()
		if math.Abs(amt) < 1e-12 {
			continue
		}
		mark := r.MarkPrice.Float64()
		notion := math.Abs(amt) * mark
		lev := r.Leverage.Float64()
		if lev <= 0 {
			lev = 1
		}
		side := "LONG"
		if amt < 0 {
			side = "SHORT"
		}
		im := r.InitialMargin.Float64()
		if im <= 0 {
			im = r.PositionInitialMargin.Float64()
		}
		if im <= 0 {
			im = notion / lev
		}
		upnlPos := r.UnrealizedPnL.Float64()
		roe := 0.0
		if im > 0 {
			roe = upnlPos / im * 100
		}
		positions = append(positions, marginPositionRow{
			Symbol:           r.Symbol,
			PositionAmt:      amt,
			EntryPrice:       r.EntryPrice.Float64(),
			MarkPrice:        mark,
			LiquidationPrice: r.LiquidationPrice.Float64(),
			BreakEvenPrice:   r.BreakEvenPrice.Float64(),
			Notional:         roundFloat(notion, 2),
			UnrealizedPnL:    roundFloat(upnlPos, 4),
			ROEPct:           roundFloat(roe, 2),
			Leverage:         lev,
			InitialMarginEst: roundFloat(im, 2),
			MaintMargin:      roundFloat(r.MaintMargin.Float64(), 2),
			Side:             side,
			MarginType:       "cross",
		})
	}
	sort.Slice(positions, func(i, j int) bool {
		return positions[i].UnrealizedPnL < positions[j].UnrealizedPnL
	})
	return positions
}

func (s *Server) analysisMargin(c *gin.Context) {
	session, ok := s.sessionOrAbort(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	account, err := session.UpdateAccount(ctx)
	if err != nil {
		account = session.GetAccount()
	}

	wallet, avail, upnl, marginBal := 0.0, 0.0, 0.0, 0.0
	posIM, orderIM, totalIM, maintIM := 0.0, 0.0, 0.0, 0.0

	if account != nil {
		if fi := account.FuturesInfo; fi != nil {
			wallet = fi.TotalWalletBalance.Float64()
			upnl = fi.TotalUnrealizedProfit.Float64()
			marginBal = fi.TotalMarginBalance.Float64()
			posIM = fi.TotalPositionInitialMargin.Float64()
			orderIM = fi.TotalOpenOrderInitialMargin.Float64()
			totalIM = fi.TotalInitialMargin.Float64()
			maintIM = fi.TotalMaintMargin.Float64()
			avail = fi.AvailableBalance.Float64()
		}
		if avail == 0 {
			if bal, ok := account.Balance("USDT"); ok {
				avail = bal.Available.Float64()
			}
		}
		if marginBal == 0 {
			marginBal = wallet + upnl
		}
	}

	positions := collectPositions(ctx, session)

	orderSyms := map[string]struct{}{}
	for _, p := range positions {
		orderSyms[p.Symbol] = struct{}{}
	}
	if s.Trader != nil {
		_ = s.Trader.IterateStrategies(func(st types.StrategyID) error {
			if g, ok := st.(*grid2.Strategy); ok && g.Symbol != "" {
				orderSyms[g.Symbol] = struct{}{}
			}
			return nil
		})
	}

	orders := make([]marginOrderRow, 0)
	for sym := range orderSyms {
		oo, err := session.Exchange.QueryOpenOrders(ctx, sym)
		if err != nil || len(oo) == 0 {
			continue
		}
		row := marginOrderRow{Symbol: sym, Count: len(oo)}
		for _, o := range oo {
			px := o.Price.Float64()
			qty := o.Quantity.Sub(o.ExecutedQuantity).Float64()
			if qty < 0 {
				qty = 0
			}
			n := px * qty
			if o.Side == types.SideTypeBuy {
				row.BuyNotional += n
				row.BuyCount++
			} else {
				row.SellNotional += n
				row.SellCount++
			}
		}
		row.BuyNotional = roundFloat(row.BuyNotional, 2)
		row.SellNotional = roundFloat(row.SellNotional, 2)
		orders = append(orders, row)
	}

	utilIM, utilUsed := 0.0, 0.0
	if wallet > 0 {
		utilIM = totalIM / wallet * 100
		utilUsed = (wallet - avail) / wallet * 100
	}

	c.JSON(http.StatusOK, gin.H{
		"session": session.Name,
		"account": gin.H{
			"walletBalance":           roundFloat(wallet, 2),
			"availableBalance":        roundFloat(avail, 2),
			"marginBalance":           roundFloat(marginBal, 2),
			"unrealizedPnL":           roundFloat(upnl, 2),
			"totalInitialMargin":      roundFloat(totalIM, 2),
			"positionInitialMargin":   roundFloat(posIM, 2),
			"openOrderInitialMargin":  roundFloat(orderIM, 2),
			"maintMargin":             roundFloat(maintIM, 2),
			"utilInitialMarginPct":    roundFloat(utilIM, 2),
			"utilWalletMinusAvailPct": roundFloat(utilUsed, 2),
			"targetUtilPct":           65.0,
		},
		"positions":  positions,
		"openOrders": orders,
	})
}

type marketSymbolAnalysis struct {
	Symbol      string    `json:"symbol"`
	Last        float64   `json:"last"`
	DayLow      float64   `json:"dayLow"`
	DayHigh     float64   `json:"dayHigh"`
	BouncePct   float64   `json:"bouncePct"`
	Chg2hPct    float64   `json:"chg2hPct"`
	Chg4hPct    float64   `json:"chg4hPct"`
	RSI15m      float64   `json:"rsi15m"`
	GreenBars2h int       `json:"greenBars2h"`
	HigherLows  bool      `json:"higherLows"`
	Score       int       `json:"score"`
	Verdict     string    `json:"verdict"`
	Notes       []string  `json:"notes"`
	ChunkLows   []float64 `json:"chunkLows"`
	AboveMid    bool      `json:"aboveMid"`
}

func calcRSI(closes []float64, n int) float64 {
	if len(closes) < n+1 {
		return 50
	}
	var gains, losses float64
	for i := len(closes) - n; i < len(closes); i++ {
		d := closes[i] - closes[i-1]
		if d >= 0 {
			gains += d
		} else {
			losses -= d
		}
	}
	ag, al := gains/float64(n), losses/float64(n)
	if al == 0 {
		return 100
	}
	return 100 - 100/(1+ag/al)
}

func analyzeSymbolKlines(klines []types.KLine) marketSymbolAnalysis {
	a := marketSymbolAnalysis{Notes: []string{}, ChunkLows: []float64{}}
	if len(klines) == 0 {
		a.Verdict = "no data"
		return a
	}
	a.Symbol = klines[0].Symbol
	n := len(klines)
	closes := make([]float64, n)
	for i, k := range klines {
		closes[i] = k.Close.Float64()
	}
	lastClose := klines[n-1].Close.Float64()
	a.Last = lastClose

	day := klines
	if n > 96 {
		day = klines[n-96:]
	}
	dayLow, dayHigh := day[0].Low.Float64(), day[0].High.Float64()
	for _, k := range day {
		if v := k.Low.Float64(); v < dayLow {
			dayLow = v
		}
		if v := k.High.Float64(); v > dayHigh {
			dayHigh = v
		}
	}
	a.DayLow, a.DayHigh = dayLow, dayHigh
	a.BouncePct = (lastClose/dayLow - 1) * 100

	i8 := n - 8
	if i8 < 0 {
		i8 = 0
	}
	i16 := n - 16
	if i16 < 0 {
		i16 = 0
	}
	a.Chg2hPct = (lastClose/klines[i8].Open.Float64() - 1) * 100
	a.Chg4hPct = (lastClose/klines[i16].Open.Float64() - 1) * 100
	a.RSI15m = calcRSI(closes, 14)

	green := 0
	for i := i8; i < n; i++ {
		if klines[i].Close.Compare(klines[i].Open) >= 0 {
			green++
		}
	}
	a.GreenBars2h = green

	start := n - 24
	if start < 0 {
		start = 0
	}
	for i := start; i+8 <= n; i += 8 {
		lo := klines[i].Low.Float64()
		for j := i; j < i+8; j++ {
			if v := klines[j].Low.Float64(); v < lo {
				lo = v
			}
		}
		a.ChunkLows = append(a.ChunkLows, roundFloat(lo, 8))
	}
	if len(a.ChunkLows) >= 3 {
		a.HigherLows = a.ChunkLows[len(a.ChunkLows)-1] > a.ChunkLows[len(a.ChunkLows)-2] &&
			a.ChunkLows[len(a.ChunkLows)-2] > a.ChunkLows[len(a.ChunkLows)-3]
	}

	mid := (dayHigh + dayLow) / 2
	a.AboveMid = lastClose > mid

	score := 0
	if lastClose > dayLow*1.005 {
		score++
		a.Notes = append(a.Notes, "off day low")
	} else {
		a.Notes = append(a.Notes, "near day low")
	}
	if a.Chg2hPct > 0.3 {
		score++
		a.Notes = append(a.Notes, "2h up")
	} else if a.Chg2hPct < -0.3 {
		score--
		a.Notes = append(a.Notes, "2h down")
	} else {
		a.Notes = append(a.Notes, "2h flat")
	}
	if a.HigherLows {
		score += 2
		a.Notes = append(a.Notes, "higher lows")
	} else if len(a.ChunkLows) >= 2 && a.ChunkLows[len(a.ChunkLows)-1] < a.ChunkLows[len(a.ChunkLows)-2] {
		score--
		a.Notes = append(a.Notes, "lower lows")
	}
	if a.RSI15m < 30 {
		score++
		a.Notes = append(a.Notes, "RSI oversold")
	} else if a.RSI15m > 45 && a.BouncePct > 1 {
		score++
		a.Notes = append(a.Notes, "RSI leaving oversold")
	}
	if green >= 5 {
		score++
		a.Notes = append(a.Notes, "mostly green 2h")
	} else if green <= 3 {
		score--
		a.Notes = append(a.Notes, "few green bars")
	}
	if a.AboveMid {
		score++
		a.Notes = append(a.Notes, "above day mid")
	} else {
		a.Notes = append(a.Notes, "below day mid")
	}

	a.Score = score
	switch {
	case score >= 4:
		a.Verdict = "止跌迹象偏强"
	case score >= 2:
		a.Verdict = "弱止跌/观望"
	default:
		a.Verdict = "尚未止跌"
	}
	a.Last = roundFloat(a.Last, 8)
	a.DayLow = roundFloat(a.DayLow, 8)
	a.DayHigh = roundFloat(a.DayHigh, 8)
	a.BouncePct = roundFloat(a.BouncePct, 2)
	a.Chg2hPct = roundFloat(a.Chg2hPct, 2)
	a.Chg4hPct = roundFloat(a.Chg4hPct, 2)
	a.RSI15m = roundFloat(a.RSI15m, 1)
	return a
}

func (s *Server) analysisMarket(c *gin.Context) {
	session, ok := s.sessionOrAbort(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	raw := c.DefaultQuery("symbols", "BTCUSDT,ETHUSDT,XRPUSDT,NEARUSDT,HYPEUSDT,AVAXUSDT,DOGEUSDT,WLDUSDT,ENAUSDT,SOLUSDT,BNBUSDT,SUIUSDT")
	out := make([]marketSymbolAnalysis, 0)
	for _, sym := range strings.Split(raw, ",") {
		sym = strings.TrimSpace(sym)
		if sym == "" {
			continue
		}
		klines, _, err := s.queryAnalysisKLines(ctx, session, sym, types.Interval15m, 96)
		if err != nil {
			out = append(out, marketSymbolAnalysis{Symbol: sym, Verdict: "error: " + err.Error(), Notes: []string{}})
			continue
		}
		a := analyzeSymbolKlines(klines)
		a.Symbol = sym
		out = append(out, a)
	}

	stage := "尚未止跌"
	if len(out) > 0 {
		switch {
		case out[0].Score >= 4:
			stage = "止跌迹象偏强"
		case out[0].Score >= 2:
			stage = "弱止跌/观望"
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"session":  session.Name,
		"interval": "15m",
		"asOf":     time.Now().UTC().Format(time.RFC3339),
		"stage":    stage,
		"stageNote": "顶部 BTC阶段 取列表首个 symbol（默认 BTCUSDT）的 score 映射",
		"symbols":  out,
		"rules": gin.H{
			"purpose": "启发式止跌打分（运维盘感落地），非回测策略；每次刷新实时拉 K 重算",
			"data": []string{
				"K线: 15m × 最多96根（约滚动24h），非自然日",
				"日高低: 上述窗口内 high/low",
				"Bounce% = (last/dayLow - 1)×100",
				"2h%/4h%: 相对最近8/16根开盘涨跌（约2h/4h）；4h%仅展示不进分",
				"RSI: 近14根收盘简易平均涨跌版（非Wilder平滑，与TV略有偏差）",
				"绿柱: 近8根 close≥open 根数",
				"Higher lows: 近24根按每8根切3段取低点，要求 L3>L2>L1（+2主信号）",
				"Above mid: last > (dayHigh+dayLow)/2",
			},
			"scoring": []gin.H{
				{"when": "last > dayLow×1.005", "delta": "+1", "note": "off day low"},
				{"when": "否则贴地", "delta": "0", "note": "near day low"},
				{"when": "2h% > +0.3%", "delta": "+1", "note": "2h up"},
				{"when": "2h% < -0.3%", "delta": "-1", "note": "2h down"},
				{"when": "|2h%|≤0.3%", "delta": "0", "note": "2h flat"},
				{"when": "连续3段抬高低点", "delta": "+2", "note": "higher lows"},
				{"when": "最近两段低点下降", "delta": "-1", "note": "lower lows"},
				{"when": "RSI < 30", "delta": "+1", "note": "RSI oversold"},
				{"when": "RSI > 45 且 Bounce% > 1%", "delta": "+1", "note": "RSI leaving oversold"},
				{"when": "2h绿柱 ≥ 5", "delta": "+1", "note": "mostly green 2h"},
				{"when": "2h绿柱 ≤ 3", "delta": "-1", "note": "few green bars"},
				{"when": "站上日中轴", "delta": "+1", "note": "above day mid"},
				{"when": "否则在中轴下", "delta": "0", "note": "below day mid"},
			},
			"verdict": []gin.H{
				{"minScore": 4, "label": "止跌迹象偏强"},
				{"minScore": 2, "label": "弱止跌/观望"},
				{"minScore": nil, "label": "尚未止跌（score≤1）"},
			},
			"validation": "未做历史回测；对照 Notes/Score/特征列人工核验即可",
		},
	})
}

func (s *Server) analysisGridCalc(c *gin.Context) {
	session, ok := s.sessionOrAbort(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	symbol := strings.TrimSpace(c.DefaultQuery("symbol", "AVAXUSDT"))
	atrMult, _ := strconv.ParseFloat(c.DefaultQuery("atrMult", "1"), 64)
	gridNumber, _ := strconv.Atoi(c.DefaultQuery("gridNumber", "8"))
	if gridNumber < 2 {
		gridNumber = 2
	}
	qty, _ := strconv.ParseFloat(c.DefaultQuery("quantity", "0"), 64)
	leverage, _ := strconv.ParseFloat(c.DefaultQuery("leverage", "3"), 64)
	if leverage <= 0 {
		leverage = 3
	}
	targetUtil, _ := strconv.ParseFloat(c.DefaultQuery("targetUtil", "0.65"), 64)
	if targetUtil > 1 {
		targetUtil /= 100
	}

	ticker, err := session.Exchange.QueryTicker(ctx, symbol)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	last := ticker.Last.Float64()
	if last <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid last price"})
		return
	}

	klines, err := session.Exchange.QueryKLines(ctx, symbol, types.Interval1d, types.KLineQueryOptions{Limit: 15})
	if err != nil || len(klines) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to query daily klines"})
		return
	}
	var trSum float64
	for i := 1; i < len(klines); i++ {
		h, l, pc := klines[i].High.Float64(), klines[i].Low.Float64(), klines[i-1].Close.Float64()
		trSum += math.Max(h-l, math.Max(math.Abs(h-pc), math.Abs(l-pc)))
	}
	atrPct := (trSum / float64(len(klines)-1)) / last

	tick := 0.001
	if market, mok := session.Market(symbol); mok && market.TickSize.Sign() > 0 {
		tick = market.TickSize.Float64()
	}

	w := atrMult * atrPct
	k := math.Sqrt(1 + w)
	lo := math.Floor((last/k)/tick) * tick
	hi := math.Ceil((last*k)/tick) * tick
	widthPct := (hi/lo - 1) * 100
	stepPct := widthPct / float64(gridNumber-1)
	mid := (lo + hi) / 2
	tp := math.Ceil((hi*1.05)/tick) * tick

	pins := make([]float64, gridNumber)
	for i := 0; i < gridNumber; i++ {
		pins[i] = roundFloat(lo+float64(i)*(hi-lo)/float64(gridNumber-1), 8)
	}

	designBuyPins := (gridNumber - 1) / 2
	if designBuyPins < 1 {
		designBuyPins = 1
	}

	suggestedQty := qty
	note := "quantity provided by query"
	if qty <= 0 {
		note = "quantity auto-sized toward target util (approx)"
		wallet, totalIM := 0.0, 0.0
		if account, err := session.UpdateAccount(ctx); err == nil && account != nil {
			if fi := account.FuturesInfo; fi != nil {
				wallet = fi.TotalWalletBalance.Float64()
				totalIM = fi.TotalInitialMargin.Float64()
			}
		}
		targetIM := wallet * targetUtil
		need := math.Max(targetIM-totalIM, wallet*0.02)
		suggestedQty = math.Round(need * leverage / (mid * float64(designBuyPins)))
		if suggestedQty < 1 {
			suggestedQty = 1
		}
	}

	maxIM := suggestedQty * mid * float64(gridNumber-1) / leverage
	buyIM := suggestedQty * mid * float64(designBuyPins) / leverage

	bandPos := "IN"
	if last > hi {
		bandPos = "ABOVE"
	} else if last < lo {
		bandPos = "BELOW"
	}
	fromLow := 0.0
	if hi != lo {
		fromLow = (last - lo) / (hi - lo) * 100
	}

	chartInterval, chartLimit := parseChartInterval(c.DefaultQuery("interval", "1h"), c.DefaultQuery("limit", ""))
	klinesPayload := serializeKlines(nil)
	klineSource := "exchange"
	if klChart, src, err := s.queryAnalysisKLines(ctx, session, symbol, chartInterval, chartLimit); err == nil {
		klinesPayload = serializeKlines(klChart)
		klineSource = src
	}

	allPos := collectPositions(ctx, session)
	var symbolPos *marginPositionRow
	for i := range allPos {
		if allPos[i].Symbol == symbol {
			p := allPos[i]
			symbolPos = &p
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"symbol":      symbol,
		"last":        roundFloat(last, 8),
		"dailyAtrPct": roundFloat(atrPct*100, 2),
		"atrMult":     atrMult,
		"band": gin.H{
			"lower":      roundFloat(lo, 8),
			"upper":      roundFloat(hi, 8),
			"widthPct":   roundFloat(widthPct, 2),
			"stepPct":    roundFloat(stepPct, 3),
			"takeProfit": roundFloat(tp, 8),
			"mid":        roundFloat(mid, 8),
			"position":   bandPos,
			"fromLowPct": roundFloat(fromLow, 1),
			"inBand":     last >= lo && last <= hi,
		},
		"gridNumber":          gridNumber,
		"pins":                pins,
		"pinLevels":           buildPinLevels(pins, last, suggestedQty),
		"leverage":            leverage,
		"quantity":            roundFloat(suggestedQty, 0),
		"designBuyPins":       designBuyPins,
		"maxInitialMarginEst": roundFloat(maxIM, 2),
		"buyInitialMarginEst": roundFloat(buyIM, 2),
		"targetUtil":          targetUtil,
		"note":                note,
		"interval":            string(chartInterval),
		"klineSource":         klineSource,
		"klines":              klinesPayload,
		"klines1h":            klinesPayload, // backward-compatible alias
		"positions":           allPos,
		"position":            symbolPos,
	})
}

func serializeKlines(klines []types.KLine) []gin.H {
	out := make([]gin.H, 0, len(klines))
	for _, k := range klines {
		out = append(out, gin.H{
			"t": k.StartTime.Time().UTC().Format(time.RFC3339),
			"o": roundFloat(k.Open.Float64(), 8),
			"h": roundFloat(k.High.Float64(), 8),
			"l": roundFloat(k.Low.Float64(), 8),
			"c": roundFloat(k.Close.Float64(), 8),
			"v": roundFloat(k.Volume.Float64(), 8),
		})
	}
	return out
}

// allowed chart intervals for Analysis overlays (incl. Binance 8h).
var analysisChartIntervals = map[string]int{
	"5m": 96, "15m": 96, "30m": 96,
	"1h": 72, "2h": 72, "4h": 60,
	"6h": 48, "8h": 45, "12h": 42,
	"1d": 90,
}

func parseChartInterval(raw, limitRaw string) (types.Interval, int) {
	iv := strings.ToLower(strings.TrimSpace(raw))
	if iv == "d" || iv == "day" || iv == "daily" || iv == "日线" {
		iv = "1d"
	}
	defLimit, ok := analysisChartIntervals[iv]
	if !ok {
		iv = "1h"
		defLimit = analysisChartIntervals["1h"]
	}
	limit := defLimit
	if limitRaw != "" {
		if n, err := strconv.Atoi(limitRaw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 500 {
		limit = 500
	}
	return types.Interval(iv), limit
}

// buildPinLevels labels pins like an order book with quantity@price:
//   buys (price <= last): B1 > B2 > … (B1 = highest buy / closest to last)
//   sells (price > last): S1 < S2 < … (S1 = lowest sell / closest to last)
func buildPinLevels(pins []float64, last, qty float64) []gin.H {
	type lv struct {
		price float64
		side  string
	}
	var buys, sells []lv
	for _, p := range pins {
		if !(p > 0) {
			continue
		}
		if p > last {
			sells = append(sells, lv{price: p, side: "sell"})
		} else {
			buys = append(buys, lv{price: p, side: "buy"})
		}
	}
	sort.Slice(buys, func(i, j int) bool { return buys[i].price > buys[j].price })
	sort.Slice(sells, func(i, j int) bool { return sells[i].price < sells[j].price })

	qtyLabel := formatQtyLabel(qty)
	out := make([]gin.H, 0, len(buys)+len(sells))
	for i, b := range buys {
		label := fmt.Sprintf("%s@%s", qtyLabel, trimFloat(b.price))
		out = append(out, gin.H{
			"i": i + 1, "price": b.price, "side": "buy",
			"quantity": qty, "label": label, "depth": fmt.Sprintf("B%d", i+1),
		})
	}
	for i, s := range sells {
		label := fmt.Sprintf("%s@%s", qtyLabel, trimFloat(s.price))
		out = append(out, gin.H{
			"i": i + 1, "price": s.price, "side": "sell",
			"quantity": qty, "label": label, "depth": fmt.Sprintf("S%d", i+1),
		})
	}
	return out
}

func formatQtyLabel(qty float64) string {
	if qty <= 0 {
		return "?"
	}
	if qty == math.Trunc(qty) {
		return strconv.FormatInt(int64(qty), 10)
	}
	return strconv.FormatFloat(qty, 'f', -1, 64)
}

func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func (s *Server) analysisKlines(c *gin.Context) {
	session, ok := s.sessionOrAbort(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	symbol := strings.TrimSpace(c.DefaultQuery("symbol", "BTCUSDT"))
	interval, limit := parseChartInterval(c.DefaultQuery("interval", "1h"), c.DefaultQuery("limit", ""))

	klines, klineSource, err := s.queryAnalysisKLines(ctx, session, symbol, interval, limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	last := 0.0
	if len(klines) > 0 {
		last = klines[len(klines)-1].Close.Float64()
	}
	if t, err := session.Exchange.QueryTicker(ctx, symbol); err == nil && t != nil && t.Last.Float64() > 0 {
		last = t.Last.Float64()
	}

	// optional pins from active grid2 strategy
	pins := []float64{}
	lower, upper, qty := 0.0, 0.0, 0.0
	if s.Trader != nil {
		_ = s.Trader.IterateStrategies(func(st types.StrategyID) error {
			if g, ok := st.(*grid2.Strategy); ok && g.Symbol == symbol {
				lower = g.LowerPrice.Float64()
				upper = g.UpperPrice.Float64()
				qty = g.QuantityOrAmount.Quantity.Float64()
				if g.GridNum > 1 && upper > lower {
					n := int(g.GridNum)
					for i := 0; i < n; i++ {
						pins = append(pins, roundFloat(lower+float64(i)*(upper-lower)/float64(n-1), 8))
					}
				}
			}
			return nil
		})
	}

	var symbolPos *marginPositionRow
	for _, p := range collectPositions(ctx, session) {
		if p.Symbol == symbol {
			pp := p
			symbolPos = &pp
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"symbol":      symbol,
		"interval":    string(interval),
		"last":        roundFloat(last, 8),
		"quantity":    roundFloat(qty, 8),
		"klineSource": klineSource,
		"klines":      serializeKlines(klines),
		"pins":        pins,
		"pinLevels":   buildPinLevels(pins, last, qty),
		"band": gin.H{
			"lower": lower,
			"upper": upper,
		},
		"position":  symbolPos,
		"intervals": []string{"5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d"},
	})
}

func (s *Server) analysisTodayPnL(c *gin.Context) {
	session, ok := s.sessionOrAbort(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	ex, ok := session.Exchange.(*binance.Exchange)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "today PnL requires binance futures session"})
		return
	}

	loc := time.FixedZone("CST", 8*3600)
	now := time.Now().In(loc)
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	// BNB fee discount: COMMISSION income.asset is often BNB — convert to USDT.
	bnbPrice := 0.0
	if t, err := session.Exchange.QueryTicker(ctx, "BNBUSDT"); err == nil && t != nil {
		bnbPrice = t.Last.Float64()
	}

	type agg struct {
		Realized, CommissionUSDT, CommissionBNB, Funding float64
	}
	bySym := map[string]*agg{}
	var totRealized, totCommUSDT, totCommBNB, totFunding float64

	toUSDT := func(income float64, asset string) (usdt float64, bnb float64) {
		asset = strings.ToUpper(strings.TrimSpace(asset))
		switch asset {
		case "BNB":
			return income * bnbPrice, income
		case "USDT", "USD", "BUSD", "":
			return income, 0
		default:
			if asset != "" {
				if t, err := session.Exchange.QueryTicker(ctx, asset+"USDT"); err == nil && t != nil {
					return income * t.Last.Float64(), 0
				}
			}
			return income, 0
		}
	}

	incomeCounts := map[string]int{}
	fetch := func(incomeType binanceapi.FuturesIncomeType) error {
		rows, err := ex.QueryFuturesIncomeHistory(ctx, "", incomeType, &day0, &now)
		if err != nil {
			return fmt.Errorf("income %s: %w", incomeType, err)
		}
		incomeCounts[string(incomeType)] = len(rows)
		for _, r := range rows {
			inc := r.Income.Float64()
			sym := r.Symbol
			if sym == "" {
				sym = "(account)"
			}
			if bySym[sym] == nil {
				bySym[sym] = &agg{}
			}
			switch r.IncomeType {
			case binanceapi.FuturesIncomeRealizedPnL:
				bySym[sym].Realized += inc
				totRealized += inc
			case binanceapi.FuturesIncomeCommission:
				usdt, bnb := toUSDT(inc, r.Asset)
				bySym[sym].CommissionUSDT += usdt
				bySym[sym].CommissionBNB += bnb
				totCommUSDT += usdt
				totCommBNB += bnb
			case binanceapi.FuturesIncomeFundingFee:
				bySym[sym].Funding += inc
				totFunding += inc
			}
		}
		return nil
	}
	for _, t := range []binanceapi.FuturesIncomeType{
		binanceapi.FuturesIncomeRealizedPnL,
		binanceapi.FuturesIncomeCommission,
		binanceapi.FuturesIncomeFundingFee,
	} {
		if err := fetch(t); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
	}

	symbols := make([]gin.H, 0, len(bySym))
	for sym, a := range bySym {
		symbols = append(symbols, gin.H{
			"symbol":          sym,
			"realized":        roundFloat(a.Realized, 4),
			"commission":      roundFloat(a.CommissionUSDT, 4), // USDT-equivalent (BNB converted)
			"commissionBNB":   roundFloat(a.CommissionBNB, 8),
			"commissionAsset": "BNB->USDT",
			"funding":         roundFloat(a.Funding, 4),
			"net":             roundFloat(a.Realized+a.CommissionUSDT+a.Funding, 4),
		})
	}
	sort.Slice(symbols, func(i, j int) bool {
		ni, _ := symbols[i]["net"].(float64)
		nj, _ := symbols[j]["net"].(float64)
		return ni > nj
	})

	positions := collectPositions(ctx, session)
	uPnLSum := 0.0
	for _, p := range positions {
		uPnLSum += p.UnrealizedPnL
	}

	c.JSON(http.StatusOK, gin.H{
		"session": session.Name,
		"range": gin.H{
			"tz":    "CST",
			"start": day0.Format(time.RFC3339),
			"end":   now.Format(time.RFC3339),
		},
		"feeNote": gin.H{
			"bnbPriceUSDT": roundFloat(bnbPrice, 4),
			"detail":       "COMMISSION paid in BNB is converted to USDT via BNBUSDT last price. Net aligns with Binance /fapi/v1/income (REALIZED_PNL+COMMISSION+FUNDING_FEE) for CST today. Unrealized is current open-position float (not today's delta).",
		},
		"incomeCounts": incomeCounts,
		"totals": gin.H{
			"realized":      roundFloat(totRealized, 4),
			"commission":    roundFloat(totCommUSDT, 4),
			"commissionBNB": roundFloat(totCommBNB, 8),
			"funding":       roundFloat(totFunding, 4),
			"net":           roundFloat(totRealized+totCommUSDT+totFunding, 4),
			"unrealized":    roundFloat(uPnLSum, 4),
		},
		"positions": positions,
		"symbols":   symbols,
	})
}

// analysisAvgDown: careful average-down calculator for underwater longs (stranded grids).
// Query: session, symbol?, addIm?, addNotional?, addQty?, price? (limit), leverage?,
//        targetUtil? (default 0.65), reserveAvail? (default 3000), maxUtil? (default 0.70)
func (s *Server) analysisAvgDown(c *gin.Context) {
	session, ok := s.sessionOrAbort(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	account, err := session.UpdateAccount(ctx)
	if err != nil {
		account = session.GetAccount()
	}
	wallet, avail, totalIM, upnl := 0.0, 0.0, 0.0, 0.0
	if account != nil {
		if fi := account.FuturesInfo; fi != nil {
			wallet = fi.TotalWalletBalance.Float64()
			avail = fi.AvailableBalance.Float64()
			totalIM = fi.TotalInitialMargin.Float64()
			upnl = fi.TotalUnrealizedProfit.Float64()
		}
	}

	targetUtil, _ := strconv.ParseFloat(c.DefaultQuery("targetUtil", "0.65"), 64)
	if targetUtil > 1 {
		targetUtil /= 100
	}
	maxUtil, _ := strconv.ParseFloat(c.DefaultQuery("maxUtil", "0.70"), 64)
	if maxUtil > 1 {
		maxUtil /= 100
	}
	reserveAvail, _ := strconv.ParseFloat(c.DefaultQuery("reserveAvail", "3000"), 64)
	leverage, _ := strconv.ParseFloat(c.DefaultQuery("leverage", "3"), 64)
	if leverage <= 0 {
		leverage = 3
	}

	headTarget := wallet*targetUtil - totalIM
	headMax := wallet*maxUtil - totalIM
	safeByAvail := avail - reserveAvail
	safeBudget := math.Min(math.Max(0, headTarget), math.Max(0, safeByAvail))
	safeBudget = math.Min(safeBudget, math.Max(0, headMax))

	// disabled grid symbols from running strategies (Enable: false)
	disabled := map[string]bool{}
	if s.Trader != nil {
		_ = s.Trader.IterateStrategies(func(st types.StrategyID) error {
			if g, ok := st.(*grid2.Strategy); ok {
				if g.Enable != nil && !*g.Enable {
					disabled[g.Symbol] = true
				}
			}
			return nil
		})
	}

	var risks []types.PositionRisk
	if svc, ok := session.Exchange.(positionRiskService); ok {
		risks, _ = svc.QueryPositionRisk(ctx)
	}

	candidates := []gin.H{}
	for _, r := range risks {
		amt := r.PositionAmount.Float64()
		if amt <= 0 {
			continue // only longs for avg-down buy
		}
		entry := r.EntryPrice.Float64()
		mark := r.MarkPrice.Float64()
		if mark <= 0 {
			continue
		}
		up := r.UnrealizedPnL.Float64()
		notional := amt * mark
		im := notional / leverage
		distPct := (mark/entry - 1) * 100
		candidates = append(candidates, gin.H{
			"symbol":           r.Symbol,
			"positionAmt":      roundFloat(amt, 8),
			"entryPrice":       roundFloat(entry, 8),
			"markPrice":        roundFloat(mark, 8),
			"unrealizedPnL":    roundFloat(up, 4),
			"notional":         roundFloat(notional, 2),
			"initialMarginEst": roundFloat(im, 2),
			"distFromEntryPct": roundFloat(distPct, 2),
			"gridDisabledHint": disabled[r.Symbol],
			"underwater":       up < 0,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i]["unrealizedPnL"].(float64) < candidates[j]["unrealizedPnL"].(float64)
	})

	resp := gin.H{
		"session": session.Name,
		"warning": "补仓不会立刻减少浮亏；继续下跌会放大亏损。仅建议小额限价，保留网格运行保证金。",
		"account": gin.H{
			"walletBalance":   roundFloat(wallet, 2),
			"availableBalance": roundFloat(avail, 2),
			"totalInitialMargin": roundFloat(totalIM, 2),
			"unrealizedPnL":   roundFloat(upnl, 2),
			"utilInitialMarginPct": roundFloat(totalIM/math.Max(wallet, 1e-9)*100, 2),
			"targetUtilPct":   roundFloat(targetUtil*100, 1),
			"maxUtilPct":      roundFloat(maxUtil*100, 1),
			"reserveAvail":    roundFloat(reserveAvail, 2),
			"headroomTargetIM": roundFloat(headTarget, 2),
			"headroomMaxIM":   roundFloat(headMax, 2),
			"safeBudgetIM":    roundFloat(safeBudget, 2),
			"safeBudgetNotional": roundFloat(safeBudget*leverage, 2),
		},
		"leverage":    leverage,
		"candidates":  candidates,
		"presets": []gin.H{
			{"id": "conservative", "label": "保守合计IM600", "totalAddIm": 600},
			{"id": "target65", "label": "用满至65%头寸", "totalAddIm": math.Max(0, roundFloat(headTarget, 0))},
		},
	}

	symbol := strings.TrimSpace(c.Query("symbol"))
	if symbol != "" {
		var cur gin.H
		for _, cand := range candidates {
			if cand["symbol"] == symbol {
				cur = cand
				break
			}
		}
		if cur == nil {
			// still allow calc from live ticker if flat? skip
			c.JSON(http.StatusOK, resp)
			return
		}
		amt := cur["positionAmt"].(float64)
		entry := cur["entryPrice"].(float64)
		mark := cur["markPrice"].(float64)

		price := mark
		if v, err := strconv.ParseFloat(c.Query("price"), 64); err == nil && v > 0 {
			price = v
		}
		addIm, _ := strconv.ParseFloat(c.DefaultQuery("addIm", "0"), 64)
		addNotional, _ := strconv.ParseFloat(c.DefaultQuery("addNotional", "0"), 64)
		addQty, _ := strconv.ParseFloat(c.DefaultQuery("addQty", "0"), 64)
		if addQty <= 0 && addNotional > 0 {
			addQty = addNotional / price
			addIm = addNotional / leverage
		} else if addQty <= 0 && addIm > 0 {
			addNotional = addIm * leverage
			addQty = addNotional / price
		} else if addQty > 0 {
			addNotional = addQty * price
			addIm = addNotional / leverage
		}

		newAmt := amt + addQty
		newEntry := entry
		if newAmt > 0 {
			newEntry = (amt*entry + addQty*price) / newAmt
		}
		newUpnl := (mark - newEntry) * newAmt
		scenario := func(px float64) gin.H {
			return gin.H{
				"price": roundFloat(px, 8),
				"uPnL":  roundFloat((px-newEntry)*newAmt, 2),
			}
		}
		overBudget := addIm > safeBudget+1e-6
		resp["scenario"] = gin.H{
			"symbol":       symbol,
			"limitPrice":   roundFloat(price, 8),
			"addQty":       roundFloat(addQty, 6),
			"addNotional":  roundFloat(addNotional, 2),
			"addIm":        roundFloat(addIm, 2),
			"oldEntry":     roundFloat(entry, 8),
			"newEntry":     roundFloat(newEntry, 8),
			"entryImprovePct": roundFloat((newEntry/entry-1)*100, 2),
			"oldAmt":       roundFloat(amt, 6),
			"newAmt":       roundFloat(newAmt, 6),
			"uPnLNowAtMark": roundFloat(newUpnl, 2),
			"ifDrop5Pct":   scenario(mark * 0.95),
			"ifDrop10Pct":  scenario(mark * 0.90),
			"ifBackToOldEntry": scenario(entry),
			"overSafeBudget": overBudget,
			"note":         "限价单成交后均价才会变化；未成交仅占用委托保证金。",
		}
	}

	c.JSON(http.StatusOK, resp)
}
