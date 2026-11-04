package ingest

import (
	"sync"
	"sync/atomic"
	"time"
)

// IngestStats is a read-only snapshot of current ingest pipeline performance.
type IngestStats struct {
	Status             string  `json:"status"` // "healthy", "pressured", "overloaded"
	QueueDepth         int     `json:"queue_depth"`
	QueueCapacity      int     `json:"queue_capacity"`
	UtilizationPct     float64 `json:"utilization_pct"`
	Workers            int     `json:"workers"`
	RequestsPerSec     float64 `json:"requests_per_sec"`
	RequestsAccepted   uint64  `json:"requests_accepted"`
	RequestsRejected   uint64  `json:"requests_rejected"`
	EventsReceived     uint64  `json:"events_received_total"`
	EventsFlushed      uint64  `json:"events_flushed_total"`
	BatchesFlushed     uint64  `json:"batches_flushed_total"`
	DroppedBatches     uint64  `json:"dropped_batches_total"`
	DroppedItems       uint64  `json:"dropped_invalid_items_total"`
	SanitizedItems     uint64  `json:"sanitized_items_total"`
	LastFlushLatencyMs int64   `json:"last_flush_latency_ms"`
}

// Metrics tracks real-time ingestion counters and sliding-window throughput.
type Metrics struct {
	queueCapacity int
	workers       int

	requestsAccepted atomic.Uint64
	requestsRejected atomic.Uint64
	eventsReceived   atomic.Uint64
	eventsFlushed    atomic.Uint64
	batchesFlushed   atomic.Uint64
	droppedBatches   atomic.Uint64
	droppedItems     atomic.Uint64
	sanitizedItems   atomic.Uint64
	lastFlushLatency atomic.Int64

	// Sliding window for RPS calculation
	mu          sync.Mutex
	windowSlots [10]uint64
	currentSlot int
	lastSlotSec int64
	stopCh      chan struct{}
}

func NewMetrics(queueCapacity, workers int) *Metrics {
	m := &Metrics{
		queueCapacity: queueCapacity,
		workers:       workers,
		lastSlotSec:   time.Now().Unix(),
		stopCh:        make(chan struct{}),
	}
	return m
}

func (m *Metrics) IncAcceptedRequests() {
	m.requestsAccepted.Add(1)
	m.recordRPSHit()
}

func (m *Metrics) IncRejectedRequests() {
	m.requestsRejected.Add(1)
	m.recordRPSHit()
}

func (m *Metrics) AddEventsReceived(n int) {
	if n > 0 {
		m.eventsReceived.Add(uint64(n))
	}
}

func (m *Metrics) AddEventsFlushed(n int) {
	if n > 0 {
		m.eventsFlushed.Add(uint64(n))
	}
}

func (m *Metrics) IncBatchesFlushed() {
	m.batchesFlushed.Add(1)
}

func (m *Metrics) IncDroppedBatches() {
	m.droppedBatches.Add(1)
}

func (m *Metrics) AddDroppedItems(n int) {
	if n > 0 {
		m.droppedItems.Add(uint64(n))
	}
}

func (m *Metrics) AddSanitizedItems(n int) {
	if n > 0 {
		m.sanitizedItems.Add(uint64(n))
	}
}

func (m *Metrics) SetLastFlushLatency(ms int64) {
	m.lastFlushLatency.Store(ms)
}

func (m *Metrics) recordRPSHit() {
	sec := time.Now().Unix()
	m.mu.Lock()
	defer m.mu.Unlock()

	diff := sec - m.lastSlotSec
	if diff > 0 {
		if diff >= 10 {
			m.windowSlots = [10]uint64{}
			m.currentSlot = 0
		} else {
			for i := int64(1); i <= diff; i++ {
				m.currentSlot = (m.currentSlot + 1) % 10
				m.windowSlots[m.currentSlot] = 0
			}
		}
		m.lastSlotSec = sec
	}
	m.windowSlots[m.currentSlot]++
}

func (m *Metrics) calculateRPS() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	var total uint64
	for _, count := range m.windowSlots {
		total += count
	}
	return float64(total) / 10.0
}

// Snapshot generates an IngestStats snapshot based on current queue depth.
func (m *Metrics) Snapshot(currentQueueDepth int) IngestStats {
	var utilPct float64
	if m.queueCapacity > 0 {
		utilPct = float64(currentQueueDepth) / float64(m.queueCapacity) * 100.0
	}

	status := "healthy"
	if utilPct >= 95.0 {
		status = "overloaded"
	} else if utilPct >= 75.0 {
		status = "pressured"
	}

	return IngestStats{
		Status:             status,
		QueueDepth:         currentQueueDepth,
		QueueCapacity:      m.queueCapacity,
		UtilizationPct:     utilPct,
		Workers:            m.workers,
		RequestsPerSec:     m.calculateRPS(),
		RequestsAccepted:   m.requestsAccepted.Load(),
		RequestsRejected:   m.requestsRejected.Load(),
		EventsReceived:     m.eventsReceived.Load(),
		EventsFlushed:      m.eventsFlushed.Load(),
		BatchesFlushed:     m.batchesFlushed.Load(),
		DroppedBatches:     m.droppedBatches.Load(),
		DroppedItems:       m.droppedItems.Load(),
		SanitizedItems:     m.sanitizedItems.Load(),
		LastFlushLatencyMs: m.lastFlushLatency.Load(),
	}
}
