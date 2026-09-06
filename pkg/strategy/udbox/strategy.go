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

// Phase is the runtime regime: range (箱内震荡) or trend (突破跟随).
type Phase string

const (
	PhaseRange Phase = "range"
	PhaseTrend Phase = "trend"
)

// Strategy implements UD优道-style box trading with optional hybrid mode:
//
//   - range: lock a consolidation box, buy lower zone / sell upper zone (高抛低吸)
//   - trend: on close breakout, follow direction with box-edge stop + trail
//
// EnableRange=true switches between the two; false keeps pure breakout (classic Darvas).
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

	// RequireCompression enables 起涨点-style range compression filter (trend entries)
	RequireCompression  bool `json:"requireCompression"`
	CompressionLookback int  `json:"compressionLookback"`

	// EnableLong / EnableShort — UD: 上涨结构做多为主, 下跌结构做空为主
	EnableLong  bool `json:"enableLong"`
	EnableShort bool `json:"enableShort"`

	// Nested (优道嵌套): higher timeframe trend filter (mainly for trend phase)
	NestInterval  types.Interval `json:"nestInterval"`
	NestEMAWindow int            `json:"nestEMAWindow"`
	UseNestFilter bool           `json:"useNestFilter"`

	// Position sizing (trend). Range uses RangeQuantity or Quantity*RangeQtyRatio.
	Quantity fixedpoint.Value `json:"quantity"`
	Leverage fixedpoint.Value `json:"leverage"`

	// Hybrid range (箱内震荡)
	EnableRange      bool             `json:"enableRange"`
	RangeBuyZonePct  float64          `json:"rangeBuyZonePct"`  // bottom fraction of box width
	RangeSellZonePct float64          `json:"rangeSellZonePct"` // top fraction
	RangeQuantity    fixedpoint.Value `json:"rangeQuantity"`    // if zero, Quantity * RangeQtyRatio
	RangeQtyRatio    float64          `json:"rangeQtyRatio"`    // default 0.5
	RangeTakeMid     bool             `json:"rangeTakeMid"`     // TP at box mid; else opposite zone
	// RangeRequireCompression: only lock/trade range when VolatilityCompressing (起涨点收敛)
	RangeRequireCompression bool `json:"rangeRequireCompression"`

	// Exit (trend)
	UseBoxStop    bool    `json:"useBoxStop"`
	TrailNewBox   bool    `json:"trailNewBox"`
	RoiTakeProfit float64 `json:"roiTakeProfit"` // e.g. 0.05 = +5% ROI, 0 to disable

	// Persistence
	Position    *types.Position    `persistence:"position"`
	ProfitStats *types.ProfitStats `persistence:"profit_stats"`
	TradeStats  *types.TradeStats  `persistence:"trade_stats"`

	// runtime
	session       *bbgo.ExchangeSession
	orderExecutor *bbgo.GeneralOrderExecutor

	klineBuf []types.KLine

	// lockedBox is frozen while ranging until breakout (avoids rolling window drift)
	lockedBox    Box
	hasLockedBox bool
	phase        Phase
	stopPrice    float64
	entryBox     Box

	mu sync.Mutex

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
		s.MinBoxWidthPct = 0.008
	}
	if s.MaxBoxWidthPct <= 0 {
		s.MaxBoxWidthPct = 0.08
	}
	if s.CompressionLookback <= 0 {
		s.CompressionLookback = 10
	}
	if s.NestEMAWindow <= 0 {
		s.NestEMAWindow = 20
	}
	if !s.EnableLong && !s.EnableShort {
		s.EnableLong = true
		s.EnableShort = true
	}
	if s.Leverage.IsZero() && s.Quantity.IsZero() {
		s.Leverage = fixedpoint.NewFromInt(1)
	}
	if s.RangeBuyZonePct <= 0 {
		s.RangeBuyZonePct = 0.25
	}
	if s.RangeSellZonePct <= 0 {
		s.RangeSellZonePct = 0.25
	}
	if s.RangeQtyRatio <= 0 {
		s.RangeQtyRatio = 0.5
	}
	if s.EnableRange {
		s.phase = PhaseRange
	} else {
		s.phase = PhaseTrend
	}
	return nil
}

func (s *Strategy) rangeQty() fixedpoint.Value {
	if !s.RangeQuantity.IsZero() {
		return s.RangeQuantity
	}
	if !s.Quantity.IsZero() {
		return s.Quantity.Mul(fixedpoint.NewFromFloat(s.RangeQtyRatio))
	}
	return s.Quantity
}

// closePositionFully closes and retries once to clear residual dust that can
// block the next short (spot-style wallets) or leave a dust long open.
func (s *Strategy) closePositionFully(ctx context.Context, tag string) {
	if err := s.orderExecutor.ClosePosition(ctx, one, tag); err != nil {
		log.WithError(err).Errorf("%s close failed", tag)
	}
	if s.Position == nil {
		return
	}
	base := s.Position.GetBase()
	if base.IsZero() {
		return
	}
	if err := s.orderExecutor.ClosePosition(ctx, one, tag+"Dust"); err != nil {
		log.WithError(err).Warnf("%s dust scrub failed base=%s", tag, base.String())
	}
}

// scrubBeforeShort closes any residual long/dust so a fresh short can open.
func (s *Strategy) scrubBeforeShort(ctx context.Context) {
	if s.Position == nil {
		return
	}
	base := s.Position.GetBase()
	if base.Sign() > 0 {
		s.closePositionFully(ctx, "preShortScrub")
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

	if s.UseNestFilter {
		s.nestEMA = session.StandardIndicatorSet(s.Symbol).EWMA(types.IntervalWindow{
			Interval: s.NestInterval,
			Window:   s.NestEMAWindow,
		})
	}

	session.MarketDataStream.OnKLineClosed(types.KLineWith(s.Symbol, s.Interval, func(k types.KLine) {
		s.onKLineClosed(ctx, k)
	}))

	log.Infof("%s udbox started interval=%s boxWindow=%d range=%v nest=%v long=%v short=%v",
		s.Symbol, s.Interval, s.BoxWindow, s.EnableRange, s.UseNestFilter, s.EnableLong, s.EnableShort)
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

	hist := s.klineBuf
	if len(hist) > 1 {
		hist = hist[:len(hist)-1]
	}
	detected, okDetect := DetectBox(hist, s.BoxWindow, s.MinBoxWidthPct, s.MaxBoxWidthPct)

	closePx := k.Close.Float64()

	// Acquire / keep locked box for hybrid range
	if s.EnableRange {
		canLock := okDetect && detected.IsInside(closePx)
		if canLock && s.RangeRequireCompression && !VolatilityCompressing(hist, s.CompressionLookback) {
			canLock = false
		}
		if !s.hasLockedBox && canLock {
			s.lockedBox = detected
			s.hasLockedBox = true
			s.phase = PhaseRange
			log.Infof("%s lock range box [%.6g, %.6g] width=%.3f%% compress=%v",
				s.Symbol, detected.Bottom, detected.Top, detected.WidthPct()*100, s.RangeRequireCompression)
		}
	}

	box := detected
	hasBox := okDetect
	if s.hasLockedBox {
		box = s.lockedBox
		hasBox = true
	}

	posOpen := s.Position.IsOpened(k.Close)

	// --- breakout vs locked/detected box → trend ---
	if hasBox && (box.BreakLong(closePx, s.BreakBufferPct) || box.BreakShort(closePx, s.BreakBufferPct)) {
		s.handleBreakout(ctx, k, box, hist)
		return
	}

	// --- manage open positions ---
	if posOpen {
		if s.phase == PhaseTrend || !s.EnableRange {
			s.manageTrendPosition(ctx, k, detected, okDetect)
		} else {
			s.manageRangePosition(ctx, k, box)
		}
		return
	}

	if !hasBox {
		return
	}

	// --- flat: range or wait for breakout ---
	if s.EnableRange && s.phase == PhaseRange && box.IsInside(closePx) {
		s.tryRangeEntry(ctx, k, box)
		return
	}

	// pure trend mode (or unlocked): wait outside box
	if !s.EnableRange {
		if s.RequireCompression && !VolatilityCompressing(hist, s.CompressionLookback) {
			return
		}
		if box.IsInside(closePx) {
			return
		}
		nestOKLong, nestOKShort := s.nestBias(closePx)
		if s.EnableLong && box.BreakLong(closePx, s.BreakBufferPct) && nestOKLong {
			s.openTrend(ctx, true, k, box)
		} else if s.EnableShort && box.BreakShort(closePx, s.BreakBufferPct) && nestOKShort {
			s.openTrend(ctx, false, k, box)
		}
	}
}

func (s *Strategy) handleBreakout(ctx context.Context, k types.KLine, box Box, hist []types.KLine) {
	closePx := k.Close.Float64()
	up := box.BreakLong(closePx, s.BreakBufferPct)
	down := box.BreakShort(closePx, s.BreakBufferPct)

	if s.RequireCompression && s.phase != PhaseTrend && !VolatilityCompressing(hist, s.CompressionLookback) {
		// still allow breakout when already ranging with locked box
		if !(s.EnableRange && s.hasLockedBox) {
			return
		}
	}

	nestOKLong, nestOKShort := s.nestBias(closePx)
	wantLong := up && s.EnableLong && nestOKLong
	wantShort := down && s.EnableShort && nestOKShort
	if !wantLong && !wantShort {
		// unlock failed breakout attempt if price clearly left
		if s.hasLockedBox && !box.IsInside(closePx) {
			log.Infof("%s breakout ignored by nest/side filter, unlock box", s.Symbol)
			s.unlockBox()
		}
		return
	}

	s.phase = PhaseTrend

	// Flip / align inventory from range → trend
	if s.Position.IsOpened(k.Close) {
		if wantLong && s.Position.IsShort() {
			bbgo.Notify("%s udbox break UP — close short, flip long", s.Symbol)
			s.closePositionFully(ctx, "breakFlip")
		} else if wantShort && s.Position.IsLong() {
			bbgo.Notify("%s udbox break DOWN — close long, flip short", s.Symbol)
			s.closePositionFully(ctx, "breakFlip")
		} else if wantLong && s.Position.IsLong() {
			// already aligned: promote to trend stops
			s.entryBox = box
			s.stopPrice = box.Bottom
			bbgo.Notify("%s udbox break UP — promote long to trend stop=%.6g", s.Symbol, s.stopPrice)
			s.unlockBox()
			return
		} else if wantShort && s.Position.IsShort() {
			s.entryBox = box
			s.stopPrice = box.Top
			bbgo.Notify("%s udbox break DOWN — promote short to trend stop=%.6g", s.Symbol, s.stopPrice)
			s.unlockBox()
			return
		}
	} else if wantShort {
		// dust long can still occupy base in spot-style wallets
		s.scrubBeforeShort(ctx)
	}

	if wantLong {
		s.openTrend(ctx, true, k, box)
	} else if wantShort {
		s.openTrend(ctx, false, k, box)
	}
	s.unlockBox()
}

func (s *Strategy) unlockBox() {
	s.hasLockedBox = false
	s.lockedBox = Box{}
}

func (s *Strategy) tryRangeEntry(ctx context.Context, k types.KLine, box Box) {
	if s.RangeRequireCompression {
		hist := s.klineBuf
		if len(hist) > 1 {
			hist = hist[:len(hist)-1]
		}
		if !VolatilityCompressing(hist, s.CompressionLookback) {
			return
		}
	}
	closePx := k.Close.Float64()
	qty := s.rangeQty()

	if s.EnableLong && box.InLowerZone(closePx, s.RangeBuyZonePct) {
		bbgo.Notify("%s udbox RANGE buy zone close=%s box=[%.6g, %.6g]",
			s.Symbol, k.Close.String(), box.Bottom, box.Top)
		opt := bbgo.OpenPositionOptions{
			Long: true, Quantity: qty, Leverage: s.Leverage, Price: k.Close,
			Tags: []string{"udbox-range-buy"},
		}
		if _, err := s.orderExecutor.OpenPosition(ctx, opt); err != nil {
			log.WithError(err).Error("range long open failed")
			return
		}
		s.stopPrice = box.Bottom * (1 - s.BreakBufferPct) // soft stop under box
		s.entryBox = box
		return
	}

	if s.EnableShort && box.InUpperZone(closePx, s.RangeSellZonePct) {
		s.scrubBeforeShort(ctx)
		bbgo.Notify("%s udbox RANGE sell zone close=%s box=[%.6g, %.6g]",
			s.Symbol, k.Close.String(), box.Bottom, box.Top)
		opt := bbgo.OpenPositionOptions{
			Short: true, Quantity: qty, Leverage: s.Leverage, Price: k.Close,
			Tags: []string{"udbox-range-sell"},
		}
		if _, err := s.orderExecutor.OpenPosition(ctx, opt); err != nil {
			log.WithError(err).Error("range short open failed")
			return
		}
		s.stopPrice = box.Top * (1 + s.BreakBufferPct)
		s.entryBox = box
	}
}

func (s *Strategy) manageRangePosition(ctx context.Context, k types.KLine, box Box) {
	closePx := k.Close.Float64()

	// Hard stop: leave box against us without confirmed breakout handling
	// (breakout path already ran first; here handle TP)
	if s.Position.IsLong() {
		tp := box.Mid()
		if !s.RangeTakeMid {
			tp = box.Top - box.Width()*s.RangeSellZonePct
		}
		if closePx >= tp {
			bbgo.Notify("%s udbox RANGE long TP @ %s", s.Symbol, k.Close.String())
			s.closePositionFully(ctx, "rangeTP")
			s.stopPrice = 0
			return
		}
		if closePx < box.Bottom {
			// failed support — close; breakout handler may also flip
			bbgo.Notify("%s udbox RANGE long stop under box @ %s", s.Symbol, k.Close.String())
			s.closePositionFully(ctx, "rangeStop")
			s.stopPrice = 0
			return
		}
	}

	if s.Position.IsShort() {
		tp := box.Mid()
		if !s.RangeTakeMid {
			tp = box.Bottom + box.Width()*s.RangeBuyZonePct
		}
		if closePx <= tp {
			bbgo.Notify("%s udbox RANGE short TP @ %s", s.Symbol, k.Close.String())
			s.closePositionFully(ctx, "rangeTP")
			s.stopPrice = 0
			return
		}
		if closePx > box.Top {
			bbgo.Notify("%s udbox RANGE short stop above box @ %s", s.Symbol, k.Close.String())
			s.closePositionFully(ctx, "rangeStop")
			s.stopPrice = 0
		}
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
	longOK = price >= ema
	shortOK = price <= ema
	return
}

func (s *Strategy) openTrend(ctx context.Context, long bool, k types.KLine, box Box) {
	s.phase = PhaseTrend
	if !long {
		s.scrubBeforeShort(ctx)
	}
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
	bbgo.Notify("%s udbox TREND %s breakout close=%s box=[%.6g, %.6g]",
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

func (s *Strategy) manageTrendPosition(ctx context.Context, k types.KLine, box Box, hasBox bool) {
	closePx := k.Close.Float64()

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
			s.closePositionFully(ctx, "boxStop")
			s.stopPrice = 0
			s.phase = PhaseRange
			return
		}
	}

	if s.RoiTakeProfit > 0 {
		roi := s.Position.ROI(k.Close)
		if roi.Float64() >= s.RoiTakeProfit {
			bbgo.Notify("%s udbox ROI take profit %.2f%%", s.Symbol, roi.Float64()*100)
			s.closePositionFully(ctx, "roiTakeProfit")
			s.stopPrice = 0
			s.phase = PhaseRange
		}
	}
}
