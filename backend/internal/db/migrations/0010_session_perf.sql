-- Profiling samples (CPU / memory / FPS ...) per session, one row per SDK perf item.
-- Kept out of telemetry_events so logs, issues and analytics queries never see them.
CREATE TABLE IF NOT EXISTS session_perf_batches (
    id          BIGSERIAL   PRIMARY KEY,
    project_id  BIGINT      NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    session_id  TEXT        NOT NULL,
    platform    TEXT        NOT NULL DEFAULT '',
    start_ts    TIMESTAMPTZ NOT NULL,
    end_ts      TIMESTAMPTZ NOT NULL,
    interval_ms INT         NOT NULL,
    device      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    samples     JSONB       NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_session_perf_batches_session
    ON session_perf_batches (project_id, session_id, start_ts);
