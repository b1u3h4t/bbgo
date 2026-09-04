package grid2

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

// AutoBollinger configures Bollinger-band based grid bounds (Hummingbot bollingrid-like).
// When set, upperPrice/lowerPrice are derived from the latest BOLL bands at strategy start.
type AutoBollinger struct {
	Interval  types.Interval `json:"interval"`
	Window    int            `json:"window"`
	BandWidth float64        `json:"bandWidth"` // std multiplier K, default 2
}

func (a *AutoBollinger) defaults() {
	if a.Interval == "" {
		a.Interval = types.Interval15m
	}
	if a.Window <= 0 {
		a.Window = 100
	}
	if a.BandWidth <= 0 {
		a.BandWidth = 2.0
	}
}

func (a *AutoBollinger) String() string {
	a.defaults()
	return fmt.Sprintf("%s-%d-k%.2f", a.Interval, a.Window, a.BandWidth)
}

// filterGridSubmitOrders applies Hummingbot-style activationBounds and maxOpenOrders.
// Orders nearest to lastPrice are preferred when MaxOpenOrders is set.
// activationBounds only applies when lastPrice is inside the configured grid range;
// otherwise classic grid behavior is preserved (place the near-side book).
func (s *Strategy) filterGridSubmitOrders(orders []types.SubmitOrder, lastPrice fixedpoint.Value) []types.SubmitOrder {
	if len(orders) == 0 {
		return orders
	}

	filtered := orders
	insideGrid := !s.LowerPrice.IsZero() && !s.UpperPrice.IsZero() &&
		lastPrice.Compare(s.LowerPrice) >= 0 && lastPrice.Compare(s.UpperPrice) <= 0

	if s.ActivationBounds.Sign() > 0 && !lastPrice.IsZero() && insideGrid {
		lo := lastPrice.Mul(fixedpoint.One.Sub(s.ActivationBounds))
		hi := lastPrice.Mul(fixedpoint.One.Add(s.ActivationBounds))
		kept := make([]types.SubmitOrder, 0, len(filtered))
		for _, o := range filtered {
			if o.Price.Compare(lo) >= 0 && o.Price.Compare(hi) <= 0 {
				kept = append(kept, o)
			}
		}
		s.logger.Infof(
			"activationBounds %s: kept %d/%d orders within [%s, %s] around last=%s",
			s.ActivationBounds.Percentage(), len(kept), len(filtered), lo.String(), hi.String(), lastPrice.String(),
		)
		filtered = kept
	} else if s.ActivationBounds.Sign() > 0 && !insideGrid {
		s.logger.Infof(
			"activationBounds skipped: last=%s outside grid [%s, %s]",
			lastPrice.String(), s.LowerPrice.String(), s.UpperPrice.String(),
		)
	}

	if s.MaxOpenOrders > 0 && len(filtered) > s.MaxOpenOrders {
		type ranked struct {
			order    types.SubmitOrder
			distance fixedpoint.Value
		}
		rankedOrders := make([]ranked, 0, len(filtered))
		for _, o := range filtered {
			d := o.Price.Sub(lastPrice).Abs()
			rankedOrders = append(rankedOrders, ranked{order: o, distance: d})
		}
		sort.SliceStable(rankedOrders, func(i, j int) bool {
			return rankedOrders[i].distance.Compare(rankedOrders[j].distance) < 0
		})
		kept := make([]types.SubmitOrder, 0, s.MaxOpenOrders)
		for i := 0; i < s.MaxOpenOrders; i++ {
			kept = append(kept, rankedOrders[i].order)
		}
		s.logger.Infof("maxOpenOrders %d: kept %d nearest of %d orders", s.MaxOpenOrders, len(kept), len(filtered))
		filtered = kept
	}

	return filtered
}

// submitGridOrders submits all orders, optionally in batches (Hummingbot max_orders_per_batch).
func (s *Strategy) submitGridOrders(ctx context.Context, orders []types.SubmitOrder) (types.OrderSlice, error) {
	if len(orders) == 0 {
		return nil, nil
	}

	batchSize := s.MaxOrdersPerBatch
	if batchSize <= 0 || batchSize >= len(orders) {
		return s.orderExecutor.SubmitOrders(ctx, orders...)
	}

	var created types.OrderSlice
	freq := s.OrderFrequency.Duration()
	for i := 0; i < len(orders); i += batchSize {
		end := i + batchSize
		if end > len(orders) {
			end = len(orders)
		}
		batch := orders[i:end]
		s.logger.Infof("submitting grid order batch %d-%d / %d", i+1, end, len(orders))
		part, err := s.orderExecutor.SubmitOrders(ctx, batch...)
		if err != nil {
			return created, err
		}
		created = append(created, part...)
		if end < len(orders) && freq > 0 && !bbgo.IsBackTesting {
			time.Sleep(freq)
		}
	}
	return created, nil
}

func (s *Strategy) applyAutoBollinger(session *bbgo.ExchangeSession) error {
	if s.AutoBollinger == nil {
		return nil
	}
	s.AutoBollinger.defaults()

	iw := types.IntervalWindow{Interval: s.AutoBollinger.Interval, Window: s.AutoBollinger.Window}
	boll := session.StandardIndicatorSet(s.Symbol).BOLL(iw, s.AutoBollinger.BandWidth)
	up := boll.LastUpBand()
	down := boll.LastDownBand()
	if up <= 0 || down <= 0 || up <= down {
		return fmt.Errorf("autoBollinger: invalid bands up=%f down=%f (need more klines for %s window=%d)", up, down, s.AutoBollinger.Interval, s.AutoBollinger.Window)
	}

	s.UpperPrice = s.Market.TruncatePrice(fixedpoint.NewFromFloat(up))
	s.LowerPrice = s.Market.TruncatePrice(fixedpoint.NewFromFloat(down))
	s.logger.Infof(
		"autoBollinger enabled (%s): upper=%s lower=%s sma=%f stdK=%.2f",
		s.AutoBollinger.String(), s.UpperPrice.String(), s.LowerPrice.String(), boll.GetSMA().Last(0), s.AutoBollinger.BandWidth,
	)
	return nil
}

func (s *Strategy) newTakeProfitRatioHandler(ctx context.Context, _ *bbgo.ExchangeSession) types.KLineCallback {
	return types.KLineWith(s.Symbol, types.Interval1m, func(k types.KLine) {
		if s.TakeProfitRatio.Sign() <= 0 || s.takeProfitAnchor.IsZero() {
			return
		}
		target := s.takeProfitAnchor.Mul(fixedpoint.One.Add(s.TakeProfitRatio))
		if k.High.Compare(target) < 0 {
			return
		}

		s.logger.Infof(
			"takeProfitRatio %s hit: anchor=%s target=%s high=%s, closing grid",
			s.TakeProfitRatio.Percentage(), s.takeProfitAnchor.String(), target.String(), k.High.String(),
		)

		if err := s.CloseGrid(ctx); err != nil {
			s.logger.WithError(err).Errorf("can not close grid")
			return
		}

		base := s.Position.GetBase()
		if base.Sign() <= 0 {
			return
		}
		s.logger.Infof("position base %f > 0, closing position...", base.Float64())
		if err := s.orderExecutor.ClosePosition(ctx, fixedpoint.One, "grid2:takeProfitRatio"); err != nil {
			s.logger.WithError(err).Errorf("can not close position")
		}
	})
}

// trimExcessOpenOrders cancels farthest active maker orders when over MaxOpenOrders.
func (s *Strategy) trimExcessOpenOrders(ctx context.Context, lastPrice fixedpoint.Value) {
	if s.MaxOpenOrders <= 0 || s.orderExecutor == nil {
		return
	}
	active := s.orderExecutor.ActiveMakerOrders().Orders()
	if len(active) <= s.MaxOpenOrders {
		return
	}

	sort.SliceStable(active, func(i, j int) bool {
		di := active[i].Price.Sub(lastPrice).Abs()
		dj := active[j].Price.Sub(lastPrice).Abs()
		return di.Compare(dj) > 0 // farthest first
	})
	excess := len(active) - s.MaxOpenOrders
	toCancel := active[:excess]
	s.logger.Infof("maxOpenOrders trim: canceling %d farthest of %d active orders", excess, len(active))
	if err := s.orderExecutor.GracefulCancel(ctx, toCancel...); err != nil {
		s.logger.WithError(err).Warnf("trimExcessOpenOrders cancel error")
	}
}
