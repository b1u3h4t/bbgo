package udbox

import (
	"context"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

const ID = "udbox"

var log = logrus.WithField("strategy", ID)
var one = fixedpoint.One

func init() {
	bbgo.RegisterStrategy(ID, &Strategy{})
}

// Strategy implements UD优道-style 箱体突破 as a trend-following system.
//
// Core mapping from channel logic (Darvas + UD daily practice):
//  1. 主做周期 Interval: build consolidation box from recent N bars
//  2. 嵌套过滤 NestInterval + NestEMAWindow: only long when HTF EMA rising, short when falling
//  3. 箱内不做: wait for close breakout of box top/bottom (出方向)
//  4. 起涨点: optional volatility compression before breakout
//  5. 停损: opposite side of the triggering box
//  6. 移动停损: after breakout, trail stop to new box bottom (long) / top (short)
//
// This is intentionally NOT a grid: it holds directional inventory with defined risk.
type Strategy struct {
	Environment *bbgo.Environment
	Market      types.Market

	Symbol string `json:"symbol"`

	// Interval is the primary trading timeframe (主做周期), e.g. 4h or 1d
	Interval types.Interval `json:"interval"`

	// BoxWindow number of bars used to compute the active box
	BoxWindow int `json:"boxWindow"`

	// MinBoxWidthPct / MaxBoxWidthPct filter "tradable" boxes (too narrow = noise, too wide = bad R)
	MinBoxWidthPct float64 `json:"minBoxWidthPct"`
	MaxBoxWidthPct float64 `json:"maxBoxWidthPct"`

	// BreakBufferPct requires close beyond box edge by this fraction (e.g. 0.001 = 0.1%)
	BreakBufferPct float64 `json:"breakBufferPct"`

	// RequireCompression enables 起涨点-style range compression filter
	RequireCompression bool `json:"requireCompression"`
	CompressionLookback int  `json:"compressionLookback"`

	// EnableLong / EnableShort — UD: 上涨结构做多为主, 下跌结构做空为主
	EnableLong  bool `json:"enableLong"`
	EnableShort bool `json:"enableShort"`

	// Nested (优道嵌套): higher timeframe trend filter
	NestInterval  types.Interval `json:"nestInterval"`
	NestEMAWindow int            `json:"nestEMAWindow"`
	UseNestFilter bool           `json:"useNestFilter"`

	// Position sizing
	Quantity fixedpoint.Value `json:"quantity"`
	Leverage fixedpoint.Value `json:"leverage"`

	// Exit
	UseBoxStop     bool    `json:"useBoxStop"`
	TrailNewBox    bool    `json:"trailNewBox"`
	RoiTakeProfit  float64 `json:"roiTakeProfit"` // e.g. 0.05 = +5% ROI, 0 to disable

	// Persistence
	Position    *types.Position    `persistence:"position"`
	ProfitStats *types.ProfitStats `persistence:"profit_stats"`
	TradeStats  *types.TradeStats  `persistence:"trade_stats"`

	// runtime
	session       *bbgo.ExchangeSession
	orderExecutor *bbgo.GeneralOrderExecutor

	klineBuf   []types.KLine
	activeBox  Box
	hasBox     bool
	stopPrice  float64 // trailed stop
	entryBox   Box
	mu         sync.Mutex

	nestEMA types.Float64Indicator

	bbgo.StrategyController
}

func (s *Strategy) ID() string { return ID }

func (s *Strategy) InstanceID() string {
	return fmt.Sprintf("%s:%s:%s", ID, s.Symbol, s.Interval)
}

func (s *Strategy) Subscribe(session *bbgo.ExchangeSession) {
	if s.Interval == "" {
		s.Interval = types.Interval4h
	}
	session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: s.Interval})
	if s.UseNestFilter {
		if s.NestInterval == "" {
			s.NestInterval = types.Interval1d
		}
		session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: s.NestInterval})
	}
}

func (s *Strategy) Defaults() error {
	if s.BoxWindow <= 0 {
		s.BoxWindow = 20
	}
	if s.MinBoxWidthPct <= 0 {
		s.MinBoxWidthPct = 0.008 // 0.8%
	}
	if s.MaxBoxWidthPct <= 0 {
		s.MaxBoxWidthPct = 0.08 // 8%
	}
	if s.CompressionLookback <= 0 {
		s.CompressionLookback = 10
	}
	if s.NestEMAWindow <= 0 {
		s.NestEMAWindow = 20
	}
	// default both sides on for futures; spot users can disable short
	if !s.EnableLong && !s.EnableShort {
		s.EnableLong = true
		s.EnableShort = true
	}
	// Prefer enabling stops in yaml; if neither sizing field set, keep leverage default
	if s.Leverage.IsZero() && s.Quantity.IsZero() {
		s.Leverage = fixedpoint.NewFromInt(1)
	}
	return nil
}

func (s *Strategy) Run(ctx context.Context, _ bbgo.OrderExecutor, session *bbgo.ExchangeSession) error {
	_ = s.Defaults()
	s.session = session

	if s.Position == nil {
		s.Position = types.NewPositionFromMarket(s.Market)
	}
	if s.ProfitStats == nil {
		s.ProfitStats = types.NewProfitStats(s.Market)
	}
	if s.TradeStats == nil {
		s.TradeStats = types.NewTradeStats(s.Symbol)
	}

	instanceID := s.InstanceID()
	s.orderExecutor = bbgo.NewGeneralOrderExecutor(session, s.Symbol, ID, instanceID, s.Position)
	s.orderExecutor.BindEnvironment(s.Environment)
	s.orderExecutor.BindProfitStats(s.ProfitStats)
	s.orderExecutor.BindTradeStats(s.TradeStats)
	s.orderExecutor.Bind()

	s.Status = types.StrategyStatusRunning
	s.OnSuspend(func() { _ = s.orderExecutor.GracefulCancel(ctx) })
	s.OnEmergencyStop(func() {
		_ = s.orderExecutor.ClosePosition(context.Background(), one, "emergency")
		_ = s.Suspend()
	})

	if s.UseNestFilter {
		s.nestEMA = session.StandardIndicatorSet(s.Symbol).EWMA(types.IntervalWindow{
			Interval: s.NestInterval,
			Window:   s.NestEMAWindow,
		})
	}

	session.MarketDataStream.OnKLineClosed(types.KLineWith(s.Symbol, s.Interval, func(k types.KLine) {
		s.onKLineClosed(ctx, k)
	}))

	log.Infof("%s udbox started interval=%s boxWindow=%d nest=%v long=%v short=%v",
		s.Symbol, s.Interval, s.BoxWindow, s.UseNestFilter, s.EnableLong, s.EnableShort)
	return nil
}

func (s *Strategy) onKLineClosed(ctx context.Context, k types.KLine) {
	if s.Status != types.StrategyStatusRunning {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.klineBuf = append(s.klineBuf, k)
	maxKeep := s.BoxWindow*4 + 50
	if len(s.klineBuf) > maxKeep {
		s.klineBuf = s.klineBuf[len(s.klineBuf)-maxKeep:]
	}

	// Build box from bars BEFORE the current closed kline so the breakout bar
	// does not inflate the box high/low (classic Darvas / UD "出方向" semantics).
	hist := s.klineBuf
	if len(hist) > 1 {
		hist = hist[:len(hist)-1]
	}
	box, ok := DetectBox(hist, s.BoxWindow, s.MinBoxWidthPct, s.MaxBoxWidthPct)
	s.hasBox = ok
	if ok {
		s.activeBox = box
		log.Debugf("%s box top=%.6g bottom=%.6g width=%.3f%%",
			s.Symbol, box.Top, box.Bottom, box.WidthPct()*100)
	}

	closePx := k.Close.Float64()
	posOpen := s.Position.IsOpened(k.Close)

	// manage open position: stop / trail / ROI
	if posOpen {
		s.managePosition(ctx, k, box, ok)
		return
	}

	if !ok {
		return
	}

	if s.RequireCompression && !VolatilityCompressing(hist, s.CompressionLookback) {
		log.Debugf("%s skip: no volatility compression", s.Symbol)
		return
	}

	// inside box → 箱内观望
	if box.IsInside(closePx) {
		return
	}

	nestOKLong, nestOKShort := s.nestBias(closePx)

	if s.EnableLong && box.BreakLong(closePx, s.BreakBufferPct) && nestOKLong {
		s.open(ctx, true, k, box)
		return
	}
	if s.EnableShort && box.BreakShort(closePx, s.BreakBufferPct) && nestOKShort {
		s.open(ctx, false, k, box)
	}
}

func (s *Strategy) nestBias(price float64) (longOK, shortOK bool) {
	longOK, shortOK = true, true
	if !s.UseNestFilter || s.nestEMA == nil {
		return
	}
	ema := s.nestEMA.Last(0)
	if ema <= 0 {
		return
	}
	// simple nest: price vs HTF EMA — UD nested structure filter
	longOK = price >= ema
	shortOK = price <= ema
	return
}

func (s *Strategy) open(ctx context.Context, long bool, k types.KLine, box Box) {
	opt := bbgo.OpenPositionOptions{
		Long:     long,
		Short:    !long,
		Quantity: s.Quantity,
		Leverage: s.Leverage,
		Price:    k.Close,
		Tags:     []string{"udbox-break"},
	}
	side := "long"
	if !long {
		side = "short"
	}
	bbgo.Notify("%s udbox %s breakout close=%s box=[%.6g, %.6g]",
		s.Symbol, side, k.Close.String(), box.Bottom, box.Top)

	if _, err := s.orderExecutor.OpenPosition(ctx, opt); err != nil {
		log.WithError(err).Errorf("open %s position failed", side)
		return
	}

	s.entryBox = box
	if long {
		s.stopPrice = box.Bottom
	} else {
		s.stopPrice = box.Top
	}
}

func (s *Strategy) managePosition(ctx context.Context, k types.KLine, box Box, hasBox bool) {
	closePx := k.Close.Float64()

	// trail stop to new box edge after expansion (移动停损到新箱底/顶)
	if s.TrailNewBox && hasBox {
		if s.Position.IsLong() && box.Bottom > s.stopPrice {
			s.stopPrice = box.Bottom
			log.Infof("%s trail long stop → %.6g (new box bottom)", s.Symbol, s.stopPrice)
		}
		if s.Position.IsShort() && (s.stopPrice == 0 || box.Top < s.stopPrice) {
			s.stopPrice = box.Top
			log.Infof("%s trail short stop → %.6g (new box top)", s.Symbol, s.stopPrice)
		}
	}

	if s.UseBoxStop && s.stopPrice > 0 {
		hit := false
		if s.Position.IsLong() && closePx < s.stopPrice {
			bbgo.Notify("%s udbox long stopped @ %s (stop=%.6g)", s.Symbol, k.Close.String(), s.stopPrice)
			hit = true
		}
		if s.Position.IsShort() && closePx > s.stopPrice {
			bbgo.Notify("%s udbox short stopped @ %s (stop=%.6g)", s.Symbol, k.Close.String(), s.stopPrice)
			hit = true
		}
		if hit {
			if err := s.orderExecutor.ClosePosition(ctx, one, "boxStop"); err != nil {
				log.WithError(err).Error("boxStop close failed")
			}
			// Clear any dust left by base-denominated fees in backtest/spot-like fee mode.
			if s.Position.IsOpened(k.Close) {
				_ = s.orderExecutor.ClosePosition(ctx, one, "boxStopDust")
			}
			s.stopPrice = 0
			return
		}
	}

	if s.RoiTakeProfit > 0 {
		roi := s.Position.ROI(k.Close)
		if roi.Float64() >= s.RoiTakeProfit {
			bbgo.Notify("%s udbox ROI take profit %.2f%%", s.Symbol, roi.Float64()*100)
			_ = s.orderExecutor.ClosePosition(ctx, one, "roiTakeProfit")
			s.stopPrice = 0
		}
	}
}
