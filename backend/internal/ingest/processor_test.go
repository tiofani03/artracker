package ingest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type recordingProcessor struct {
	mu   sync.Mutex
	rows int
	err  error
}

func (p *recordingProcessor) ProcessRows(_ context.Context, rows []TelemetryRow) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rows += len(rows)
	return p.err
}

func (p *recordingProcessor) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rows
}

func runOneBatch(t *testing.T, pool *WorkerPool) {
	t.Helper()
	pool.Enqueue(BatchJob{ProjectID: 1, ReceivedAt: time.Now(), Envelope: IngestEnvelope{
		Items: []RawItem{{Type: "event", Name: "a"}, {Type: "event", Name: "b"}},
	}})
	if err := pool.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerRunsAllProcessors(t *testing.T) {
	first, second := &recordingProcessor{}, &recordingProcessor{}
	pool := NewWorkerPool(10, 1, 100, 60000, NewMemoryStorageSink(), nil, NewMetrics(10, 1), first)
	pool.AddRowProcessor(second)
	runOneBatch(t, pool)
	if first.count() != 2 || second.count() != 2 {
		t.Fatalf("both processors must see 2 rows, got %d and %d", first.count(), second.count())
	}
}

func TestWorkerProcessorErrorIsolated(t *testing.T) {
	failing, ok := &recordingProcessor{err: errors.New("boom")}, &recordingProcessor{}
	sink := NewMemoryStorageSink()
	pool := NewWorkerPool(10, 1, 100, 60000, sink, nil, NewMetrics(10, 1), failing, ok)
	runOneBatch(t, pool)
	if ok.count() != 2 {
		t.Fatalf("second processor must still run, saw %d rows", ok.count())
	}
	if sink.Count() != 2 {
		t.Fatalf("flush must still store the rows, got %d", sink.Count())
	}
}

func TestServiceWithIssueProcessorStillWorks(t *testing.T) {
	p := &recordingProcessor{}
	pool := NewWorkerPool(10, 1, 100, 60000, NewMemoryStorageSink(), nil, NewMetrics(10, 1))
	s := &Service{workerPool: pool}
	s.WithIssueProcessor(p).AddRowProcessor(nil) // nil is ignored
	runOneBatch(t, pool)
	if p.count() != 2 {
		t.Fatalf("issue processor must run, saw %d rows", p.count())
	}
}
