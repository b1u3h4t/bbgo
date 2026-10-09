package prospec

import (
	"context"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

const ID = "prospec"

var log = logrus.WithField("strategy", ID)
var one = fixedpoint.One

func init() {
	bbgo.RegisterStrategy(ID, &Strategy{})
}

// Strategy implements Victor Sperandeo–style trading:
// nest filter (higher TF) + 1-2-3 trend change + 2B failed breakouts on the
// execution interval. Close confirmation only; stop beyond structure.
type Strategy struct {
	Environment *bbgo.Environment
	Market      types.Market

	Symbol   string         `json:"symbol"`
	Interval types.Interval `json:"interval"`

	// NestInterval is the higher TF bias filter (e.g. 4h when trading 15m, 1d when trading 4h).
	NestInterval  types.Interval `json:"nestInterval"`
	UseNestFilter bool           `json:"useNestFilter"`

	SwingLook int `json:"swingLook"`

	EnableLong  bool `json:"enableLong"`
	EnableShort bool `json:"enableShort"`
	Enable123   bool `json:"enable123"`
	Enable2B    bool `json:"enable2B"`

	// RequireNestAlign: 2B/123 must agree with nest bias (chop allows half-size conceptually via skip if strict).
	RequireNestAlign bool `json:"requireNestAlign"`

	Quantity fixedpoint.Value `json:"quantity"`
	Leverage fixedpoint.Value `json:"leverage"`

	// MinRR minimum reward:risk using stop→target estimate (0 disables)
	MinRR float64 `json:"minRR"`

	// RewardRisk sets take-profit distance in R multiples (ablation: 2 on daily+nest).
	RewardRisk float64 `json:"rewardRisk"`

	// TrendFit: hl | ols | ransac (default ransac; casoon≈ransac+minTouches3).
	TrendFit string `json:"trendFit"`

	Position    *types.Position    `persistence:"position"`
	ProfitStats *types.ProfitStats `persistence:"profit_stats"`
	TradeStats  *types.TradeStats  `persistence:"trade_stats"`

	session       *bbgo.ExchangeSession
	orderExecutor *bbgo.GeneralOrderExecutor

	klineBuf []types.KLine
	nestBuf  []types.KLine
	stopPx   float64

	mu sync.Mutex

	bbgo.StrategyController
}

func (s *Strategy) ID() string { return ID }

func (s *Strategy) InstanceID() string {
	return fmt.Sprintf("%s:%s:%s", ID, s.Symbol, s.Interval)
}

func (s *Strategy) Subscribe(session *bbgo.ExchangeSession) {
	if s.Interval == "" {
		s.Interval = types.Interval15m
	}
	session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: s.Interval})
	if s.UseNestFilter {
		if s.NestInterval == "" {
			s.NestInterval = types.Interval4h
		}
		session.Subscribe(types.KLineChannel, s.Symbol, types.SubscribeOptions{Interval: s.NestInterval})
	}
}

func (s *Strategy) Defaults() error {
	if s.Interval == "" {
		s.Interval = types.Interval15m
	}
	if s.NestInterval == "" {
		s.NestInterval = types.Interval4h
	}
	if s.SwingLook <= 0 {
		s.SwingLook = 3
	}
	if !s.EnableLong && !s.EnableShort {
		s.EnableLong = true
		s.EnableShort = true
	}
	if !s.Enable123 && !s.Enable2B {
		s.Enable123 = true
		s.Enable2B = true
	}
	if s.Leverage.IsZero() && s.Quantity.IsZero() {
		s.Leverage = fixedpoint.NewFromInt(1)
	}
	// Ablation defaults: 2R targets. Nest align must be enabled explicitly in yaml
	// (bool zero-value cannot distinguish unset vs false).
	if s.RewardRisk <= 0 {
		s.RewardRisk = DefaultRewardRisk
	}
	if s.TrendFit == "" {
		s.TrendFit = string(TrendFitRANSAC)
	}
	if s.Interval == types.Interval15m || s.Interval == types.Interval30m || s.Interval == types.Interval4h {
		if !s.UseNestFilter || !s.RequireNestAlign {
			log.Warnf("prospec: interval=%s without useNestFilter+requireNestAlign — ablation shows negative expectancy", s.Interval)
		}
	}
	return nil
}

func (s *Strategy) Validate() error {
	_ = s.Defaults()
	if s.Symbol == "" {
		return fmt.Errorf("prospec: symbol is required")
	}
	if s.UseNestFilter && s.NestInterval == s.Interval {
		return fmt.Errorf("prospec: nestInterval must differ from interval")
	}
	if s.SwingLook < 2 {
		return fmt.Errorf("prospec: swingLook must be >= 2")
	}
	return nil
}

func (s *Strategy) closePositionFully(ctx context.Context, tag string) {
	if err := s.orderExecutor.ClosePosition(ctx, one, tag); err != nil {
		log.WithError(err).Errorf("%s close failed", tag)
	}
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
		s.closePositionFully(context.Background(), "emergency")
		_ = s.Suspend()
	})

	session.MarketDataStream.OnKLineClosed(types.KLineWith(s.Symbol, s.Interval, func(k types.KLine) {
		s.onExecClosed(ctx, k)
	}))
	if s.UseNestFilter {
		session.MarketDataStream.OnKLineClosed(types.KLineWith(s.Symbol, s.NestInterval, func(k types.KLine) {
			s.mu.Lock()
			s.nestBuf = append(s.nestBuf, k)
			if len(s.nestBuf) > 200 {
				s.nestBuf = s.nestBuf[len(s.nestBuf)-200:]
			}
			s.mu.Unlock()
		}))
	}

	log.Infof("%s prospec start interval=%s nest=%v/%s 123=%v 2b=%v align=%v swingLook=%d",
		s.Symbol, s.Interval, s.UseNestFilter, s.NestInterval, s.Enable123, s.Enable2B, s.RequireNestAlign, s.SwingLook)
	return nil
}

func (s *Strategy) onExecClosed(ctx context.Context, k types.KLine) {
	if s.Status != types.StrategyStatusRunning {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.klineBuf = append(s.klineBuf, k)
	if len(s.klineBuf) > 300 {
		s.klineBuf = s.klineBuf[len(s.klineBuf)-300:]
	}

	last := k.Close.Float64()
	if s.Position.IsOpened(k.Close) && s.stopPx > 0 {
		if s.Position.IsLong() && last < s.stopPx {
			bbgo.Notify("%s prospec long stopped @ %s stop=%.6g", s.Symbol, k.Close.String(), s.stopPx)
			s.closePositionFully(ctx, "structStop")
			s.stopPx = 0
			return
		}
		if s.Position.IsShort() && last > s.stopPx {
			bbgo.Notify("%s prospec short stopped @ %s stop=%.6g", s.Symbol, k.Close.String(), s.stopPx)
			s.closePositionFully(ctx, "structStop")
			s.stopPx = 0
			return
		}
		return
	}

	if s.Position.IsOpened(k.Close) {
		return
	}

	nest := NestState{Bias: "chop", Note: "nest off"}
	if s.UseNestFilter {
		nest = ClassifyNest(s.nestBuf, string(s.NestInterval))
	}
	var o123 OneTwoThree
	if s.Enable123 {
		m := TrendFitMethod(s.TrendFit)
		if m != TrendFitHL && m != TrendFitOLS && m != TrendFitRANSAC {
			m = TrendFitRANSAC
		}
		o123 = DetectOneTwoThreeMethod(s.klineBuf, s.SwingLook, m)
	}
	var twoB *TwoBSignal
	if s.Enable2B {
		twoB = DetectTwoB(s.klineBuf, s.SwingLook, 16)
	}
	setup := BuildSetupRR(nest, o123, twoB, last, s.RewardRisk)
	if setup.Side == "flat" || setup.Kind == "wait" {
		return
	}
	if s.RequireNestAlign && !setup.Aligned {
		log.Infof("%s prospec skip %s: not nest-aligned (%s)", s.Symbol, setup.Label, nest.Bias)
		return
	}
	if setup.Side == "long" && !s.EnableLong {
		return
	}
	if setup.Side == "short" && !s.EnableShort {
		return
	}
	if s.MinRR > 0 && setup.Stop > 0 && setup.Target > 0 {
		risk := mathAbs(setup.Entry - setup.Stop)
		reward := mathAbs(setup.Target - setup.Entry)
		if risk > 0 && reward/risk < s.MinRR {
			log.Infof("%s prospec skip RR=%.2f < min %.2f", s.Symbol, reward/risk, s.MinRR)
			return
		}
	}

	s.open(ctx, setup, k)
}

func mathAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func (s *Strategy) open(ctx context.Context, setup Setup, k types.KLine) {
	long := setup.Side == "long"
	if !long {
		base := s.Position.GetBase()
		if base.Sign() > 0 {
			s.closePositionFully(ctx, "preShortScrub")
		}
	}
	opt := bbgo.OpenPositionOptions{
		Long:     long,
		Short:    !long,
		Quantity: s.Quantity,
		Leverage: s.Leverage,
		Price:    k.Close,
		Tags:     []string{"prospec-" + setup.Kind},
	}
	bbgo.Notify("%s prospec %s %s entry=%s stop=%.6g target=%.6g — %s",
		s.Symbol, setup.Label, setup.Side, k.Close.String(), setup.Stop, setup.Target, setup.Action)
	if _, err := s.orderExecutor.OpenPosition(ctx, opt); err != nil {
		log.WithError(err).Errorf("open %s failed", setup.Side)
		return
	}
	s.stopPx = setup.Stop
}
