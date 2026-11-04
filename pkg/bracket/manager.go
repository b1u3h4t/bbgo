package bracket

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

// Exchange is the subset of types.Exchange used by the bracket manager.
type Exchange interface {
	SubmitOrder(ctx context.Context, order types.SubmitOrder) (*types.Order, error)
	CancelOrders(ctx context.Context, orders ...types.Order) error
}

// OrderQuerier optionally reconciles order status on startup.
type OrderQuerier interface {
	QueryOrder(ctx context.Context, q types.OrderQuery) (*types.Order, error)
}

// MarketFinder resolves market meta for formatting prices/qty.
type MarketFinder func(symbol string) (types.Market, bool)

// AddRequest is the payload for creating a new bracket job.
type AddRequest struct {
	Symbol   string
	Side     types.SideType
	Quantity fixedpoint.Value
	Price    fixedpoint.Value // ignored when Market is true
	Market   bool
	TpPrice  fixedpoint.Value
	SlPrice  fixedpoint.Value // <= 0 disables SL
}

// Manager owns active bracket jobs and reacts to order updates.
type Manager struct {
	mu        sync.Mutex
	jobs      map[string]*Job
	byOrder   map[uint64]string // exchange order id -> job id
	attaching map[string]struct{}
	exchange  Exchange
	markets   MarketFinder
	statePath string
}

// NewManager creates an empty manager. Call Load before serving traffic.
func NewManager(ex Exchange, markets MarketFinder, statePath string) *Manager {
	return &Manager{
		jobs:      make(map[string]*Job),
		byOrder:   make(map[uint64]string),
		attaching: make(map[string]struct{}),
		exchange:  ex,
		markets:   markets,
		statePath: statePath,
	}
}

// List returns a snapshot of all jobs sorted by CreatedAt ascending.
func (m *Manager) List() []*Job {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		cp := *j
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, k int) bool {
		return out[i].CreatedAt.Before(out[k].CreatedAt)
	})
	return out
}

// Get returns a copy of a job by id.
func (m *Manager) Get(id string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, false
	}
	cp := *j
	return &cp, true
}

// Add submits the entry order and registers a pending_entry job.
func (m *Manager) Add(ctx context.Context, req AddRequest) (*Job, error) {
	if req.Symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if req.Side != types.SideTypeBuy && req.Side != types.SideTypeSell {
		return nil, fmt.Errorf("side must be BUY or SELL")
	}
	if req.Quantity.Sign() <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}
	if req.TpPrice.Sign() <= 0 {
		return nil, fmt.Errorf("tp price must be positive")
	}
	if !req.Market && req.Price.Sign() <= 0 {
		return nil, fmt.Errorf("price is required for limit entry")
	}

	market, ok := m.markets(req.Symbol)
	if !ok {
		return nil, fmt.Errorf("market %s not found", req.Symbol)
	}

	so := types.SubmitOrder{
		Symbol:   req.Symbol,
		Side:     req.Side,
		Quantity: req.Quantity,
		Market:   market,
	}
	if req.Market {
		so.Type = types.OrderTypeMarket
	} else {
		so.Type = types.OrderTypeLimit
		so.Price = req.Price
		so.TimeInForce = types.TimeInForceGTC
	}

	created, err := m.exchange.SubmitOrder(ctx, so)
	if err != nil {
		return nil, fmt.Errorf("submit entry: %w", err)
	}

	now := time.Now().UTC()
	job := &Job{
		ID:           uuid.NewString(),
		State:        StatePendingEntry,
		Symbol:       req.Symbol,
		Side:         req.Side,
		Quantity:     req.Quantity,
		EntryPrice:   req.Price,
		TpPrice:      req.TpPrice,
		SlPrice:      req.SlPrice,
		EntryOrderID: created.OrderID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	m.mu.Lock()
	m.jobs[job.ID] = job
	m.indexOrderLocked(created.OrderID, job.ID)
	m.mu.Unlock()

	if err := m.Persist(); err != nil {
		log.WithError(err).Warn("bracket: persist after add failed")
	}

	log.Infof("bracket: job %s entry submitted order=%d %s %s qty=%s",
		job.ID, created.OrderID, job.Side, job.Symbol, job.Quantity.String())

	// Market entries may already be filled in the submit response.
	if created.Status == types.OrderStatusFilled {
		m.OnOrderUpdate(ctx, *created)
	}

	out, _ := m.Get(job.ID)
	return out, nil
}

// Cancel cancels open entry/TP/SL orders for a job and marks it canceled.
func (m *Manager) Cancel(ctx context.Context, id string) (*Job, error) {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("job %s not found", id)
	}
	if !job.Active() {
		cp := *job
		m.mu.Unlock()
		return &cp, nil
	}
	toCancel := m.openOrdersLocked(job)
	m.mu.Unlock()

	if len(toCancel) > 0 {
		if err := m.exchange.CancelOrders(ctx, toCancel...); err != nil {
			log.WithError(err).Warnf("bracket: cancel orders for job %s", id)
		}
	}

	m.mu.Lock()
	if j, ok := m.jobs[id]; ok {
		j.State = StateCanceled
		j.UpdatedAt = time.Now().UTC()
		j.LastError = ""
	}
	m.mu.Unlock()

	if err := m.Persist(); err != nil {
		log.WithError(err).Warn("bracket: persist after cancel failed")
	}
	out, ok := m.Get(id)
	if !ok {
		return nil, fmt.Errorf("job %s not found", id)
	}
	return out, nil
}

// OnOrderUpdate handles a user-data-stream order update.
func (m *Manager) OnOrderUpdate(ctx context.Context, order types.Order) {
	m.mu.Lock()
	jobID, ok := m.byOrder[order.OrderID]
	if !ok {
		m.mu.Unlock()
		return
	}
	job, ok := m.jobs[jobID]
	if !ok || !job.Active() {
		m.mu.Unlock()
		return
	}

	switch job.State {
	case StatePendingEntry:
		m.handleEntryUpdate(ctx, job, order)
	case StatePendingExit:
		m.handleExitUpdate(ctx, job, order)
	default:
		m.mu.Unlock()
	}
}

// handleEntryUpdate must be called with m.mu held; it releases the lock.
func (m *Manager) handleEntryUpdate(ctx context.Context, job *Job, order types.Order) {
	if order.OrderID != job.EntryOrderID {
		m.mu.Unlock()
		return
	}

	switch order.Status {
	case types.OrderStatusFilled:
		job.ExecutedQuantity = order.ExecutedQuantity
		if job.ExecutedQuantity.Sign() <= 0 {
			job.ExecutedQuantity = order.Quantity
		}
		m.mu.Unlock()
		m.attachExits(ctx, job.ID)

	case types.OrderStatusCanceled, types.OrderStatusRejected, types.OrderStatusExpired:
		if order.ExecutedQuantity.Sign() > 0 {
			job.ExecutedQuantity = order.ExecutedQuantity
			m.mu.Unlock()
			m.attachExits(ctx, job.ID)
			return
		}
		job.State = StateCanceled
		job.UpdatedAt = time.Now().UTC()
		log.Infof("bracket: job %s entry canceled with zero fill", job.ID)
		m.mu.Unlock()
		m.persistAsync()

	default:
		m.mu.Unlock()
	}
}

func (m *Manager) attachExits(ctx context.Context, jobID string) {
	m.mu.Lock()
	job, ok := m.jobs[jobID]
	if !ok || job.State != StatePendingEntry {
		m.mu.Unlock()
		return
	}
	if _, busy := m.attaching[jobID]; busy {
		m.mu.Unlock()
		return
	}
	m.attaching[jobID] = struct{}{}
	symbol := job.Symbol
	qty := job.ExecutedQuantity
	if qty.Sign() <= 0 {
		qty = job.Quantity
	}
	exitSide := job.ExitSide()
	tpPrice := job.TpPrice
	slPrice := job.SlPrice
	wantSL := job.WantSL()
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.attaching, jobID)
		m.mu.Unlock()
	}()

	market, ok := m.markets(symbol)
	if !ok {
		m.setError(jobID, fmt.Sprintf("market %s not found", symbol))
		return
	}

	createdTP, err := m.exchange.SubmitOrder(ctx, types.SubmitOrder{
		Symbol:      symbol,
		Side:        exitSide,
		Type:        types.OrderTypeLimit,
		Price:       tpPrice,
		Quantity:    qty,
		TimeInForce: types.TimeInForceGTC,
		ReduceOnly:  true,
		Market:      market,
	})
	if err != nil {
		m.setError(jobID, fmt.Sprintf("submit TP: %v", err))
		log.WithError(err).Errorf("bracket: job %s submit TP failed", jobID)
		return
	}
	log.Infof("bracket: job %s TP placed order=%d @ %s", jobID, createdTP.OrderID, tpPrice.String())

	var slOrderID uint64
	if wantSL {
		createdSL, err := m.exchange.SubmitOrder(ctx, types.SubmitOrder{
			Symbol:     symbol,
			Side:       exitSide,
			Type:       types.OrderTypeStopMarket,
			StopPrice:  slPrice,
			Quantity:   qty,
			ReduceOnly: true,
			Market:     market,
		})
		if err != nil {
			m.setError(jobID, fmt.Sprintf("submit SL: %v", err))
			log.WithError(err).Errorf("bracket: job %s submit SL failed (TP already live)", jobID)
		} else {
			slOrderID = createdSL.OrderID
			log.Infof("bracket: job %s SL placed order=%d stop@ %s", jobID, slOrderID, slPrice.String())
		}
	}

	m.mu.Lock()
	job, ok = m.jobs[jobID]
	if !ok {
		m.mu.Unlock()
		return
	}
	// Only advance if still waiting for entry attach (avoid double attach).
	if job.State != StatePendingEntry {
		m.mu.Unlock()
		return
	}
	job.TpOrderID = createdTP.OrderID
	m.indexOrderLocked(createdTP.OrderID, job.ID)
	if slOrderID != 0 {
		job.SlOrderID = slOrderID
		m.indexOrderLocked(slOrderID, job.ID)
	}
	job.State = StatePendingExit
	job.UpdatedAt = time.Now().UTC()
	if job.LastError != "" && slOrderID != 0 {
		job.LastError = ""
	}
	m.mu.Unlock()
	m.persistAsync()
}

// handleExitUpdate must be called with m.mu held; it releases the lock.
func (m *Manager) handleExitUpdate(ctx context.Context, job *Job, order types.Order) {
	isTP := job.TpOrderID != 0 && order.OrderID == job.TpOrderID
	isSL := job.SlOrderID != 0 && order.OrderID == job.SlOrderID
	if !isTP && !isSL {
		m.mu.Unlock()
		return
	}

	if order.Status != types.OrderStatusFilled {
		if order.Status == types.OrderStatusCanceled || order.Status == types.OrderStatusExpired {
			if isTP {
				job.TpOrderID = 0
			}
			if isSL {
				job.SlOrderID = 0
			}
			if job.TpOrderID == 0 && job.SlOrderID == 0 {
				job.State = StateCanceled
				job.UpdatedAt = time.Now().UTC()
				m.mu.Unlock()
				m.persistAsync()
				return
			}
		}
		m.mu.Unlock()
		return
	}

	jobID := job.ID
	symbol := job.Symbol
	var otherID uint64
	if isTP {
		otherID = job.SlOrderID
		log.Infof("bracket: job %s TP filled, canceling SL if any", jobID)
	} else {
		otherID = job.TpOrderID
		log.Infof("bracket: job %s SL filled, canceling TP if any", jobID)
	}
	m.mu.Unlock()

	if otherID != 0 {
		other := types.Order{
			SubmitOrder: types.SubmitOrder{Symbol: symbol},
			OrderID:     otherID,
		}
		if err := m.exchange.CancelOrders(ctx, other); err != nil {
			log.WithError(err).Warnf("bracket: job %s cancel other exit %d", jobID, otherID)
		}
	}

	m.mu.Lock()
	if j, ok := m.jobs[jobID]; ok && j.Active() {
		j.State = StateDone
		j.UpdatedAt = time.Now().UTC()
		j.LastError = ""
	}
	m.mu.Unlock()
	m.persistAsync()
}

func (m *Manager) setError(jobID, msg string) {
	m.mu.Lock()
	if j, ok := m.jobs[jobID]; ok {
		j.LastError = msg
		j.UpdatedAt = time.Now().UTC()
	}
	m.mu.Unlock()
	m.persistAsync()
}

func (m *Manager) openOrdersLocked(job *Job) []types.Order {
	var out []types.Order
	add := func(id uint64) {
		if id == 0 {
			return
		}
		out = append(out, types.Order{
			SubmitOrder: types.SubmitOrder{Symbol: job.Symbol},
			OrderID:     id,
		})
	}
	switch job.State {
	case StatePendingEntry:
		add(job.EntryOrderID)
	case StatePendingExit:
		add(job.TpOrderID)
		add(job.SlOrderID)
	}
	return out
}

func (m *Manager) indexOrderLocked(orderID uint64, jobID string) {
	if orderID == 0 {
		return
	}
	m.byOrder[orderID] = jobID
}

func (m *Manager) persistAsync() {
	if err := m.Persist(); err != nil {
		log.WithError(err).Warn("bracket: async persist failed")
	}
}

// Persist writes jobs to the state file atomically.
func (m *Manager) Persist() error {
	if m.statePath == "" {
		return nil
	}

	m.mu.Lock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		cp := *j
		jobs = append(jobs, &cp)
	}
	m.mu.Unlock()

	sort.Slice(jobs, func(i, k int) bool {
		return jobs[i].CreatedAt.Before(jobs[k].CreatedAt)
	})

	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(m.statePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp := m.statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.statePath)
}

// Load reads jobs from the state file and rebuilds indexes.
func (m *Manager) Load() error {
	if m.statePath == "" {
		return nil
	}
	data, err := os.ReadFile(m.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}

	var jobs []*Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs = make(map[string]*Job, len(jobs))
	m.byOrder = make(map[uint64]string)
	for _, j := range jobs {
		if j == nil || j.ID == "" {
			continue
		}
		m.jobs[j.ID] = j
		m.indexOrderLocked(j.EntryOrderID, j.ID)
		m.indexOrderLocked(j.TpOrderID, j.ID)
		m.indexOrderLocked(j.SlOrderID, j.ID)
	}
	log.Infof("bracket: loaded %d jobs from %s", len(m.jobs), m.statePath)
	return nil
}

// Reconcile queries exchange for active jobs and advances state if needed.
func (m *Manager) Reconcile(ctx context.Context) {
	querier, ok := m.exchange.(OrderQuerier)
	if !ok {
		log.Warn("bracket: exchange does not support QueryOrder; skip reconcile")
		return
	}

	m.mu.Lock()
	ids := make([]string, 0)
	for _, j := range m.jobs {
		if j.Active() {
			ids = append(ids, j.ID)
		}
	}
	m.mu.Unlock()

	for _, id := range ids {
		m.reconcileJob(ctx, querier, id)
	}
}

func (m *Manager) reconcileJob(ctx context.Context, querier OrderQuerier, id string) {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok || !job.Active() {
		m.mu.Unlock()
		return
	}
	state := job.State
	entryID := job.EntryOrderID
	tpID := job.TpOrderID
	slID := job.SlOrderID
	symbol := job.Symbol
	m.mu.Unlock()

	query := func(oid uint64) (*types.Order, error) {
		if oid == 0 {
			return nil, nil
		}
		return querier.QueryOrder(ctx, types.OrderQuery{
			Symbol:  symbol,
			OrderID: strconv.FormatUint(oid, 10),
		})
	}

	switch state {
	case StatePendingEntry:
		o, err := query(entryID)
		if err != nil {
			log.WithError(err).Warnf("bracket: reconcile entry %d", entryID)
			return
		}
		if o != nil {
			m.OnOrderUpdate(ctx, *o)
		}
	case StatePendingExit:
		if tpID != 0 {
			if o, err := query(tpID); err != nil {
				log.WithError(err).Warnf("bracket: reconcile TP %d", tpID)
			} else if o != nil {
				m.OnOrderUpdate(ctx, *o)
			}
		}
		m.mu.Lock()
		still := m.jobs[id]
		alive := still != nil && still.State == StatePendingExit
		m.mu.Unlock()
		if alive && slID != 0 {
			if o, err := query(slID); err != nil {
				log.WithError(err).Warnf("bracket: reconcile SL %d", slID)
			} else if o != nil {
				m.OnOrderUpdate(ctx, *o)
			}
		}
	}
}
