package ingest

import (
	"context"
	"testing"
	"time"
)

func TestWorkerPoolFlushOnBatchSize(t *testing.T) {
	sink := NewMemoryStorageSink()
	metrics := NewMetrics(100, 2)
	// Flush at batch size 5 or 5 seconds
	pool := NewWorkerPool(100, 2, 5, 5000, sink, nil, metrics)
	defer pool.Shutdown(context.Background())

	// Enqueue a job with 5 items
	items := make([]RawItem, 5)
	for i := 0; i < 5; i++ {
		items[i] = RawItem{
			Type: "event",
			Name: "test_event",
		}
	}

	job := BatchJob{
		ProjectID:  1,
		ReceivedAt: time.Now(),
		Envelope:   IngestEnvelope{Items: items},
	}

	if !pool.Enqueue(job) {
		t.Fatal("enqueue failed")
	}

	// Give a few milliseconds for workers to process and flush
	time.Sleep(50 * time.Millisecond)

	if sink.Count() != 5 {
		t.Fatalf("expected 5 flushed items, got %d", sink.Count())
	}
}

func TestWorkerPoolFlushOnInterval(t *testing.T) {
	sink := NewMemoryStorageSink()
	metrics := NewMetrics(100, 1)
	// Flush at 50ms interval or batch size 100
	pool := NewWorkerPool(100, 1, 100, 50, sink, nil, metrics)
	defer pool.Shutdown(context.Background())

	job := BatchJob{
		ProjectID:  1,
		ReceivedAt: time.Now(),
		Envelope: IngestEnvelope{
			Items: []RawItem{{Type: "event", Name: "interval_event"}},
		},
	}

	pool.Enqueue(job)

	// Wait for interval tick (100ms > 50ms)
	time.Sleep(100 * time.Millisecond)

	if sink.Count() != 1 {
		t.Fatalf("expected 1 item flushed on ticker, got %d", sink.Count())
	}
}

func TestWorkerPoolBackpressureWhenFull(t *testing.T) {
	sink := NewMemoryStorageSink()
	metrics := NewMetrics(2, 0) // 0 workers so queue stays full
	pool := &WorkerPool{
		queue:         make(chan BatchJob, 2),
		sink:          sink,
		sanitizer:     NewSanitizer(),
		metrics:       metrics,
		batchSize:     10,
		flushInterval: time.Second,
		numWorkers:    0,
	}

	job := BatchJob{ProjectID: 1, ReceivedAt: time.Now()}
	if !pool.Enqueue(job) {
		t.Fatal("first job should fit in queue")
	}
	if !pool.Enqueue(job) {
		t.Fatal("second job should fit in queue")
	}
	// Third job should fail (queue cap=2)
	if pool.Enqueue(job) {
		t.Fatal("third job should be rejected due to full buffer")
	}
}

func TestWorkerPoolGracefulDrain(t *testing.T) {
	sink := NewMemoryStorageSink()
	metrics := NewMetrics(100, 1)
	// High interval so it only flushes on shutdown
	pool := NewWorkerPool(100, 1, 1000, 60000, sink, nil, metrics)

	job := BatchJob{
		ProjectID:  1,
		ReceivedAt: time.Now(),
		Envelope: IngestEnvelope{
			Items: []RawItem{
				{Type: "event", Name: "drain_1"},
				{Type: "event", Name: "drain_2"},
			},
		},
	}
	pool.Enqueue(job)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	if sink.Count() != 2 {
		t.Fatalf("expected 2 items flushed during graceful drain, got %d", sink.Count())
	}
}

func validPerfAttrs() []byte {
	return []byte(`{"v":1,"platform":"android","interval_ms":1000,"samples":[{"t":1000,"cpu":10},{"t":2000,"cpu":20}]}`)
}

func TestWorkerRoutesPerfItemsToPerfSink(t *testing.T) {
	sink := NewMemoryStorageSink()
	perfSink := NewMemoryPerfSink()
	metrics := NewMetrics(100, 1)
	pool := NewWorkerPool(100, 1, 100, 60000, sink, perfSink, metrics)

	job := BatchJob{
		ProjectID:  3,
		ReceivedAt: time.Now(),
		Envelope: IngestEnvelope{Items: []RawItem{
			{Type: "event", Name: "tap", SessionID: "ses_a"},
			{Type: PerfItemType, Name: "perf.samples", SessionID: "ses_a", Attributes: validPerfAttrs()},
		}},
	}
	if !pool.Enqueue(job) {
		t.Fatal("enqueue failed")
	}
	if err := pool.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	rows := sink.Rows()
	if len(rows) != 1 || rows[0].Kind != "event" {
		t.Fatalf("telemetry sink should hold only the event, got %+v", rows)
	}
	perf := perfSink.Rows()
	if len(perf) != 1 || len(perf[0].Samples) != 2 || perf[0].ProjectID != 3 || perf[0].SessionID != "ses_a" {
		t.Fatalf("perf sink wrong: %+v", perf)
	}
	if got := pool.Stats().DroppedItems; got != 0 {
		t.Fatalf("dropped items = %d, want 0", got)
	}
}

func TestWorkerDropsInvalidPerfItem(t *testing.T) {
	sink := NewMemoryStorageSink()
	perfSink := NewMemoryPerfSink()
	metrics := NewMetrics(100, 1)
	pool := NewWorkerPool(100, 1, 100, 60000, sink, perfSink, metrics)

	job := BatchJob{
		ProjectID:  3,
		ReceivedAt: time.Now(),
		Envelope: IngestEnvelope{Items: []RawItem{
			{Type: PerfItemType, Name: "perf.samples", SessionID: "ses_a", Attributes: []byte(`{"v":2,"interval_ms":1000,"samples":[{"t":1}]}`)},
		}},
	}
	pool.Enqueue(job)
	if err := pool.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if len(perfSink.Rows()) != 0 || sink.Count() != 0 {
		t.Fatalf("nothing should be stored: perf=%d telemetry=%d", len(perfSink.Rows()), sink.Count())
	}
	if got := pool.Stats().DroppedItems; got != 1 {
		t.Fatalf("dropped items = %d, want 1", got)
	}
}

func TestWorkerDiscardsPerfItemsWithoutPerfSink(t *testing.T) {
	sink := NewMemoryStorageSink()
	pool := NewWorkerPool(100, 1, 100, 60000, sink, nil, NewMetrics(100, 1))
	pool.Enqueue(BatchJob{
		ProjectID:  3,
		ReceivedAt: time.Now(),
		Envelope: IngestEnvelope{Items: []RawItem{
			{Type: PerfItemType, SessionID: "ses_a", Attributes: validPerfAttrs()},
		}},
	})
	if err := pool.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if sink.Count() != 0 {
		t.Fatalf("perf must never reach telemetry sink, got %d", sink.Count())
	}
	if got := pool.Stats().DroppedItems; got != 1 {
		t.Fatalf("dropped items = %d, want 1", got)
	}
}
