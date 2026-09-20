package grid2

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/pkg/errors"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/exchange"
	maxapi "github.com/c9s/bbgo/pkg/exchange/max/maxapi"
	"github.com/c9s/bbgo/pkg/exchange/retry"
	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/strategy/grid2/grid2types"
	"github.com/c9s/bbgo/pkg/types"
	"github.com/c9s/bbgo/pkg/util/timejitter"
)

var syncWindow = -3 * time.Minute

func (s *Strategy) initializeRecoverC() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	isInitialize := false

	if s.recoverC == nil {
		s.logger.Info("[Recover] initializing recover channel")
		s.recoverC = make(chan struct{}, 1)
	} else {
		s.logger.Info("[Recover] recover channel is already initialized, trigger active orders recover")
		isInitialize = true

		select {
		case s.recoverC <- struct{}{}:
			s.logger.Info("[Recover] trigger active orders recover")
		default:
			s.logger.Info("[Recover] activeOrdersRecoverC is full")
		}
	}

	return isInitialize
}

func (s *Strategy) recoverPeriodically(ctx context.Context) {
	if isInitialize := s.initializeRecoverC(); isInitialize {
		return
	}

	interval := timejitter.Milliseconds(25*time.Minute, 10*60*1000)
	s.logger.Infof("[Recover] interval: %s", interval)

	recoverTicker := time.NewTicker(interval)
	defer recoverTicker.Stop()

	syncMarketTicker := time.NewTicker(4 * time.Hour)
	defer syncMarketTicker.Stop()

	var lastRecoverTime time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-recoverTicker.C:
			if s.gridStopped.Load() {
				s.logger.Info("[Recover] grid was closed, stop recover loop")
				return
			}
			s.recoverC <- struct{}{}
		case <-syncMarketTicker.C:
			if err := s.ExchangeSession.UpdateMarkets(ctx); err != nil {
				s.logger.WithError(err).Warn("failed to update markets")
			}
		case <-s.recoverC:
			if s.gridStopped.Load() {
				s.logger.Info("[Recover] grid was closed, stop recover loop")
				return
			}
			// if we already recovered in 10 min, we should skip to avoid recovering too frequently
			if !time.Now().After(lastRecoverTime.Add(10 * time.Minute)) {
				continue
			}

			if err := s.recover(ctx); err != nil {
				s.logger.WithError(err).Error("failed to recover")
			} else {
				lastRecoverTime = time.Now()
			}
		}
	}
}

/*
  	Background knowledge
  	1. active orderbook add orders only when receive new order event or call Add/Update method manually
  	2. active orderbook remove orders only when receive filled/cancelled event or call Remove/Update method manually
  	As a result
  	1. at the same twin-order-price, there is no order in open orders and no order in active orderbook
		- failed to create the order
			=> query the last order from trades to emit filled, and it will submit again
		- not receive new order event and the order filled before we find it.
			=> query the untracked order (also is the last order) from trades to emit filled and it will submit the reversed order
  	2. at the same twin-order-price, there is order in open orders but not in active orderbook
  		- not receive new order event
		  	=> add order into active orderbook
  	3. at the same twin-order-price, there is order in active orderbook but not in open orders
  		- not receive filled event
			=> query the filled order and call Update method
  	4. at the same twin-order-price, there are different orders in open orders and active orderbook
	  	- should not happen !!!
		  	=> log error
	5. at the same twin-order-price, there is the same order in open orders and active orderbook
		- normal case
			=> no need to do anything
	After killing pod, active orderbook must be empty. we can think it is the same as not receive new event.
	Process
	1. build twin orderbook with pins and open orders.
	2. build twin orderbook with pins and active orders.
	3. compare above twin orderbooks to add open orders into active orderbook and update active orders.
	4. run grid recover to make sure all the twin price has its order.
*/

func (s *Strategy) recover(ctx context.Context) error {
	s.logger.Info("[Recover] try to recover")
	if s.gridStopped.Load() {
		s.logger.Info("[Recover] grid was closed, skip recover")
		return nil
	}

	historyService, implemented := s.session.Exchange.(types.ExchangeTradeHistoryService)
	// if the exchange doesn't support ExchangeTradeHistoryService, do not run recover
	if !implemented {
		s.logger.Warn("[Recover] ExchangeTradeHistoryService is not implemented, can not recover grid")
		return nil
	}

	activeOrderBook := s.orderExecutor.ActiveMakerOrders()
	activeOrders := activeOrderBook.Orders()

	openOrders, err := retry.QueryOpenOrdersUntilSuccessfulLite(ctx, s.session.Exchange, s.Symbol)
	if err != nil {
		return err
	}

	// check if it's new strategy or need to recover
	if len(activeOrders) == 0 && len(openOrders) == 0 && s.GridProfitStats.InitialOrderID == 0 {
		// even though there is no open orders and initial orderID is 0
		// we still need to query trades to make sure if we need to recover or not
		trades, err := historyService.QueryTrades(ctx, s.Symbol, &types.TradeQueryOptions{
			// from 1, because some API will ignore 0 last trade id
			LastTradeID: 1,
			// if there is any trades, we need to recover.
			Limit: 1,
		})

		if err != nil {
			return errors.Wrapf(err, "[Recover] unable to query trades when recovering")
		}

		if len(trades) == 0 {
			s.logger.Info("[Recover] no open order, no active order, no trade, it's a new strategy so no need to recover")
			return nil
		}
	}

	s.logger.Info("[Recover] start recovering")

	if s.gridStopped.Load() {
		s.logger.Info("[Recover] grid was closed, skip recover")
		return nil
	}

	// Drop closing orders that would lock in a loss (e.g. WLD short: buy above avgCost).
	if err := s.cancelUnprofitableClosingOrders(ctx, openOrders); err != nil {
		s.logger.WithError(err).Warn("[Recover] cancel unprofitable closing orders failed")
	} else {
		// Re-query after cancel so twin sync does not resurrect canceled IDs.
		openOrders, err = retry.QueryOpenOrdersUntilSuccessfulLite(ctx, s.session.Exchange, s.Symbol)
		if err != nil {
			return err
		}
	}

	if s.getGrid() == nil {
		// Intentional CloseGrid sets gridStopped; do not rebuild from history.
		if s.gridStopped.Load() {
			s.logger.Info("[Recover] grid was closed, skip rebuilding grid")
			return nil
		}
		s.setGrid(s.newGrid())
	}

	pins := s.getGrid().Pins
	if pins == nil {
		return fmt.Errorf("[Recover] grid pins are nil")
	}

	syncBefore := time.Now().Add(syncWindow)

	s.mu.Lock()
	if s.gridStopped.Load() {
		s.mu.Unlock()
		s.logger.Info("[Recover] grid was closed, skip recover")
		return nil
	}

	activeOrdersInTwinOrderBook, err := buildTwinOrderBook(pins, activeOrders)
	if err != nil {
		s.mu.Unlock()
		return errors.Wrapf(err, "[Recover] failed to build twin orderbook from active orders")
	}
	openOrdersInTwinOrderBook, err := buildTwinOrderBook(pins, openOrders)
	if err != nil {
		s.mu.Unlock()
		return errors.Wrapf(err, "[Recover] failed to build twin orderbook from open orders")
	}

	s.logger.Infof("[Recover] active orders' twin orderbook\n%s", activeOrdersInTwinOrderBook.String())
	s.logger.Infof("[Recover] open orders in twin orderbook\n%s", openOrdersInTwinOrderBook.String())

	// remove index 0, because twin orderbook's price is from the second one
	pins = pins[1:]
	var noTwinOrderPins []fixedpoint.Value
	var case3Orders []types.Order

	for _, pin := range pins {
		v := fixedpoint.Value(pin)
		activeOrder := activeOrdersInTwinOrderBook.GetTwinOrder(v)
		openOrder := openOrdersInTwinOrderBook.GetTwinOrder(v)
		if activeOrder == nil || openOrder == nil {
			s.mu.Unlock()
			return fmt.Errorf("this pin (%s) is invalid. Please check it.", v.String())
		}

		var activeOrderID uint64 = 0
		if activeOrder.Exist() {
			activeOrderID = activeOrder.GetOrder().OrderID
		}

		var openOrderID uint64 = 0
		if openOrder.Exist() {
			openOrderID = openOrder.GetOrder().OrderID
		}

		// case 1
		if activeOrderID == 0 && openOrderID == 0 {
			noTwinOrderPins = append(noTwinOrderPins, v)
			continue
		}

		// case 2
		if activeOrderID == 0 {
			order := openOrder.GetOrder()
			s.logger.Infof("[Recover] found open order #%d is not in the active orderbook, adding...", order.OrderID)

			if order.UpdateTime.Before(syncBefore) {
				activeOrderBook.Add(order)
				// also add open orders into active order's twin orderbook, we will use this active orderbook to recover empty price grid
				activeOrdersInTwinOrderBook.AddOrder(order, true)
			} else {
				s.logger.Infof("[Recover] open order #%d is updated in 3 min, skip adding...", order.OrderID)
			}
			continue
		}

		// case 3 — defer sync/EmitFilled until after s.mu is released (Update→EmitFilled
		// synchronously re-enters processFilledOrder which must not contend on s.mu).
		if openOrderID == 0 {
			order := activeOrder.GetOrder()
			s.logger.Infof("[Recover] found active order #%d is not in the open orders, will sync after unlock...", order.OrderID)
			case3Orders = append(case3Orders, order)
			continue
		}

		// case 4
		if activeOrderID != openOrderID {
			s.mu.Unlock()
			return fmt.Errorf("[Recover] there are two different orders in the same pin, can not recover")
		}

		// case 5
		// do nothing
	}

	s.logger.Infof("[Recover] twin orderbook after adding open orders\n%s", activeOrdersInTwinOrderBook.String())
	s.logger.Infof("[Recover] pins without twin orders: %+v", noTwinOrderPins)

	var pendingEmit []types.Order

	if len(noTwinOrderPins) != 0 {
		// Release s.mu around REST history queries; twin book is local.
		s.mu.Unlock()
		if err := s.recoverEmptyGridOnTwinOrderBook(ctx, activeOrdersInTwinOrderBook, historyService, s.orderQueryService); err != nil {
			s.logger.WithError(err).Error("[Recover] failed to recover empty grid")
			return err
		}
		s.mu.Lock()
		if s.gridStopped.Load() {
			s.mu.Unlock()
			s.logger.Info("[Recover] grid was closed during empty-grid recover, abort")
			return nil
		}

		s.logger.Infof("[Recover] twin orderbook after recovering no twin order on grid\n%s", activeOrdersInTwinOrderBook.String())

		if activeOrdersInTwinOrderBook.EmptyTwinOrderSize() > 0 {
			// Price outside [lower, upper] leaves one side empty by design; history cannot
			// rebuild those twins. Soft-skip hard fail so periodic recover does not spam
			// Slack — but still run repairMissingReverseOrders so pins that failed earlier
			// with -2019 can be filled once margin frees (AVAX/XRP/ENA above band, etc.).
			if s.session != nil && !s.LowerPrice.IsZero() && !s.UpperPrice.IsZero() {
				s.mu.Unlock()
				lastPrice, err := s.getLastTradePrice(ctx, s.session)
				s.mu.Lock()
				if err == nil && !lastPrice.IsZero() {
					if lastPrice.Compare(s.LowerPrice) < 0 || lastPrice.Compare(s.UpperPrice) > 0 {
						s.logger.Warnf(
							"[Recover] price %s outside band [%s, %s] with empty twin pins %+v; skip hard fail, try repair",
							lastPrice, s.LowerPrice, s.UpperPrice, noTwinOrderPins,
						)
						s.mu.Unlock()
						if errRepair := s.repairMissingReverseOrders(ctx); errRepair != nil {
							s.logger.WithError(errRepair).Warn("[Recover] repairMissingReverseOrders failed (outside band)")
						}
						return nil
					}
				}
			}

			// Profit gate: leave pins empty when the only reverse would close at a loss.
			stillRequired := 0
			for _, pin := range noTwinOrderPins {
				twinOrder := activeOrdersInTwinOrderBook.GetTwinOrder(pin)
				if twinOrder != nil && twinOrder.Exist() {
					continue
				}
				if s.twinPinReverseIsUnprofitableClose(pin) {
					base, avg := s.Position.GetBaseAndAverageCost()
					s.logger.Infof(
						"[Recover] leave pin %s empty: reverse would close at a loss after fee (base=%s avgCost=%s feeRate=%s)",
						pin.String(), base.String(), avg.String(), s.closingFeeRate().Percentage(),
					)
					continue
				}
				stillRequired++
			}
			if stillRequired > 0 {
				s.mu.Unlock()
				// Try margin re-fill before hard-failing; frees IM after other fills.
				if errRepair := s.repairMissingReverseOrders(ctx); errRepair != nil {
					s.logger.WithError(errRepair).Warn("[Recover] repairMissingReverseOrders failed before hard fail")
				}
				return fmt.Errorf("[Recover] there is still empty grid in twin orderbook")
			}
			s.logger.Info("[Recover] remaining empty twins are profit-gated; continue with recovered fills only")
		}

		for _, pin := range noTwinOrderPins {
			twinOrder := activeOrdersInTwinOrderBook.GetTwinOrder(pin)
			if twinOrder == nil || !twinOrder.Exist() {
				// Profit-gated empty pin — already logged above.
				continue
			}

			filledOrder := twinOrder.GetOrder()
			s.logger.Infof("[Recover] find filled order #%d (status: %s)", filledOrder.OrderID, filledOrder.Status)
			if filledOrder.Status != types.OrderStatusFilled {
				s.mu.Unlock()
				return fmt.Errorf("[Recover] should not get non-filled status, check it")
			}

			pendingEmit = append(pendingEmit, filledOrder)
		}
	}

	// Drop s.mu before any EmitFilled / syncActiveOrder path. Those callbacks call
	// processFilledOrder → takeOrder* which used to re-lock s.mu (deadlock).
	s.mu.Unlock()

	for _, order := range case3Orders {
		s.logger.Infof("[Recover] syncing active order #%d outside recover lock...", order.OrderID)
		isActiveOrderBookUpdated, err := syncActiveOrder(ctx, activeOrderBook, s.orderQueryService, order.OrderID, syncBefore)
		if err != nil {
			s.logger.WithError(err).Errorf("[Recover] unable to query order #%d", order.OrderID)
			continue
		}
		if !isActiveOrderBookUpdated {
			s.logger.Infof("[Recover] active order #%d is updated in 3 min, skip updating...", order.OrderID)
		}
	}

	if len(pendingEmit) > 0 {
		if s.gridStopped.Load() {
			s.logger.Info("[Recover] grid was closed before EmitFilled, abort")
			return nil
		}

		// EmitFilled places missing reverse orders; do not recount profit/Slack.
		s.recovering.Store(true)
		defer s.recovering.Store(false)

		for _, filledOrder := range pendingEmit {
			s.logger.Infof("[Recover] emit filled order %s", filledOrder)
			activeOrderBook.EmitFilled(filledOrder)
			time.Sleep(100 * time.Millisecond)
		}
	}

	// TODO: do not emit ready here, emit ready only once when opening grid or recovering grid after worker stopped
	// s.EmitGridReady()

	time.Sleep(2 * time.Second)

	// After recover, reverse submits may have been skipped (duplicated fill id) or
	// failed live with -2019. Re-arm empty twins so sells above last / buys below
	// last match the expected ladder.
	if err := s.repairMissingReverseOrders(ctx); err != nil {
		s.logger.WithError(err).Warn("[Recover] repairMissingReverseOrders failed")
	}

	debugGrid(s.logger, s.getGrid(), s.orderExecutor.ActiveMakerOrders())

	bbgo.Sync(ctx, s)

	return nil
}

// cancelUnprofitableClosingOrders cancels open buy/sell that would reduce the
// position at a loss. Recover must not keep losing covers ("平仓一定要盈利").
// openOrders may be a pre-fetched snapshot; nil triggers a fresh query.
func (s *Strategy) cancelUnprofitableClosingOrders(ctx context.Context, openOrders []types.Order) error {
	if s.Position == nil || s.Position.GetBase().IsZero() {
		return nil
	}
	if s.orderExecutor == nil || s.session == nil {
		return nil
	}

	var err error
	if openOrders == nil {
		openOrders, err = retry.QueryOpenOrdersUntilSuccessfulLite(ctx, s.session.Exchange, s.Symbol)
		if err != nil {
			return err
		}
	}
	if len(openOrders) == 0 {
		return nil
	}

	var toCancel []types.Order
	base, avg := s.Position.GetBaseAndAverageCost()
	for _, o := range openOrders {
		if !s.isClosingOrderSide(o.Side) {
			continue
		}
		if s.isProfitableCloseAt(o.Price) {
			continue
		}
		s.logger.Infof(
			"[Recover] cancel unprofitable closing %s #%d @ %s (base=%s avgCost=%s gross=%s fee=%s net=%s feeRate=%s)",
			o.Side, o.OrderID, o.Price.String(),
			base.String(), avg.String(),
			s.Position.UnrealizedProfit(o.Price).String(),
			s.estimatedCloseFee(o.Price).String(),
			s.netCloseProfitAt(o.Price).String(),
			s.closingFeeRate().Percentage(),
		)
		toCancel = append(toCancel, o)
	}
	if len(toCancel) == 0 {
		return nil
	}

	s.lockWriteOrders()
	defer s.unlockWriteOrders()
	return s.orderExecutor.GracefulCancel(ctx, toCancel...)
}

func (s *Strategy) recoverEmptyGridOnTwinOrderBook(
	ctx context.Context,
	twinOrderBook *TwinOrderBook,
	queryTradesService types.ExchangeTradeHistoryService,
	queryOrderService types.ExchangeOrderQueryService,
) error {
	if twinOrderBook.EmptyTwinOrderSize() == 0 {
		s.logger.Info("[Recover] no empty grid")
		return nil
	}

	existedOrders := twinOrderBook.SyncOrderMap()

	until := time.Now()
	since := until.Add(-1 * time.Hour)
	// hard limit for recover
	recoverSinceLimit := time.Date(2023, time.March, 10, 0, 0, 0, 0, time.UTC)

	if s.RecoverGridWithin != 0 && until.Add(-1*s.RecoverGridWithin).After(recoverSinceLimit) {
		recoverSinceLimit = until.Add(-1 * s.RecoverGridWithin)
	}

	for {
		if err := queryTradesToUpdateTwinOrderBook(ctx, s.Symbol, twinOrderBook, queryTradesService, queryOrderService, existedOrders, since, until, s.debugLog); err != nil {
			return errors.Wrapf(err, "[Recover] failed to query trades to update twin orderbook")
		}

		until = since
		since = until.Add(-6 * time.Hour)

		if twinOrderBook.EmptyTwinOrderSize() == 0 {
			s.logger.Infof("[Recover] stop querying trades because there is no empty twin order on twin orderbook")
			break
		}

		if s.GridProfitStats != nil && s.GridProfitStats.Since != nil && until.Before(*s.GridProfitStats.Since) {
			s.logger.Infof("[Recover] stop querying trades because the time range is out of the strategy's since (%s)", *s.GridProfitStats.Since)
			break
		}

		if until.Before(recoverSinceLimit) {
			s.logger.Infof("[Recover] stop querying trades because the time range is out of the limit (%s)", recoverSinceLimit)
			break
		}
	}

	return nil
}

func buildTwinOrderBook(pins []grid2types.Pin, orders []types.Order) (*TwinOrderBook, error) {
	book := newTwinOrderBook(pins)

	for _, order := range orders {
		if err := book.AddOrder(order, true); err != nil {
			// Skip orders that do not map onto the current grid pins (e.g. leftover
			// duplicates from older runs, or unexpected algo-order history entries).
			// Failing the whole recover on one bad order previously caused panic.
			log.WithError(err).Warnf("[Recover] skip order #%d @ %s when building twin orderbook", order.OrderID, order.Price)
			continue
		}
	}

	return book, nil
}

func syncActiveOrder(
	ctx context.Context, activeOrderBook *bbgo.ActiveOrderBook, orderQueryService types.ExchangeOrderQueryService,
	orderID uint64, syncBefore time.Time,
) (isOrderUpdated bool, err error) {
	isMax := exchange.IsMaxExchange(orderQueryService)

	updatedOrder, err := retry.QueryOrderUntilSuccessful(ctx, orderQueryService, types.OrderQuery{
		Symbol:  activeOrderBook.Symbol,
		OrderID: strconv.FormatUint(orderID, 10),
	})

	if err != nil {
		return isOrderUpdated, err
	}

	// maxapi.OrderStateFinalizing does not mean the fee is calculated
	// we should only consider order state done for MAX
	if isMax && updatedOrder.OriginalStatus != string(maxapi.OrderStateDone) {
		return isOrderUpdated, nil
	}

	// should only trigger order update when the updated time is old enough
	isOrderUpdated = updatedOrder.UpdateTime.Before(syncBefore)
	if isOrderUpdated {
		activeOrderBook.Update(*updatedOrder)
	}

	return isOrderUpdated, nil
}

func queryTradesToUpdateTwinOrderBook(
	ctx context.Context,
	symbol string,
	twinOrderBook *TwinOrderBook,
	queryTradesService types.ExchangeTradeHistoryService,
	queryOrderService types.ExchangeOrderQueryService,
	existedOrders *types.SyncOrderMap,
	since, until time.Time,
	logger func(format string, args ...interface{}),
) error {
	if twinOrderBook == nil {
		return fmt.Errorf("[Recover] twin orderbook should not be nil, please check it")
	}

	var fromTradeID uint64 = 0
	var limit int64 = 1000
	for {
		trades, err := retry.QueryTradesUntilSuccessful(ctx, queryTradesService, symbol, &types.TradeQueryOptions{
			StartTime:   &since,
			EndTime:     &until,
			LastTradeID: fromTradeID,
			Limit:       limit,
		})

		if err != nil {
			return errors.Wrapf(err, "[Recover] failed to query trades to recover the grid")
		}

		if logger != nil {
			logger("[Recover] QueryTrades from %s <-> %s (from: %d) return %d trades", since, until, fromTradeID, len(trades))
		}

		for _, trade := range trades {
			if trade.Time.After(until) {
				return nil
			}

			if logger != nil {
				logger("[Recover] " + trade.String())
			}

			if existedOrders.Exists(trade.OrderID) {
				// already queries, skip
				continue
			}
			order, err := retry.QueryOrderUntilSuccessful(ctx, queryOrderService, types.OrderQuery{
				Symbol:  trade.Symbol,
				OrderID: strconv.FormatUint(trade.OrderID, 10),
			})

			if err != nil {
				return errors.Wrapf(err, "[Recover] failed to query order by trade (trade id: %d, order id: %d)", trade.ID, trade.OrderID)
			}

			if logger != nil {
				logger("[Recover] " + order.String())
			}
			// avoid query this order again
			existedOrders.Add(*order)
			// add 1 to avoid duplicate
			fromTradeID = trade.ID + 1

			if order.Type == types.OrderTypeMarket || order.Price.IsZero() {
				if logger != nil {
					logger("[Recover] skip market order #%d", order.OrderID)
				}
				continue
			}

			if err := twinOrderBook.AddOrder(*order, true); err != nil {
				// Historical fills from a previous grid range must not abort recover.
				if logger != nil {
					logger("[Recover] skip order #%d not in twin orderbook pins: %v", order.OrderID, err)
				}
				continue
			}
		}

		// stop condition
		if int64(len(trades)) < limit {
			return nil
		}
	}
}
