package ingest

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppStartSink persists validated app starts.
type AppStartSink interface {
	WriteAppStarts(ctx context.Context, rows []AppStartRow) error
}

// PgxAppStartSink writes app starts into app_starts; a repeated (project_id, start_id) is ignored.
type PgxAppStartSink struct {
	pool *pgxpool.Pool
}

func NewPgxAppStartSink(pool *pgxpool.Pool) *PgxAppStartSink {
	return &PgxAppStartSink{pool: pool}
}

const insertAppStartSQL = `
	INSERT INTO app_starts
		(project_id, start_id, session_id, platform, environment, type, duration_ms, phases, first_screen,
		 app_version, build, os_version, device_model, ram_mb, trampoline, profiled, started_at, received_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	ON CONFLICT (project_id, start_id) DO NOTHING`

func (s *PgxAppStartSink) WriteAppStarts(ctx context.Context, rows []AppStartRow) error {
	if s.pool == nil || len(rows) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for i := range rows {
		r := &rows[i]
		phases, err := json.Marshal(r.Phases)
		if err != nil {
			return err
		}
		batch.Queue(insertAppStartSQL, r.ProjectID, r.StartID, r.SessionID, r.Platform, r.Environment, r.Type,
			r.DurationMs, phases, r.FirstScreen, r.AppVersion, r.Build, r.OSVersion, r.DeviceModel, r.RAMMB,
			r.Trampoline, r.Profiled, r.StartedAt, r.ReceivedAt)
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

// MemoryAppStartSink keeps app starts in memory for tests / SKIP_DB mode.
type MemoryAppStartSink struct {
	mu   sync.Mutex
	rows []AppStartRow
}

func NewMemoryAppStartSink() *MemoryAppStartSink {
	return &MemoryAppStartSink{}
}

func (m *MemoryAppStartSink) WriteAppStarts(_ context.Context, rows []AppStartRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, rows...)
	return nil
}

func (m *MemoryAppStartSink) Rows() []AppStartRow {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]AppStartRow, len(m.rows))
	copy(out, m.rows)
	return out
}
