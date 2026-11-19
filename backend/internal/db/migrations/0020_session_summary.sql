-- Session list summary: session_contexts carries what the session list shows for sessions that have no replay
-- row, so the list never aggregates telemetry_events per request. Ingest keeps these columns current
-- (people.Aggregate / people.Store.Apply); 0021 backfills them. Schema only here, so the ACCESS EXCLUSIVE lock
-- of the ALTER is released before the backfill scans telemetry.
ALTER TABLE session_contexts
    ADD COLUMN IF NOT EXISTS environment    TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS event_count    BIGINT  NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS has_rage_click BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS has_dead_click BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS session_contexts_project_last_seen_idx ON session_contexts (project_id, last_seen DESC);
