package ingest

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PerfSink persists validated perf batches.
type PerfSink interface {
	WritePerf(ctx context.Context, rows []PerfBatchRow) error
}

// PgxPerfSink writes perf batches into session_perf_batches.
type PgxPerfSink struct {
	pool *pgxpool.Pool
}

func NewPgxPerfSink(pool *pgxpool.Pool) *PgxPerfSink {
	return &PgxPerfSink{pool: pool}
}

const insertPerfSQL = `
	INSERT INTO session_perf_batches
		(project_id, session_id, platform, start_ts, end_ts, interval_ms, device, samples, received_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

func (s *PgxPerfSink) WritePerf(ctx context.Context, rows []PerfBatchRow) error {
	if s.pool == nil || len(rows) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for i := range rows {
		r := &rows[i]
		device, err := json.Marshal(r.Device)
		if err != nil {
			return err
		}
		samples, err := json.Marshal(r.Samples)
		if err != nil {
			return err
		}
		batch.Queue(insertPerfSQL, r.ProjectID, r.SessionID, r.Platform, r.StartTS, r.EndTS,
			r.IntervalMs, device, samples, r.ReceivedAt)
	}
	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range rows {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// MemoryPerfSink keeps perf batches in memory for tests / SKIP_DB mode.
type MemoryPerfSink struct {
	mu   sync.Mutex
	rows []PerfBatchRow
}

func NewMemoryPerfSink() *MemoryPerfSink {
	return &MemoryPerfSink{}
}

func (m *MemoryPerfSink) WritePerf(_ context.Context, rows []PerfBatchRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, rows...)
	return nil
}

func (m *MemoryPerfSink) Rows() []PerfBatchRow {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PerfBatchRow, len(m.rows))
	copy(out, m.rows)
	return out
}
