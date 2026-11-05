package analytics

import (
	"sync/atomic"
)

type Metrics struct {
	requestsAccepted   atomic.Uint64
	requestsRejected   atomic.Uint64
	eventsReceived     atomic.Uint64
	eventsFlushed      atomic.Uint64
	batchesFlushed     atomic.Uint64
	droppedBatches     atomic.Uint64
	duplicatesSkipped  atomic.Uint64
	lastFlushLatencyMs atomic.Int64
	queueCapacity      int
	workers            int
}

func NewMetrics(queueCapacity, workers int) *Metrics {
	return &Metrics{
		queueCapacity: queueCapacity,
		workers:       workers,
	}
}

func (m *Metrics) IncRequestsAccepted()   { m.requestsAccepted.Add(1) }
func (m *Metrics) IncRequestsRejected()   { m.requestsRejected.Add(1) }
func (m *Metrics) AddEventsReceived(n int) { m.eventsReceived.Add(uint64(n)) }
func (m *Metrics) AddEventsFlushed(n int)  { m.eventsFlushed.Add(uint64(n)) }
func (m *Metrics) IncBatchesFlushed()     { m.batchesFlushed.Add(1) }
func (m *Metrics) IncDroppedBatches()     { m.droppedBatches.Add(1) }
func (m *Metrics) AddDuplicatesSkipped(n int) { m.duplicatesSkipped.Add(uint64(n)) }
func (m *Metrics) SetLastFlushLatency(ms int64) { m.lastFlushLatencyMs.Store(ms) }

type StatsSnapshot struct {
	QueueDepth         int     `json:"queue_depth"`
	QueueCapacity      int     `json:"queue_capacity"`
	UtilizationPct     float64 `json:"utilization_pct"`
	Workers            int     `json:"workers"`
	RequestsAccepted   uint64  `json:"requests_accepted"`
	RequestsRejected   uint64  `json:"requests_rejected"`
	EventsReceived     uint64  `json:"events_received"`
	EventsFlushed      uint64  `json:"events_flushed"`
	BatchesFlushed     uint64  `json:"batches_flushed"`
	DroppedBatches     uint64  `json:"dropped_batches"`
	DuplicatesSkipped  uint64  `json:"duplicates_skipped"`
	LastFlushLatencyMs int64   `json:"last_flush_latency_ms"`
}

func (m *Metrics) Snapshot(queueDepth int) StatsSnapshot {
	utilization := 0.0
	if m.queueCapacity > 0 {
		utilization = (float64(queueDepth) / float64(m.queueCapacity)) * 100.0
	}
	return StatsSnapshot{
		QueueDepth:         queueDepth,
		QueueCapacity:      m.queueCapacity,
		UtilizationPct:     utilization,
		Workers:            m.workers,
		RequestsAccepted:   m.requestsAccepted.Load(),
		RequestsRejected:   m.requestsRejected.Load(),
		EventsReceived:     m.eventsReceived.Load(),
		EventsFlushed:      m.eventsFlushed.Load(),
		BatchesFlushed:     m.batchesFlushed.Load(),
		DroppedBatches:     m.droppedBatches.Load(),
		DuplicatesSkipped:  m.duplicatesSkipped.Load(),
		LastFlushLatencyMs: m.lastFlushLatencyMs.Load(),
	}
}
