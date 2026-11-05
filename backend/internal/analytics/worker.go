package analytics

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type WorkerPool struct {
	queue         chan BatchJob
	sink          Sink
	enricher      *Enricher
	metrics       *Metrics
	batchSize     int
	flushInterval time.Duration
	numWorkers    int
	wg            sync.WaitGroup
	stopOnce      sync.Once
	closed        bool
	mu            sync.Mutex
}

func NewWorkerPool(
	queueSize, numWorkers, batchSize, flushIntervalMs int,
	sink Sink,
	enricher *Enricher,
	metrics *Metrics,
) *WorkerPool {
	if queueSize <= 0 {
		queueSize = 2048
	}
	if numWorkers <= 0 {
		numWorkers = 4
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	if flushIntervalMs <= 0 {
		flushIntervalMs = 250
	}

	wp := &WorkerPool{
		queue:         make(chan BatchJob, queueSize),
		sink:          sink,
		enricher:      enricher,
		metrics:       metrics,
		batchSize:     batchSize,
		flushInterval: time.Duration(flushIntervalMs) * time.Millisecond,
		numWorkers:    numWorkers,
	}

	wp.start()
	return wp
}

func (wp *WorkerPool) start() {
	wp.wg.Add(wp.numWorkers)
	for i := 0; i < wp.numWorkers; i++ {
		go wp.workerLoop(i)
	}
}

func (wp *WorkerPool) workerLoop(workerID int) {
	defer wp.wg.Done()

	ticker := time.NewTicker(wp.flushInterval)
	defer ticker.Stop()

	eventRows := make([]EventRow, 0, wp.batchSize)
	aliasRows := make([]IdentityAlias, 0)

	flush := func() {
		if len(eventRows) == 0 && len(aliasRows) == 0 {
			return
		}
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := wp.sink.WriteBatch(ctx, eventRows, aliasRows)
		cancel()

		dur := time.Since(start)
		wp.metrics.SetLastFlushLatency(dur.Milliseconds())

		if err != nil {
			slog.Error("analytics flush failed", "worker", workerID, "rows", len(eventRows), "err", err)
			wp.metrics.IncDroppedBatches()
		} else {
			wp.metrics.AddEventsFlushed(len(eventRows))
			wp.metrics.IncBatchesFlushed()
		}

		eventRows = eventRows[:0]
		aliasRows = aliasRows[:0]
	}

	for {
		select {
		case job, ok := <-wp.queue:
			if !ok {
				flush()
				return
			}

			normRows, aliases, skipped := wp.enricher.ProcessBatch(job)
			wp.metrics.AddDuplicatesSkipped(skipped)

			eventRows = append(eventRows, normRows...)
			aliasRows = append(aliasRows, aliases...)

			if len(eventRows) >= wp.batchSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}

// Enqueue performs non-blocking push to the analytics queue.
// Returns false if buffer is full (triggering 503 backpressure).
func (wp *WorkerPool) Enqueue(job BatchJob) bool {
	wp.mu.Lock()
	if wp.closed {
		wp.mu.Unlock()
		return false
	}
	wp.mu.Unlock()

	select {
	case wp.queue <- job:
		return true
	default:
		return false
	}
}

func (wp *WorkerPool) QueueDepth() int {
	return len(wp.queue)
}

func (wp *WorkerPool) Stats() StatsSnapshot {
	return wp.metrics.Snapshot(len(wp.queue))
}

func (wp *WorkerPool) Shutdown(ctx context.Context) error {
	wp.stopOnce.Do(func() {
		wp.mu.Lock()
		wp.closed = true
		wp.mu.Unlock()
		close(wp.queue)
	})

	done := make(chan struct{})
	go func() {
		wp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
