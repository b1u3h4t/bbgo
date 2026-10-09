package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/strategy/prospec"
	"github.com/c9s/bbgo/pkg/types"
)

type prospecTFView struct {
	Interval    string              `json:"interval"`
	Nest        prospec.NestState   `json:"nest"`
	OneTwoThree prospec.OneTwoThree `json:"oneTwoThree"`
	TwoB        *prospec.TwoBSignal `json:"twoB,omitempty"`
	Setup       prospec.Setup       `json:"setup"`
	Last        float64             `json:"last"`
	Bars        int                 `json:"bars"`
}

type prospecScanRow struct {
	Symbol    string  `json:"symbol"`
	NestBias  string  `json:"nestBias"`
	Stage123  int     `json:"stage123"`
	Dir123    string  `json:"dir123"`
	Confirmed bool    `json:"confirmed"`
	TwoBSide  string  `json:"twoBSide,omitempty"`
	Setup     string  `json:"setup"`
	Side      string  `json:"side"`
	Aligned   bool    `json:"aligned"`
	Label     string  `json:"label"`
	Last      float64 `json:"last"`
}

type analysisProspecResp struct {
	Symbol      string           `json:"symbol"`
	AsOf        time.Time        `json:"asOf"`
	Timezone    string           `json:"timezone"`
	Weekly      prospecTFView    `json:"weekly"`
	Daily       prospecTFView    `json:"daily"`
	H4          prospecTFView    `json:"h4"`
	M15         prospecTFView    `json:"m15"`
	Primary     prospec.Setup    `json:"primary"`
	Clocks      []regimeClock    `json:"clocks"`
	Scan        []prospecScanRow `json:"scan,omitempty"`
	Backtest4h  prospec.BTStats  `json:"backtest4h"`
	Backtest1d  prospec.BTStats  `json:"backtest1d"`
	Rules       []string         `json:"rules"`
	SymbolsUsed []string         `json:"symbolsUsed,omitempty"`
	KlineSource string           `json:"klineSource"`
	TookMs      int64            `json:"tookMs"`
}

func buildProspecTF(iv types.Interval, ks []types.KLine) prospecTFView {
	v := prospecTFView{Interval: string(iv), Bars: len(ks)}
	if len(ks) == 0 {
		return v
	}
	v.Last = ks[len(ks)-1].Close.Float64()
	v.Nest = prospec.ClassifyNest(ks, string(iv))
	v.OneTwoThree = prospec.DetectOneTwoThree(ks, 3)
	v.TwoB = prospec.DetectTwoB(ks, 3, 16)
	// setup under self-nest (same TF structure as local bias)
	v.Setup = prospec.BuildSetup(v.Nest, v.OneTwoThree, v.TwoB, v.Last)
	return v
}

func pickPrimarySetup(weekly, daily, h4, m15 prospecTFView) prospec.Setup {
	// Nest from weekly (Sperandeo: weekly charts for major trend)
	nest := weekly.Nest
	if nest.Bias == "chop" {
		nest = daily.Nest
	}
	// Prefer daily 1-2-3 for swing; 15m 2B for intraday trigger under nest
	if daily.OneTwoThree.Confirmed {
		return prospec.BuildSetup(nest, daily.OneTwoThree, nil, daily.Last)
	}
	if h4.OneTwoThree.Confirmed {
		return prospec.BuildSetup(nest, h4.OneTwoThree, nil, h4.Last)
	}
	if m15.TwoB != nil && m15.TwoB.BarsAgo <= 3 {
		return prospec.BuildSetup(nest, prospec.OneTwoThree{Direction: "none"}, m15.TwoB, m15.Last)
	}
	if daily.OneTwoThree.Stage >= 1 {
		s := daily.Setup
		s.Kind = "wait"
		s.Label = "日线 1-2-3 进行中"
		s.Action = daily.OneTwoThree.Note
		return s
	}
	s := prospec.Setup{Kind: "wait", Side: "flat", Label: "无主线", Action: "周/日未确认变盘；15m 无同向 2B。空仓等结构"}
	if nest.Bias == "bull" {
		s.Action = "周/日嵌套偏多：只做多侧 1-2-3/2B，不逆势空"
	} else if nest.Bias == "bear" {
		s.Action = "周/日嵌套偏空：只做空侧 1-2-3/2B，不逆势多"
	}
	return s
}

func (s *Server) analysisProspec(c *gin.Context) {
	session, ok := s.sessionOrAbort(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	started := time.Now()
	symbol := strings.ToUpper(strings.TrimSpace(c.DefaultQuery("symbol", "BTCUSDT")))
	if symbol == "" {
		symbol = "BTCUSDT"
	}
	doScan := c.DefaultQuery("scan", "1") != "0"

	now := time.Now().UTC()
	type tfReq struct {
		iv    types.Interval
		limit int
		key   string
	}
	reqs := []tfReq{
		{types.Interval15m, 200, "15m"},
		{types.Interval4h, 200, "4h"},
		{types.Interval1d, 200, "1d"},
		{types.Interval1w, 120, "1w"},
	}
	klineMap := map[string][]types.KLine{}
	srcCount := map[string]int{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, r := range reqs {
		wg.Add(1)
		go func(r tfReq) {
			defer wg.Done()
			ks, src, err := s.queryAnalysisKLines(ctx, session, symbol, r.iv, r.limit)
			mu.Lock()
			defer mu.Unlock()
			klineMap[r.key] = ks
			if err != nil {
				log.WithError(err).Warnf("prospec: klines %s %s", symbol, r.iv)
			} else {
				srcCount[src]++
			}
		}(r)
	}
	wg.Wait()

	weekly := buildProspecTF(types.Interval1w, klineMap["1w"])
	daily := buildProspecTF(types.Interval1d, klineMap["1d"])
	h4 := buildProspecTF(types.Interval4h, klineMap["4h"])
	m15 := buildProspecTF(types.Interval15m, klineMap["15m"])
	// rebuild m15/h4 setups under weekly nest for display consistency
	m15.Setup = prospec.BuildSetup(weekly.Nest, m15.OneTwoThree, m15.TwoB, m15.Last)
	if weekly.Nest.Bias == "chop" {
		m15.Setup = prospec.BuildSetup(daily.Nest, m15.OneTwoThree, m15.TwoB, m15.Last)
	}
	h4.Setup = prospec.BuildSetup(weekly.Nest, h4.OneTwoThree, h4.TwoB, h4.Last)

	primary := pickPrimarySetup(weekly, daily, h4, m15)

	clocks := []regimeClock{
		makeClock(now, types.Interval15m, "执行换线：下一根 15m 收盘"),
		makeClock(now, types.Interval4h, "结构换线：下一根 4h 收盘"),
		makeClock(now, types.Interval1d, "日线换线"),
		makeClock(now, types.Interval1w, "周线换线（大趋势）"),
	}

	var scan []prospecScanRow
	syms := analysisDefaultSymbols()
	if doScan {
		scan = s.prospecScan(ctx, session, syms)
	}
	bt4h := s.prospecBacktestPool(ctx, session, syms, types.Interval4h, 300, 24)
	bt1d := s.prospecBacktestPool(ctx, session, syms, types.Interval1d, 260, 12)

	srcParts := make([]string, 0, len(srcCount))
	for k := range srcCount {
		srcParts = append(srcParts, k)
	}

	c.JSON(http.StatusOK, analysisProspecResp{
		Symbol:     symbol,
		AsOf:       now,
		Timezone:   "Asia/Shanghai",
		Weekly:     weekly,
		Daily:      daily,
		H4:         h4,
		M15:        m15,
		Primary:    primary,
		Clocks:     clocks,
		Scan:       scan,
		Backtest4h: bt4h,
		Backtest1d: bt1d,
		Rules: []string{
			"《专业投机原理》：大周期定势，小周期找点；不逆势投机。",
			"周线/日线嵌套：HH+HL 且价在 EMA50 上 = 偏多；LH+LL 且价在 EMA50 下 = 偏空。",
			"1-2-3 转空：①收盘跌破上升结构低点(HL) → ②反抽高点不过前高 → ③收盘再破②的反弹低点。做空停损在②上方，目标约 1R（进场下对称）。",
			"1-2-3 转多：①收盘升破下降结构高点(LH) → ②回踩低点不破前低 → ③收盘再破②的反弹高点。做多停损在②下方，目标约 1R。",
			"2B：刺穿前高/前低后同一根收盘收回；空单停损刺穿极值上、目标在进场下方 1R；多单相反。须与嵌套同向。",
			"变盘认收盘，不认插针。未完成 1-2-3 前不提前开仓。「嵌套同向」仅当大周期 bias 与方向一致。",
			"回测：多币种 4h/日线，信号收盘进场，1R 停损目标，先触先平。",
			"策略 ID：prospec（enable123/enable2B/useNestFilter/requireNestAlign）。",
		},
		SymbolsUsed: syms,
		KlineSource: strings.Join(srcParts, ","),
		TookMs:      time.Since(started).Milliseconds(),
	})
}

func (s *Server) prospecBacktestPool(
	ctx context.Context,
	session *bbgo.ExchangeSession,
	symbols []string,
	iv types.Interval,
	limit, horizon int,
) prospec.BTStats {
	var (
		mu               sync.Mutex
		wg               sync.WaitGroup
		ok               int
		t123, w123, l123 int
		sum123           float64
		t2b, w2b, l2b    int
		sum2b            float64
	)
	sem := make(chan struct{}, 4)
	for _, sy := range symbols {
		wg.Add(1)
		go func(sy string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ks, _, err := s.queryAnalysisKLines(ctx, session, sy, iv, limit)
			if err != nil || len(ks) < 80 {
				return
			}
			a, b, c, d := prospec.Backtest123(ks, 3, horizon)
			e, f, g, h := prospec.Backtest2B(ks, 3, horizon)
			mu.Lock()
			defer mu.Unlock()
			ok++
			t123 += a
			w123 += b
			l123 += c
			sum123 += d
			t2b += e
			w2b += f
			l2b += g
			sum2b += h
		}(sy)
	}
	wg.Wait()
	st := prospec.SummarizeBT(ok, t123, w123, l123, sum123, t2b, w2b, l2b, sum2b, horizon)
	st.Note = string(iv) + " · " + st.Note
	return st
}

func (s *Server) prospecScan(ctx context.Context, session *bbgo.ExchangeSession, symbols []string) []prospecScanRow {
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		rows []prospecScanRow
	)
	sem := make(chan struct{}, 4)
	for _, sy := range symbols {
		wg.Add(1)
		go func(sy string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			dks, _, err1 := s.queryAnalysisKLines(ctx, session, sy, types.Interval1d, 160)
			mks, _, err2 := s.queryAnalysisKLines(ctx, session, sy, types.Interval15m, 120)
			if err1 != nil || err2 != nil || len(dks) < 60 {
				return
			}
			nest := prospec.ClassifyNest(dks, "1d")
			o123 := prospec.DetectOneTwoThree(dks, 3)
			twoB := prospec.DetectTwoB(mks, 3, 12)
			last := dks[len(dks)-1].Close.Float64()
			setup := prospec.BuildSetup(nest, o123, twoB, last)
			if setup.Kind == "wait" && o123.Stage < 2 && (twoB == nil || twoB.BarsAgo > 3) {
				return // only surface interesting rows
			}
			row := prospecScanRow{
				Symbol: sy, NestBias: nest.Bias, Stage123: o123.Stage, Dir123: o123.Direction,
				Confirmed: o123.Confirmed, Setup: setup.Kind, Side: setup.Side,
				Aligned: setup.Aligned, Label: setup.Label, Last: roundFloat(last, 8),
			}
			if twoB != nil {
				row.TwoBSide = twoB.Side
			}
			mu.Lock()
			rows = append(rows, row)
			mu.Unlock()
		}(sy)
	}
	wg.Wait()
	return rows
}
