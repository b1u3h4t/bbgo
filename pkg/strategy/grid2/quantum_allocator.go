package grid2

import (
	"fmt"
	"math"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

// QuantumZone mirrors Hummingbot quantum_grid_allocator allocation zones.
type QuantumZone string

const (
	QuantumZoneHedge     QuantumZone = "hedge"
	QuantumZoneLongOnly  QuantumZone = "long_only"
	QuantumZoneShortOnly QuantumZone = "short_only"
)

// QuantumAllocator adapts Hummingbot's quantum_grid_allocator to a single-symbol grid2:
//   - sizes open-order intensity from position deviation vs target
//   - biases buy vs sell when under/over target inventory
//   - optionally sets price range from gridRange or Bollinger band width
//
// Multi-asset portfolio routing is out of scope; use one strategy instance per symbol.
type QuantumAllocator struct {
	// TargetPosition is the desired signed base/contracts position (0 = flat).
	TargetPosition fixedpoint.Value `json:"targetPosition"`

	// MaxPositionAbs normalizes deviation = (actual-target)/MaxPositionAbs.
	// If zero, capacity defaults to (gridNumber-1)*quantity.
	MaxPositionAbs fixedpoint.Value `json:"maxPositionAbs"`

	LongOnlyThreshold  fixedpoint.Value `json:"longOnlyThreshold"`
	ShortOnlyThreshold fixedpoint.Value `json:"shortOnlyThreshold"`

	BaseGridValuePct fixedpoint.Value `json:"baseGridValuePct"`
	MaxGridValuePct  fixedpoint.Value `json:"maxGridValuePct"`
	MaxDeviation     fixedpoint.Value `json:"maxDeviation"`

	// GridRange is the fractional half-span around mid when DynamicGridRange is false
	// (e.g. 0.007 ≈ ±0.7%). With DynamicGridRange, BB width (BBB/100) is used instead.
	GridRange        fixedpoint.Value `json:"gridRange"`
	DynamicGridRange bool             `json:"dynamicGridRange"`

	// TpSlRatio splits the range asymmetrically around mid (HB default 0.8).
	// Above-mid span uses tpSlRatio; below-mid uses (1-tpSlRatio) for long-biased grids
	// and the reverse when short-biased.
	TpSlRatio fixedpoint.Value `json:"tpSlRatio"`

	BBLength   int            `json:"bbLength"`
	BBStd      float64        `json:"bbStd"`
	BBInterval types.Interval `json:"bbInterval"`

	// BaseMaxOpenOrders is intensity=1.0 open-order count. Effective max scales with gridValuePct.
	// If zero, falls back to Strategy.MaxOpenOrders or gridNumber-1.
	BaseMaxOpenOrders int `json:"baseMaxOpenOrders"`
}

func (q *QuantumAllocator) defaults() {
	if q.LongOnlyThreshold.IsZero() {
		q.LongOnlyThreshold = fixedpoint.NewFromFloat(0.2)
	}
	if q.ShortOnlyThreshold.IsZero() {
		q.ShortOnlyThreshold = fixedpoint.NewFromFloat(0.2)
	}
	if q.BaseGridValuePct.IsZero() {
		q.BaseGridValuePct = fixedpoint.NewFromFloat(0.08)
	}
	if q.MaxGridValuePct.IsZero() {
		q.MaxGridValuePct = fixedpoint.NewFromFloat(0.15)
	}
	if q.MaxDeviation.IsZero() {
		q.MaxDeviation = fixedpoint.NewFromFloat(0.05)
	}
	if q.TpSlRatio.IsZero() {
		q.TpSlRatio = fixedpoint.NewFromFloat(0.8)
	}
	if q.BBLength <= 0 {
		q.BBLength = 40
	}
	if q.BBStd <= 0 {
		q.BBStd = 2.0
	}
	if q.BBInterval == "" {
		q.BBInterval = types.Interval15m
	}
	// Do not invent a default GridRange: zero means "keep Strategy upper/lower"
	// and only scale intensity / side bias (Hummingbot allocator without re-banding).
}

func (q *QuantumAllocator) String() string {
	q.defaults()
	return fmt.Sprintf("qga-bb%s-%d-r%s", q.BBInterval, q.BBLength, q.GridRange.String())
}

func (q *QuantumAllocator) providesRange() bool {
	return q != nil && (q.DynamicGridRange || q.GridRange.Sign() > 0)
}

func (s *Strategy) hasQuantumRange() bool {
	return s.QuantumAllocator != nil && s.QuantumAllocator.providesRange()
}

// quantumCapacity estimates max absolute position for deviation normalization.
func (s *Strategy) quantumCapacity() fixedpoint.Value {
	q := s.QuantumAllocator
	if q != nil && q.MaxPositionAbs.Sign() > 0 {
		return q.MaxPositionAbs
	}
	qty := s.QuantityOrAmount.Quantity
	if qty.IsZero() {
		qty = fixedpoint.One
	}
	n := s.GridNum - 1
	if n < 1 {
		n = 1
	}
	return qty.Mul(fixedpoint.NewFromInt(n))
}

// quantumDeviation returns (actual-target)/capacity.
func (s *Strategy) quantumDeviation() fixedpoint.Value {
	q := s.QuantumAllocator
	if q == nil {
		return fixedpoint.Zero
	}
	actual := fixedpoint.Zero
	if s.Position != nil {
		actual = s.Position.GetBase()
	}
	diff := actual.Sub(q.TargetPosition)
	cap := s.quantumCapacity()
	if cap.IsZero() {
		return fixedpoint.Zero
	}
	return diff.Div(cap)
}

func (s *Strategy) quantumZone(deviation fixedpoint.Value) QuantumZone {
	q := s.QuantumAllocator
	if q == nil {
		return QuantumZoneHedge
	}
	if deviation.Compare(q.LongOnlyThreshold.Neg()) < 0 {
		return QuantumZoneLongOnly
	}
	if deviation.Compare(q.ShortOnlyThreshold) > 0 {
		return QuantumZoneShortOnly
	}
	return QuantumZoneHedge
}

func (s *Strategy) quantumGridValuePct(deviation fixedpoint.Value) fixedpoint.Value {
	q := s.QuantumAllocator
	q.defaults()
	if deviation.Abs().Compare(q.MaxDeviation) > 0 {
		return q.MaxGridValuePct
	}
	return q.BaseGridValuePct
}

func (s *Strategy) quantumEffectiveMaxOpenOrders(deviation fixedpoint.Value) int {
	q := s.QuantumAllocator
	q.defaults()
	base := q.BaseMaxOpenOrders
	if base <= 0 {
		base = s.MaxOpenOrders
	}
	if base <= 0 {
		base = int(s.GridNum) - 1
	}
	if base < 1 {
		base = 1
	}

	pct := s.quantumGridValuePct(deviation)
	// intensity = pct / basePct (≥1 when using max pct)
	intensity := pct.Div(q.BaseGridValuePct).Float64()
	if intensity < 1 {
		intensity = 1
	}
	n := int(math.Round(float64(base) * intensity))
	if n < 1 {
		n = 1
	}
	cap := int(s.GridNum) - 1
	if cap > 0 && n > cap {
		n = cap
	}
	return n
}

// applyQuantumAllocatorRange sets upper/lower from mid ± grid range (optional BB width).
func (s *Strategy) applyQuantumAllocatorRange(session *bbgo.ExchangeSession, mid fixedpoint.Value) error {
	q := s.QuantumAllocator
	if q == nil || !q.providesRange() || mid.IsZero() {
		return nil
	}
	q.defaults()

	gridRange := q.GridRange
	if q.DynamicGridRange {
		iw := types.IntervalWindow{Interval: q.BBInterval, Window: q.BBLength}
		boll := session.StandardIndicatorSet(s.Symbol).BOLL(iw, q.BBStd)
		up := boll.LastUpBand()
		down := boll.LastDownBand()
		sma := boll.GetSMA().Last(0)
		if up > down && sma > 0 {
			// BBB-style relative width ≈ (up-down)/sma
			gridRange = fixedpoint.NewFromFloat((up - down) / sma / 2.0) // half-span
			s.logger.Infof("quantumAllocator dynamic range from BOLL: halfSpan=%s (up=%f down=%f sma=%f)",
				gridRange.String(), up, down, sma)
		} else if gridRange.IsZero() {
			return fmt.Errorf("quantumAllocator: dynamicGridRange needs BOLL data or fallback gridRange")
		}
	}

	dev := s.quantumDeviation()
	zone := s.quantumZone(dev)
	tp := q.TpSlRatio
	sl := fixedpoint.One.Sub(tp)

	var lowerMul, upperMul fixedpoint.Value
	switch zone {
	case QuantumZoneLongOnly:
		// Favor upside take-profit / deeper buy side (HB long-only geometry)
		lowerMul = sl
		upperMul = tp
	case QuantumZoneShortOnly:
		lowerMul = tp
		upperMul = sl
	default:
		// Hedge: widen the underweight side slightly (2x on the catch-up leg)
		if dev.Sign() < 0 {
			lowerMul = sl.Mul(fixedpoint.NewFromInt(2))
			upperMul = tp
		} else if dev.Sign() > 0 {
			lowerMul = tp
			upperMul = sl.Mul(fixedpoint.NewFromInt(2))
		} else {
			lowerMul = fixedpoint.NewFromFloat(0.5)
			upperMul = fixedpoint.NewFromFloat(0.5)
			// use symmetric half of gridRange on each side when flat
			half := gridRange
			s.LowerPrice = s.Market.TruncatePrice(mid.Mul(fixedpoint.One.Sub(half)))
			s.UpperPrice = s.Market.TruncatePrice(mid.Mul(fixedpoint.One.Add(half)))
			s.logger.Infof("quantumAllocator range zone=%s mid=%s lower=%s upper=%s halfSpan=%s",
				zone, mid.String(), s.LowerPrice.String(), s.UpperPrice.String(), half.String())
			return nil
		}
	}

	s.LowerPrice = s.Market.TruncatePrice(mid.Mul(fixedpoint.One.Sub(gridRange.Mul(lowerMul))))
	s.UpperPrice = s.Market.TruncatePrice(mid.Mul(fixedpoint.One.Add(gridRange.Mul(upperMul))))
	if s.UpperPrice.Compare(s.LowerPrice) <= 0 {
		return fmt.Errorf("quantumAllocator: invalid range lower=%s upper=%s", s.LowerPrice.String(), s.UpperPrice.String())
	}

	s.logger.Infof(
		"quantumAllocator range zone=%s dev=%s mid=%s lower=%s upper=%s spanLo=%s spanHi=%s",
		zone, dev.String(), mid.String(), s.LowerPrice.String(), s.UpperPrice.String(),
		gridRange.Mul(lowerMul).Percentage(), gridRange.Mul(upperMul).Percentage(),
	)
	return nil
}

// applyQuantumAllocatorIntensity updates MaxOpenOrders from position deviation.
func (s *Strategy) applyQuantumAllocatorIntensity() {
	q := s.QuantumAllocator
	if q == nil {
		return
	}
	q.defaults()
	dev := s.quantumDeviation()
	zone := s.quantumZone(dev)
	pct := s.quantumGridValuePct(dev)
	n := s.quantumEffectiveMaxOpenOrders(dev)
	s.MaxOpenOrders = n
	s.quantumZoneState = zone
	s.logger.Infof(
		"quantumAllocator intensity zone=%s dev=%s gridValuePct=%s maxOpenOrders=%d",
		zone, dev.Percentage(), pct.Percentage(), n,
	)
}

// filterQuantumSideBias keeps the zone-appropriate side after other filters.
// long_only: all buys + nearest sells up to half of slots (for twin TP)
// short_only: all sells + nearest buys up to half
// hedge: unchanged
func (s *Strategy) filterQuantumSideBias(orders []types.SubmitOrder, lastPrice fixedpoint.Value) []types.SubmitOrder {
	if s.QuantumAllocator == nil || len(orders) == 0 {
		return orders
	}
	zone := s.quantumZoneState
	if zone == "" {
		zone = s.quantumZone(s.quantumDeviation())
	}
	if zone == QuantumZoneHedge {
		return orders
	}

	var primary, secondary []types.SubmitOrder
	for _, o := range orders {
		isBuy := o.Side == types.SideTypeBuy
		switch zone {
		case QuantumZoneLongOnly:
			if isBuy {
				primary = append(primary, o)
			} else {
				secondary = append(secondary, o)
			}
		case QuantumZoneShortOnly:
			if !isBuy {
				primary = append(primary, o)
			} else {
				secondary = append(secondary, o)
			}
		}
	}

	// Allow a few opposite-side orders for round-trip profit (nearest to mid).
	secKeep := len(secondary)
	if s.MaxOpenOrders > 0 {
		secKeep = s.MaxOpenOrders / 2
		if secKeep < 1 {
			secKeep = 1
		}
	}
	if len(secondary) > secKeep {
		type ranked struct {
			o types.SubmitOrder
			d fixedpoint.Value
		}
		rs := make([]ranked, 0, len(secondary))
		for _, o := range secondary {
			rs = append(rs, ranked{o: o, d: o.Price.Sub(lastPrice).Abs()})
		}
		// simple selection sort for small N
		for i := 0; i < len(rs); i++ {
			for j := i + 1; j < len(rs); j++ {
				if rs[j].d.Compare(rs[i].d) < 0 {
					rs[i], rs[j] = rs[j], rs[i]
				}
			}
		}
		secondary = secondary[:0]
		for i := 0; i < secKeep && i < len(rs); i++ {
			secondary = append(secondary, rs[i].o)
		}
	}

	kept := append(primary, secondary...)
	s.logger.Infof("quantumAllocator sideBias zone=%s kept %d/%d (primary=%d secondary=%d)",
		zone, len(kept), len(orders), len(primary), len(secondary))
	return kept
}
