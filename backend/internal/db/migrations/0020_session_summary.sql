-- Session list summary: session_contexts carries what the session list shows for sessions that have no replay
-- row, so the list never aggregates telemetry_events per request. Ingest keeps these columns current
-- (people.Aggregate / people.Store.Apply). Every statement is idempotent: re-running the file recomputes the
-- summary from the raw rows.
ALTER TABLE session_contexts
    ADD COLUMN IF NOT EXISTS environment    TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS event_count    BIGINT  NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS has_rage_click BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS has_dead_click BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS session_contexts_project_last_seen_idx ON session_contexts (project_id, last_seen DESC);

-- Backfill from every stored telemetry row (replay chunk uploads excluded) and crash occurrence.
INSERT INTO session_contexts (project_id, session_id, user_id, first_seen, last_seen, has_error,
                              environment, event_count, has_rage_click, has_dead_click)
SELECT src.project_id,
       src.session_id,
       COALESCE((array_agg(src.user_id ORDER BY src.ts) FILTER (WHERE src.user_id <> ''))[1], ''),
       min(src.ts),
       max(src.ts),
       bool_or(src.crash OR src.level IN ('error', 'fatal')),
       COALESCE((array_agg(src.environment ORDER BY src.ts) FILTER (WHERE src.environment <> ''))[1], ''),
       count(*),
       bool_or(src.name IN ('rage_click', 'rage_tap')),
       bool_or(src.name = 'dead_click')
FROM (
    SELECT project_id, session_id, user_id, environment, event_timestamp AS ts, level, name, false AS crash
    FROM telemetry_events
    WHERE session_id <> '' AND name <> 'replay_chunk'
    UNION ALL
    SELECT i.project_id, ie.session_id, ie.user_id, COALESCE(ie.environment, ''), ie.created_at, 'fatal', '', true
    FROM issue_events ie
    JOIN issues i ON i.id = ie.issue_id
    WHERE ie.session_id <> ''
) src
JOIN projects p ON p.id = src.project_id
GROUP BY src.project_id, src.session_id
ON CONFLICT (project_id, session_id) DO UPDATE SET
    user_id        = CASE WHEN session_contexts.user_id = '' THEN EXCLUDED.user_id ELSE session_contexts.user_id END,
    first_seen     = LEAST(session_contexts.first_seen, EXCLUDED.first_seen),
    last_seen      = GREATEST(session_contexts.last_seen, EXCLUDED.last_seen),
    has_error      = session_contexts.has_error OR EXCLUDED.has_error,
    environment    = EXCLUDED.environment,
    event_count    = EXCLUDED.event_count,
    has_rage_click = EXCLUDED.has_rage_click,
    has_dead_click = EXCLUDED.has_dead_click,
    updated_at     = now();
