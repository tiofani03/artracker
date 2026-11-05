package analytics

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sink defines the contract for persisting normalized analytics events and identity aliases.
type Sink interface {
	WriteBatch(ctx context.Context, rows []EventRow, aliases []IdentityAlias) error
}

type PgxAnalyticsSink struct {
	pool *pgxpool.Pool
}

func NewPgxAnalyticsSink(pool *pgxpool.Pool) *PgxAnalyticsSink {
	return &PgxAnalyticsSink{pool: pool}
}

func (s *PgxAnalyticsSink) WriteBatch(ctx context.Context, rows []EventRow, aliases []IdentityAlias) error {
	if s.pool == nil || (len(rows) == 0 && len(aliases) == 0) {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if len(rows) > 0 {
		ids := make([]uuid.UUID, len(rows))
		projectIDs := make([]int64, len(rows))
		sessionIDs := make([]string, len(rows))
		userIDs := make([]string, len(rows))
		deviceIDs := make([]string, len(rows))
		actorIDs := make([]string, len(rows))
		eventNames := make([]string, len(rows))
		properties := make([][]byte, len(rows))
		utmSources := make([]string, len(rows))
		utmMediums := make([]string, len(rows))
		utmCampaigns := make([]string, len(rows))
		browsers := make([]string, len(rows))
		oses := make([]string, len(rows))
		timestamps := make([]time.Time, len(rows))

		for i, r := range rows {
			ids[i] = r.ID
			projectIDs[i] = r.ProjectID
			sessionIDs[i] = r.SessionID
			userIDs[i] = r.UserID
			deviceIDs[i] = r.DeviceID
			actorIDs[i] = r.ActorID
			eventNames[i] = r.EventName
			properties[i] = r.Properties
			utmSources[i] = r.UTMSource
			utmMediums[i] = r.UTMMedium
			utmCampaigns[i] = r.UTMCampaign
			browsers[i] = r.Browser
			oses[i] = r.OS
			timestamps[i] = r.Timestamp
		}

		insertEventsSQL := `
			INSERT INTO events (
				id, project_id, session_id, user_id, device_id, actor_id,
				event_name, properties, utm_source, utm_medium, utm_campaign,
				browser, os, "timestamp"
			)
			SELECT * FROM UNNEST(
				$1::uuid[],
				$2::bigint[],
				$3::text[],
				$4::text[],
				$5::text[],
				$6::text[],
				$7::text[],
				$8::jsonb[],
				$9::text[],
				$10::text[],
				$11::text[],
				$12::text[],
				$13::text[],
				$14::timestamptz[]
			)
			ON CONFLICT (project_id, "timestamp", id) DO NOTHING;`

		if _, err := tx.Exec(ctx, insertEventsSQL,
			ids, projectIDs, sessionIDs, userIDs, deviceIDs, actorIDs,
			eventNames, properties, utmSources, utmMediums, utmCampaigns,
			browsers, oses, timestamps,
		); err != nil {
			return err
		}
	}

	if len(aliases) > 0 {
		// Dedup aliases inside current batch
		aliasMap := make(map[string]IdentityAlias)
		for _, a := range aliases {
			key := a.AnonID
			aliasMap[key] = a
		}

		pIDs := make([]int64, 0, len(aliasMap))
		anonIDs := make([]string, 0, len(aliasMap))
		userIDs := make([]string, 0, len(aliasMap))

		for _, a := range aliasMap {
			pIDs = append(pIDs, a.ProjectID)
			anonIDs = append(anonIDs, a.AnonID)
			userIDs = append(userIDs, a.UserID)
		}

		insertAliasesSQL := `
			INSERT INTO identity_aliases (project_id, anon_id, user_id, updated_at)
			SELECT p, a, u, now()
			FROM UNNEST($1::bigint[], $2::text[], $3::text[]) AS t(p, a, u)
			ON CONFLICT (project_id, anon_id)
			DO UPDATE SET user_id = EXCLUDED.user_id, updated_at = now();`

		if _, err := tx.Exec(ctx, insertAliasesSQL, pIDs, anonIDs, userIDs); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// MemoryAnalyticsSink stores events and aliases in memory for testing / SKIP_DB.
type MemoryAnalyticsSink struct {
	mu      sync.RWMutex
	rows    []EventRow
	aliases map[string]string // key: "projectID:anonID" -> "user_id"
}

func NewMemoryAnalyticsSink() *MemoryAnalyticsSink {
	return &MemoryAnalyticsSink{
		rows:    make([]EventRow, 0),
		aliases: make(map[string]string),
	}
}

func (m *MemoryAnalyticsSink) WriteBatch(_ context.Context, rows []EventRow, aliases []IdentityAlias) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.rows = append(m.rows, rows...)
	for _, a := range aliases {
		key := a.AnonID
		m.aliases[key] = a.UserID
	}
	return nil
}

func (m *MemoryAnalyticsSink) Rows() []EventRow {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copied := make([]EventRow, len(m.rows))
	copy(copied, m.rows)
	return copied
}

func (m *MemoryAnalyticsSink) Aliases() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make(map[string]string, len(m.aliases))
	for k, v := range m.aliases {
		res[k] = v
	}
	return res
}
