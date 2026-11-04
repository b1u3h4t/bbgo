package bracket

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

type fakeExchange struct {
	mu      sync.Mutex
	nextID  uint64
	orders  map[uint64]*types.Order
	submits []types.SubmitOrder
	cancels []uint64
}

func newFakeExchange() *fakeExchange {
	return &fakeExchange{
		nextID: 1000,
		orders: make(map[uint64]*types.Order),
	}
}

func (f *fakeExchange) SubmitOrder(_ context.Context, order types.SubmitOrder) (*types.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := f.nextID
	f.submits = append(f.submits, order)
	o := &types.Order{
		SubmitOrder:      order,
		OrderID:          id,
		Status:           types.OrderStatusNew,
		ExecutedQuantity: fixedpoint.Zero,
	}
	if order.Type == types.OrderTypeMarket {
		o.Status = types.OrderStatusFilled
		o.ExecutedQuantity = order.Quantity
	}
	f.orders[id] = o
	cp := *o
	return &cp, nil
}

func (f *fakeExchange) CancelOrders(_ context.Context, orders ...types.Order) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range orders {
		f.cancels = append(f.cancels, o.OrderID)
		if existing, ok := f.orders[o.OrderID]; ok {
			existing.Status = types.OrderStatusCanceled
		}
	}
	return nil
}

func (f *fakeExchange) QueryOrder(_ context.Context, q types.OrderQuery) (*types.Order, error) {
	id, err := strconv.ParseUint(q.OrderID, 10, 64)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.orders[id]
	if !ok {
		return nil, fmt.Errorf("order %d not found", id)
	}
	cp := *o
	return &cp, nil
}

func (f *fakeExchange) fill(id uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.orders[id]
	if !ok {
		return
	}
	o.Status = types.OrderStatusFilled
	o.ExecutedQuantity = o.Quantity
}

func (f *fakeExchange) submittedTypes() []types.OrderType {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]types.OrderType, len(f.submits))
	for i, s := range f.submits {
		out[i] = s.Type
	}
	return out
}

func testMarket(_ string) (types.Market, bool) {
	return types.Market{
		Symbol:          "ZECUSDT",
		PricePrecision:  2,
		VolumePrecision: 3,
		StepSize:        fixedpoint.NewFromFloat(0.001),
		TickSize:        fixedpoint.NewFromFloat(0.01),
		MinQuantity:     fixedpoint.NewFromFloat(0.001),
	}, true
}

func TestAddAndAttachTPOnly(t *testing.T) {
	ex := newFakeExchange()
	m := NewManager(ex, testMarket, "")

	job, err := m.Add(context.Background(), AddRequest{
		Symbol:   "ZECUSDT",
		Side:     types.SideTypeBuy,
		Quantity: fixedpoint.NewFromFloat(8),
		Price:    fixedpoint.NewFromFloat(1245),
		TpPrice:  fixedpoint.NewFromFloat(1276.13),
		SlPrice:  fixedpoint.Zero,
	})
	require.NoError(t, err)
	assert.Equal(t, StatePendingEntry, job.State)
	assert.NotZero(t, job.EntryOrderID)

	entryID := job.EntryOrderID
	m.OnOrderUpdate(context.Background(), types.Order{
		SubmitOrder: types.SubmitOrder{
			Symbol:   "ZECUSDT",
			Side:     types.SideTypeBuy,
			Quantity: fixedpoint.NewFromFloat(8),
		},
		OrderID:          entryID,
		Status:           types.OrderStatusFilled,
		ExecutedQuantity: fixedpoint.NewFromFloat(8),
	})

	updated, ok := m.Get(job.ID)
	require.True(t, ok)
	assert.Equal(t, StatePendingExit, updated.State)
	assert.NotZero(t, updated.TpOrderID)
	assert.Zero(t, updated.SlOrderID)

	typesSubmitted := ex.submittedTypes()
	require.Len(t, typesSubmitted, 2) // entry + TP
	assert.Equal(t, types.OrderTypeLimit, typesSubmitted[0])
	assert.Equal(t, types.OrderTypeLimit, typesSubmitted[1])

	ex.mu.Lock()
	tp := ex.submits[1]
	ex.mu.Unlock()
	assert.True(t, tp.ReduceOnly)
	assert.Equal(t, types.SideTypeSell, tp.Side)
	assert.Equal(t, "1276.13", tp.Price.String())
}

func TestAttachTPAndSL(t *testing.T) {
	ex := newFakeExchange()
	m := NewManager(ex, testMarket, "")

	job, err := m.Add(context.Background(), AddRequest{
		Symbol:   "ZECUSDT",
		Side:     types.SideTypeBuy,
		Quantity: fixedpoint.NewFromFloat(8),
		Price:    fixedpoint.NewFromFloat(1245),
		TpPrice:  fixedpoint.NewFromFloat(1276.13),
		SlPrice:  fixedpoint.NewFromFloat(1200),
	})
	require.NoError(t, err)

	m.OnOrderUpdate(context.Background(), types.Order{
		OrderID:          job.EntryOrderID,
		Status:           types.OrderStatusFilled,
		ExecutedQuantity: fixedpoint.NewFromFloat(8),
		SubmitOrder:      types.SubmitOrder{Quantity: fixedpoint.NewFromFloat(8)},
	})

	updated, ok := m.Get(job.ID)
	require.True(t, ok)
	assert.Equal(t, StatePendingExit, updated.State)
	assert.NotZero(t, updated.TpOrderID)
	assert.NotZero(t, updated.SlOrderID)

	typesSubmitted := ex.submittedTypes()
	require.Len(t, typesSubmitted, 3)
	assert.Equal(t, types.OrderTypeStopMarket, typesSubmitted[2])
	ex.mu.Lock()
	assert.True(t, ex.submits[2].ReduceOnly)
	assert.Equal(t, "1200", ex.submits[2].StopPrice.String())
	ex.mu.Unlock()
}

func TestMutualCancelOnTPFill(t *testing.T) {
	ex := newFakeExchange()
	m := NewManager(ex, testMarket, "")

	job, err := m.Add(context.Background(), AddRequest{
		Symbol:   "ZECUSDT",
		Side:     types.SideTypeBuy,
		Quantity: fixedpoint.NewFromFloat(8),
		Price:    fixedpoint.NewFromFloat(1245),
		TpPrice:  fixedpoint.NewFromFloat(1276.13),
		SlPrice:  fixedpoint.NewFromFloat(1200),
	})
	require.NoError(t, err)

	m.OnOrderUpdate(context.Background(), types.Order{
		OrderID:          job.EntryOrderID,
		Status:           types.OrderStatusFilled,
		ExecutedQuantity: fixedpoint.NewFromFloat(8),
		SubmitOrder:      types.SubmitOrder{Quantity: fixedpoint.NewFromFloat(8)},
	})

	updated, _ := m.Get(job.ID)
	m.OnOrderUpdate(context.Background(), types.Order{
		OrderID: updated.TpOrderID,
		Status:  types.OrderStatusFilled,
		SubmitOrder: types.SubmitOrder{
			Symbol: "ZECUSDT",
		},
	})

	done, ok := m.Get(job.ID)
	require.True(t, ok)
	assert.Equal(t, StateDone, done.State)
	ex.mu.Lock()
	require.Contains(t, ex.cancels, updated.SlOrderID)
	ex.mu.Unlock()
}

func TestEntryCancelZeroFill(t *testing.T) {
	ex := newFakeExchange()
	m := NewManager(ex, testMarket, "")

	job, err := m.Add(context.Background(), AddRequest{
		Symbol:   "ZECUSDT",
		Side:     types.SideTypeBuy,
		Quantity: fixedpoint.NewFromFloat(8),
		Price:    fixedpoint.NewFromFloat(1245),
		TpPrice:  fixedpoint.NewFromFloat(1276.13),
	})
	require.NoError(t, err)

	m.OnOrderUpdate(context.Background(), types.Order{
		OrderID:          job.EntryOrderID,
		Status:           types.OrderStatusCanceled,
		ExecutedQuantity: fixedpoint.Zero,
	})

	updated, ok := m.Get(job.ID)
	require.True(t, ok)
	assert.Equal(t, StateCanceled, updated.State)
	assert.Len(t, ex.submittedTypes(), 1) // only entry
}

func TestPersistAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brackets.json")
	ex := newFakeExchange()
	m := NewManager(ex, testMarket, path)

	job, err := m.Add(context.Background(), AddRequest{
		Symbol:   "ZECUSDT",
		Side:     types.SideTypeBuy,
		Quantity: fixedpoint.NewFromFloat(8),
		Price:    fixedpoint.NewFromFloat(1245),
		TpPrice:  fixedpoint.NewFromFloat(1276.13),
	})
	require.NoError(t, err)
	require.NoError(t, m.Persist())

	m2 := NewManager(ex, testMarket, path)
	require.NoError(t, m2.Load())
	loaded, ok := m2.Get(job.ID)
	require.True(t, ok)
	assert.Equal(t, job.EntryOrderID, loaded.EntryOrderID)
	assert.Equal(t, StatePendingEntry, loaded.State)
}

func TestWantSL(t *testing.T) {
	assert.False(t, (&Job{SlPrice: fixedpoint.Zero}).WantSL())
	assert.False(t, (&Job{SlPrice: fixedpoint.NewFromFloat(-1)}).WantSL())
	assert.True(t, (&Job{SlPrice: fixedpoint.NewFromFloat(0.01)}).WantSL())
}

func TestReconcileAttachesTPWhenStreamMissedFill(t *testing.T) {
	ex := newFakeExchange()
	m := NewManager(ex, testMarket, "")

	job, err := m.Add(context.Background(), AddRequest{
		Symbol:   "ZECUSDT",
		Side:     types.SideTypeBuy,
		Quantity: fixedpoint.NewFromFloat(8),
		Price:    fixedpoint.NewFromFloat(1245),
		TpPrice:  fixedpoint.NewFromFloat(1276.13),
	})
	require.NoError(t, err)
	ex.fill(job.EntryOrderID)

	m.Reconcile(context.Background())

	updated, ok := m.Get(job.ID)
	require.True(t, ok)
	assert.Equal(t, StatePendingExit, updated.State)
	assert.NotZero(t, updated.TpOrderID)
	assert.Zero(t, updated.SlOrderID)
	require.Len(t, ex.submittedTypes(), 2)
	assert.Equal(t, types.OrderTypeLimit, ex.submittedTypes()[1])

	m.Reconcile(context.Background())
	require.Len(t, ex.submittedTypes(), 2)
}

func TestReconcileLeavesOpenEntryUntouched(t *testing.T) {
	ex := newFakeExchange()
	m := NewManager(ex, testMarket, "")

	job, err := m.Add(context.Background(), AddRequest{
		Symbol:   "AVAXUSDT",
		Side:     types.SideTypeBuy,
		Quantity: fixedpoint.NewFromFloat(400),
		Price:    fixedpoint.NewFromFloat(8.36),
		TpPrice:  fixedpoint.NewFromFloat(8.57),
	})
	require.NoError(t, err)

	m.Reconcile(context.Background())

	updated, ok := m.Get(job.ID)
	require.True(t, ok)
	assert.Equal(t, StatePendingEntry, updated.State)
	assert.Zero(t, updated.TpOrderID)
	require.Len(t, ex.submittedTypes(), 1)
}
