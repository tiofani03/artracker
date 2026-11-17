package ingest

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type WorkerPool struct {
	queue         chan BatchJob
	sink          StorageSink
	perfSink      PerfSink
	sanitizer     *Sanitizer
	metrics       *Metrics
	processors    atomic.Pointer[[]RowProcessor]
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
	sink StorageSink,
	perfSink PerfSink,
	metrics *Metrics,
	processors ...IssueProcessor,
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
		perfSink:      perfSink,
		sanitizer:     NewSanitizer(),
		metrics:       metrics,
		batchSize:     batchSize,
		flushInterval: time.Duration(flushIntervalMs) * time.Millisecond,
		numWorkers:    numWorkers,
	}
	for _, p := range processors {
		wp.AddRowProcessor(p)
	}

	wp.start()
	return wp
}

// AddRowProcessor registers a processor that runs after every successful flush. Safe while workers run.
func (wp *WorkerPool) AddRowProcessor(p RowProcessor) {
	if p == nil {
		return
	}
	wp.mu.Lock()
	defer wp.mu.Unlock()
	var next []RowProcessor
	if cur := wp.processors.Load(); cur != nil {
		next = append(next, (*cur)...)
	}
	next = append(next, p)
	wp.processors.Store(&next)
}

// runProcessors gives each processor its own timeout so one slow or failing processor cannot starve the others.
func (wp *WorkerPool) runProcessors(workerID int, rows []TelemetryRow) {
	procs := wp.processors.Load()
	if procs == nil {
		return
	}
	for i, p := range *procs {
		pctx, pcancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := p.ProcessRows(pctx, rows); err != nil {
			slog.Error("row processor failed", "worker", workerID, "processor", i, "err", err)
		}
		pcancel()
	}
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

	rows := make([]TelemetryRow, 0, wp.batchSize)
	perfRows := make([]PerfBatchRow, 0, 16)

	flushPerf := func() {
		if len(perfRows) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := wp.perfSink.WritePerf(ctx, perfRows)
		cancel()
		if err != nil {
			slog.Error("perf flush failed", "worker", workerID, "rows", len(perfRows), "err", err)
			wp.metrics.IncDroppedBatches()
		}
		perfRows = perfRows[:0]
	}

	flush := func() {
		flushPerf()
		if len(rows) == 0 {
			return
		}
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := wp.sink.WriteBatch(ctx, rows)
		cancel()

		dur := time.Since(start)
		wp.metrics.SetLastFlushLatency(dur.Milliseconds())

		if err != nil {
			slog.Error("ingest flush failed", "worker", workerID, "rows", len(rows), "err", err)
			wp.metrics.IncDroppedBatches()
		} else {
			wp.metrics.AddEventsFlushed(len(rows))
			wp.metrics.IncBatchesFlushed()
			wp.runProcessors(workerID, rows)
		}

		rows = rows[:0]
	}

	for {
		select {
		case job, ok := <-wp.queue:
			if !ok {
				// Queue closed: drain remaining buffer before stopping
				flush()
				return
			}

			normRows, dropped, sanitized := wp.sanitizer.SanitizeAndNormalize(job)
			wp.metrics.AddDroppedItems(dropped)
			wp.metrics.AddSanitizedItems(sanitized)

			rows = append(rows, normRows...)
			perfRows = append(perfRows, wp.collectPerf(job)...)
			if len(rows) >= wp.batchSize || len(perfRows) >= wp.batchSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}

// collectPerf extracts and validates the perf items of a job. Invalid items (and every perf item when no
// perf sink is configured) count as dropped; perf items never reach the telemetry sink.
func (wp *WorkerPool) collectPerf(job BatchJob) []PerfBatchRow {
	var out []PerfBatchRow
	dropped := 0
	for _, item := range job.Envelope.Items {
		if strings.ToLower(strings.TrimSpace(item.Type)) != PerfItemType {
			continue
		}
		if wp.perfSink == nil {
			dropped++
			continue
		}
		row, err := ParsePerfItem(job.ProjectID, job.ReceivedAt, item)
		if err != nil {
			dropped++
			continue
		}
		out = append(out, row)
	}
	wp.metrics.AddDroppedItems(dropped)
	return out
}

// Enqueue performs non-blocking push to the ingest channel.
// Returns true if enqueued; returns false if buffer is 100% full (triggering 503 backpressure).
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
		// Queue full!
		return false
	}
}

// QueueDepth returns current items waiting in the channel.
func (wp *WorkerPool) QueueDepth() int {
	return len(wp.queue)
}

// QueueCapacity returns total size of the channel buffer.
func (wp *WorkerPool) QueueCapacity() int {
	return cap(wp.queue)
}

// Stats returns a current snapshot of ingest pipeline statistics.
func (wp *WorkerPool) Stats() IngestStats {
	return wp.metrics.Snapshot(len(wp.queue))
}

// Shutdown initiates a graceful drain of the worker pool.
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
