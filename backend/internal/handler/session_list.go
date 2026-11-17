package handler

import (
	"fmt"
	"strings"
	"time"
)

// sessionTelemetryWindow bounds how far back telemetry-derived sessions (sessions with no replay row) are looked up
// when the caller gives no explicit `from`.
const sessionTelemetryWindow = 7 * 24 * time.Hour

// sessionTelemetryCTE defines two CTEs, src and tel. tel has one row per session id seen in telemetry_events
// (excluding replay chunk uploads) or issue_events inside the window, with the same columns as replay_sessions.
// Placeholders: $1 = project id, $2 = window start. It is a shared fragment so a single-session lookup can
// derive the same aggregates.
const sessionTelemetryCTE = `
src AS (
	SELECT session_id, user_id, environment, event_timestamp AS ts, level, name, false AS crash
	FROM telemetry_events
	WHERE project_id = $1 AND session_id <> '' AND name <> 'replay_chunk' AND event_timestamp >= $2
	UNION ALL
	SELECT ie.session_id, ie.user_id, NULL::text, ie.created_at, 'fatal'::text, ''::text, true
	FROM issue_events ie JOIN issues i ON i.id = ie.issue_id
	WHERE i.project_id = $1 AND ie.session_id <> '' AND ie.created_at >= $2
), tel AS (
	SELECT
		session_id AS id,
		$1::bigint AS project_id,
		COALESCE(max(user_id) FILTER (WHERE user_id <> ''), '') AS user_id,
		COALESCE(max(environment), 'production') AS environment,
		min(ts) AS start_time,
		max(ts) AS end_time,
		(EXTRACT(EPOCH FROM (max(ts) - min(ts))) * 1000)::bigint AS duration_ms,
		0::int AS chunk_count,
		count(*)::bigint AS event_count,
		0::bigint AS total_size_bytes,
		COALESCE(bool_or(crash OR level IN ('error', 'fatal')), false) AS has_error,
		COALESCE(bool_or(name IN ('rage_click', 'rage_tap')), false) AS has_rage_click,
		COALESCE(bool_or(name = 'dead_click'), false) AS has_dead_click,
		''::text AS storage_prefix,
		min(ts) AS created_at,
		max(ts) AS updated_at
	FROM src
	GROUP BY session_id
)`

type sessionListFilter struct {
	HasError, HasRage, HasDead *bool
	UserID, UserPrefix         string
	Environment                string
	From, To                   *time.Time
}

// buildSessionListQuery returns the merged session list query: every replay_sessions row of the project, plus
// sessions derived from telemetry/crash events that have no replay row. Selected columns are the ReplaySessionItem
// columns in scan order, then has_replay, then full_count. All user input goes through placeholders.
func buildSessionListQuery(projectID int64, f sessionListFilter, now time.Time, limit, offset int) (string, []any) {
	win := now.Add(-sessionTelemetryWindow)
	if f.From != nil {
		win = *f.From
	}
	args := []any{projectID, win}

	var q strings.Builder
	q.WriteString("WITH" + sessionTelemetryCTE + `, merged AS (
	SELECT
		id, project_id, user_id, environment, start_time, end_time,
		duration_ms, chunk_count, event_count, total_size_bytes,
		has_error, has_rage_click, has_dead_click, storage_prefix, created_at, updated_at,
		(chunk_count > 0) AS has_replay
	FROM replay_sessions
	WHERE project_id = $1
	UNION ALL
	SELECT
		id, project_id, user_id, environment, start_time, end_time,
		duration_ms, chunk_count, event_count, total_size_bytes,
		has_error, has_rage_click, has_dead_click, storage_prefix, created_at, updated_at,
		false
	FROM tel
	WHERE NOT EXISTS (SELECT 1 FROM replay_sessions r WHERE r.project_id = $1 AND r.id = tel.id)
)
SELECT
	id, project_id, user_id, environment, start_time, end_time,
	duration_ms, chunk_count, event_count, total_size_bytes,
	has_error, has_rage_click, has_dead_click, storage_prefix, created_at, updated_at,
	has_replay, COUNT(*) OVER() AS full_count
FROM merged
WHERE true`)

	add := func(cond string, v any) {
		args = append(args, v)
		fmt.Fprintf(&q, " AND "+cond, len(args))
	}
	if f.HasError != nil {
		add("has_error = $%d", *f.HasError)
	}
	if f.HasRage != nil {
		add("has_rage_click = $%d", *f.HasRage)
	}
	if f.HasDead != nil {
		add("has_dead_click = $%d", *f.HasDead)
	}
	if f.UserID != "" {
		add("user_id = $%d", f.UserID)
	}
	// Case-insensitive "starts with". starts_with() takes the prefix literally, so '%' and '_' typed by the
	// user are not LIKE wildcards.
	if f.UserPrefix != "" {
		add("starts_with(lower(user_id), lower($%d))", f.UserPrefix)
	}
	if f.Environment != "" {
		add("environment = $%d", f.Environment)
	}
	if f.From != nil {
		add("start_time >= $%d", *f.From)
	}
	if f.To != nil {
		add("start_time <= $%d", *f.To)
	}

	args = append(args, limit, offset)
	fmt.Fprintf(&q, " ORDER BY start_time DESC, id LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	return q.String(), args
}
