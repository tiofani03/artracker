package ingest

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds configuration parameters for the Ingest subsystem.
type Config struct {
	Workers         int
	QueueSize       int
	BatchSize       int
	FlushIntervalMs int
	ProjectRPS      float64
	ProjectEventsPS float64
	KeyCacheTTLSec  int
}

// Service is the unified manager for authentication caching, rate limiting,
// decompression, async queueing, and metrics.
type Service struct {
	keyCache       KeyResolver
	rateLimiter    *ProjectRateLimiter
	decompress     *Decompressor
	workerPool     *WorkerPool
	metrics        *Metrics
	sink           StorageSink
	issueProcessor IssueProcessor
}

// NewService creates and initializes the complete Ingest pipeline.
func NewService(cfg Config, pool *pgxpool.Pool, customSink ...StorageSink) *Service {
	metrics := NewMetrics(cfg.QueueSize, cfg.Workers)
	keyCache := NewMemoryKeyCache(pool, time.Duration(cfg.KeyCacheTTLSec)*time.Second)
	rateLimiter := NewProjectRateLimiter(cfg.ProjectRPS, cfg.ProjectEventsPS)
	decompress := NewDecompressor()

	var sink StorageSink
	if len(customSink) > 0 && customSink[0] != nil {
		sink = customSink[0]
	} else if pool != nil {
		sink = NewPgxStorageSink(pool)
	} else {
		sink = NewMemoryStorageSink()
	}

	workerPool := NewWorkerPool(
		cfg.QueueSize,
		cfg.Workers,
		cfg.BatchSize,
		cfg.FlushIntervalMs,
		sink,
		metrics,
	)

	return &Service{
		keyCache:    keyCache,
		rateLimiter: rateLimiter,
		decompress:  decompress,
		workerPool:  workerPool,
		metrics:     metrics,
		sink:        sink,
	}
}

func (s *Service) KeyCache() KeyResolver {
	return s.keyCache
}

func (s *Service) RateLimiter() *ProjectRateLimiter {
	return s.rateLimiter
}

func (s *Service) Decompressor() *Decompressor {
	return s.decompress
}

func (s *Service) WorkerPool() *WorkerPool {
	return s.workerPool
}

func (s *Service) Metrics() *Metrics {
	return s.metrics
}

func (s *Service) Sink() StorageSink {
	return s.sink
}

// WithIssueProcessor attaches the error-tracking processor to the worker pool
// so flushed batches are grouped into issues. Returns the service for chaining.
func (s *Service) WithIssueProcessor(p IssueProcessor) *Service {
	s.issueProcessor = p
	if s.workerPool != nil {
		s.workerPool.issueProcessor = p
	}
	return s
}

// Shutdown gracefully flushes all queued items to storage.
func (s *Service) Shutdown(ctx context.Context) error {
	s.rateLimiter.Stop()
	return s.workerPool.Shutdown(ctx)
}
