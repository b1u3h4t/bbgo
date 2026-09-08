package bbgo

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

type Delta fixedpoint.Value

func (d Delta) Side() types.SideType {
	side := types.SideTypeBuy

	if fixedpoint.Value(d).IsZero() {
		side = types.SideTypeNone
	}

	if fixedpoint.Value(d).Sign() < 0 {
		side = types.SideTypeSell
	}

	return side
}

func (d Delta) Quantity() fixedpoint.Value {
	return fixedpoint.Value(d).Abs()
}

var positionExposurePendingMetrics = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "bbgo_position_exposure_pending",
		Help: "the pending position exposure",
	}, []string{"strategy_type", "strategy_id", "exchange", "symbol"},
)

var positionExposureNetMetrics = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "bbgo_position_exposure_net",
		Help: "the net position exposure",
	}, []string{"strategy_type", "strategy_id", "exchange", "symbol"},
)

var positionExposureUncoveredMetrics = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "bbgo_position_exposure_uncovered",
		Help: "the uncovered position exposure",
	}, []string{"strategy_type", "strategy_id", "exchange", "symbol"},
)

var positionExposureSizeMetrics = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "bbgo_position_exposure_size",
		Help:    "the size of position exposure",
		Buckets: prometheus.LinearBuckets(0, 10, 10),
	}, []string{"strategy_type", "strategy_id", "exchange", "symbol"},
)

//go:generate callbackgen -type PositionExposure
type PositionExposure struct {
	symbol string

	// mu protects net and pending as a consistent pair. Close updates both
	// fields; readers like GetUncovered/IsClosed must not observe a torn state
	// where only one field has been updated (that can falsely look uncovered
	// and trigger a duplicate hedge).
	mu sync.Mutex

	// net = net position
	// pending = covered position
	net, pending fixedpoint.Value

	openCallbacks  []func(d fixedpoint.Value)
	coverCallbacks []func(d fixedpoint.Value)
	closeCallbacks []func(d fixedpoint.Value)

	labels prometheus.Labels

	positionExposurePendingMetrics,
	positionExposureNetMetrics,
	positionExposureUncoveredMetrics prometheus.Gauge
	positionExposureSizeMetrics prometheus.Observer

	logger logrus.FieldLogger
}

func NewPositionExposure(symbol string) *PositionExposure {
	return &PositionExposure{
		symbol: symbol,
		logger: logrus.WithField("symbol", symbol),
	}
}

func (m *PositionExposure) SetLogger(logger logrus.FieldLogger) {
	m.logger = logger
}

func (m *PositionExposure) GetSymbol() string {
	return m.symbol
}

func (m *PositionExposure) GetNet() fixedpoint.Value {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.net
}

func (m *PositionExposure) GetPending() fixedpoint.Value {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pending
}

func (m *PositionExposure) Open(delta fixedpoint.Value) {
	m.mu.Lock()
	m.net = m.net.Add(delta)
	net, pending := m.net, m.pending
	m.mu.Unlock()

	m.logger.Infof(
		"%s opened:%f netPosition:%f coveredPosition: %f",
		m.symbol,
		delta.Float64(),
		net.Float64(),
		pending.Float64(),
	)

	m.EmitOpen(delta)
}

func (m *PositionExposure) Uncover(delta fixedpoint.Value) {
	delta = delta.Neg()

	m.mu.Lock()
	m.pending = m.pending.Add(delta)
	net, pending := m.net, m.pending
	m.mu.Unlock()

	m.logger.Infof(
		"%s uncovered:%f netPosition:%f coveredPosition: %f",
		m.symbol,
		delta.Float64(),
		net.Float64(),
		pending.Float64(),
	)

	m.EmitCover(delta)
}

func (m *PositionExposure) Cover(delta fixedpoint.Value) {
	m.mu.Lock()
	m.pending = m.pending.Add(delta)
	net, pending := m.net, m.pending
	m.mu.Unlock()

	m.logger.Infof(
		"%s covered:%f netPosition:%f coveredPosition: %f",
		m.symbol,
		delta.Float64(),
		net.Float64(),
		pending.Float64(),
	)

	m.EmitCover(delta)
}

func (m *PositionExposure) Close(delta fixedpoint.Value) {
	m.mu.Lock()
	m.pending = m.pending.Add(delta)
	m.net = m.net.Add(delta)
	net, pending := m.net, m.pending
	m.mu.Unlock()

	m.logger.Infof(
		"%s closed:%f netPosition:%f coveredPosition: %f",
		m.symbol,
		delta.Float64(),
		net.Float64(),
		pending.Float64(),
	)

	m.EmitClose(delta)
}

func (m *PositionExposure) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.net.IsZero() && m.pending.IsZero()
}

func (m *PositionExposure) String() string {
	m.mu.Lock()
	net, pending := m.net, m.pending
	uncovered := net.Sub(pending)
	m.mu.Unlock()

	return fmt.Sprintf("PositionExposure<%s> net:%s pending:%s uncovered:%s",
		m.symbol,
		net.String(),
		pending.String(),
		uncovered.String(),
	)
}

func (m *PositionExposure) GetUncovered() fixedpoint.Value {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.net.Sub(m.pending)
}

func (m *PositionExposure) SetMetricsLabels(strategyType, strategyID, exchange, symbol string) {
	m.labels = prometheus.Labels{
		"strategy_type": strategyType,
		"strategy_id":   strategyID,
		"exchange":      exchange,
		"symbol":        symbol,
	}

	m.positionExposurePendingMetrics = positionExposurePendingMetrics.With(m.labels)
	m.positionExposureNetMetrics = positionExposureNetMetrics.With(m.labels)
	m.positionExposureUncoveredMetrics = positionExposureUncoveredMetrics.With(m.labels)
	m.positionExposureSizeMetrics = positionExposureSizeMetrics.With(m.labels)
}

func (m *PositionExposure) updateMetrics() {
	if m.positionExposurePendingMetrics == nil {
		return
	}

	m.mu.Lock()
	net, pending := m.net, m.pending
	uncovered := net.Sub(pending)
	m.mu.Unlock()

	m.positionExposurePendingMetrics.Set(pending.Float64())
	m.positionExposureNetMetrics.Set(net.Float64())
	m.positionExposureUncoveredMetrics.Set(uncovered.Float64())
	m.positionExposureSizeMetrics.Observe(net.Float64())
}
