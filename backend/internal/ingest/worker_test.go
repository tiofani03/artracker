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
	pool := NewWorkerPool(100, 2, 5, 5000, sink, metrics)
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
	pool := NewWorkerPool(100, 1, 100, 50, sink, metrics)
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
	pool := NewWorkerPool(100, 1, 1000, 60000, sink, metrics)

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
