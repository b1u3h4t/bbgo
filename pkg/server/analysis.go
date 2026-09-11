package server

import (
	"context"
	"math"
	"net/http"
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
	Symbol           string  `json:"symbol"`
	PositionAmt      float64 `json:"positionAmt"`
	EntryPrice       float64 `json:"entryPrice"`
	MarkPrice        float64 `json:"markPrice"`
	Notional         float64 `json:"notional"`
	UnrealizedPnL    float64 `json:"unrealizedPnL"`
	Leverage         float64 `json:"leverage"`
	InitialMarginEst float64 `json:"initialMarginEst"`
	Side             string  `json:"side"`
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

	positions := make([]marginPositionRow, 0)
	if risker, ok := session.Exchange.(positionRiskService); ok {
		if risks, err := risker.QueryPositionRisk(ctx); err == nil {
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
				positions = append(positions, marginPositionRow{
					Symbol:           r.Symbol,
					PositionAmt:      amt,
					EntryPrice:       r.EntryPrice.Float64(),
					MarkPrice:        mark,
					Notional:         roundFloat(notion, 2),
					UnrealizedPnL:    roundFloat(r.UnrealizedPnL.Float64(), 4),
					Leverage:         lev,
					InitialMarginEst: roundFloat(notion/lev, 2),
					Side:             side,
				})
			}
		}
	}

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
		klines, err := session.Exchange.QueryKLines(ctx, sym, types.Interval15m, types.KLineQueryOptions{Limit: 96})
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
		"symbols":  out,
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
		"leverage":            leverage,
		"quantity":            roundFloat(suggestedQty, 0),
		"designBuyPins":       designBuyPins,
		"maxInitialMarginEst": roundFloat(maxIM, 2),
		"buyInitialMarginEst": roundFloat(buyIM, 2),
		"targetUtil":          targetUtil,
		"note":                note,
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

	type agg struct {
		Realized, Commission, Funding float64
	}
	bySym := map[string]*agg{}
	byType := map[string]float64{}

	fetch := func(incomeType binanceapi.FuturesIncomeType) {
		rows, err := ex.QueryFuturesIncomeHistory(ctx, "", incomeType, &day0, &now)
		if err != nil {
			return
		}
		for _, r := range rows {
			inc := r.Income.Float64()
			byType[string(r.IncomeType)] += inc
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
			case binanceapi.FuturesIncomeCommission:
				bySym[sym].Commission += inc
			case binanceapi.FuturesIncomeFundingFee:
				bySym[sym].Funding += inc
			}
		}
	}
	fetch(binanceapi.FuturesIncomeRealizedPnL)
	fetch(binanceapi.FuturesIncomeCommission)
	fetch(binanceapi.FuturesIncomeFundingFee)

	symbols := make([]gin.H, 0, len(bySym))
	for sym, a := range bySym {
		symbols = append(symbols, gin.H{
			"symbol":     sym,
			"realized":   roundFloat(a.Realized, 4),
			"commission": roundFloat(a.Commission, 4),
			"funding":    roundFloat(a.Funding, 4),
			"net":        roundFloat(a.Realized+a.Commission+a.Funding, 4),
		})
	}

	realized := byType[string(binanceapi.FuturesIncomeRealizedPnL)]
	commission := byType[string(binanceapi.FuturesIncomeCommission)]
	funding := byType[string(binanceapi.FuturesIncomeFundingFee)]

	c.JSON(http.StatusOK, gin.H{
		"session": session.Name,
		"range": gin.H{
			"tz":    "CST",
			"start": day0.Format(time.RFC3339),
			"end":   now.Format(time.RFC3339),
		},
		"totals": gin.H{
			"realized":   roundFloat(realized, 4),
			"commission": roundFloat(commission, 4),
			"funding":    roundFloat(funding, 4),
			"net":        roundFloat(realized+commission+funding, 4),
		},
		"symbols": symbols,
	})
}
