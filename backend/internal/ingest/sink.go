package ingest

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StorageSink defines the contract for persisting normalized telemetry batches.
type StorageSink interface {
	WriteBatch(ctx context.Context, rows []TelemetryRow) error
}

var telemetryColumns = []string{
	"id",
	"project_id",
	"kind",
	"name",
	"level",
	"message",
	"session_id",
	"user_id",
	"environment",
	"release_version",
	"client_timestamp",
	"event_timestamp",
	"received_at",
	"attributes",
	"sdk_meta",
}

// PgxStorageSink writes batches directly to PostgreSQL using binary pgx.CopyFrom.
type PgxStorageSink struct {
	pool *pgxpool.Pool
}

func NewPgxStorageSink(pool *pgxpool.Pool) *PgxStorageSink {
	return &PgxStorageSink{pool: pool}
}

func (s *PgxStorageSink) WriteBatch(ctx context.Context, rows []TelemetryRow) error {
	if s.pool == nil || len(rows) == 0 {
		return nil
	}

	_, err := s.pool.CopyFrom(
		ctx,
		pgx.Identifier{"telemetry_events"},
		telemetryColumns,
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := &rows[i]
			return []any{
				r.ID,
				r.ProjectID,
				r.Kind,
				r.Name,
				r.Level,
				r.Message,
				r.SessionID,
				r.UserID,
				r.Environment,
				r.ReleaseVersion,
				r.ClientTimestamp,
				r.EventTimestamp,
				r.ReceivedAt,
				r.Attributes,
				r.SDKMeta,
			}, nil
		}),
	)
	return err
}

// MemoryStorageSink stores flushed rows in memory for tests / SKIP_DB mode.
type MemoryStorageSink struct {
	mu   sync.Mutex
	rows []TelemetryRow
}

func NewMemoryStorageSink() *MemoryStorageSink {
	return &MemoryStorageSink{
		rows: make([]TelemetryRow, 0),
	}
}

func (m *MemoryStorageSink) WriteBatch(_ context.Context, rows []TelemetryRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, rows...)
	return nil
}

func (m *MemoryStorageSink) Rows() []TelemetryRow {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]TelemetryRow, len(m.rows))
	copy(copied, m.rows)
	return copied
}

func (m *MemoryStorageSink) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}
