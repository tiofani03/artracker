-- User flow: one row per stay on a screen within a session, extracted by the heatmap worker.
CREATE TABLE IF NOT EXISTS screen_views (
    project_id  BIGINT      NOT NULL,
    session_id  TEXT        NOT NULL,
    seq         INT         NOT NULL,
    screen      TEXT        NOT NULL,
    entered_at  TIMESTAMPTZ NOT NULL,
    duration_ms INT         NOT NULL,
    is_entry    BOOLEAN     NOT NULL DEFAULT false,
    is_exit     BOOLEAN     NOT NULL DEFAULT false,
    platform    TEXT        NOT NULL DEFAULT '',
    app_version TEXT        NOT NULL DEFAULT '',
    source      TEXT        NOT NULL DEFAULT 'replay',
    PRIMARY KEY (project_id, session_id, seq),
    FOREIGN KEY (project_id, session_id) REFERENCES replay_sessions (project_id, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS screen_views_project_screen_ts_idx ON screen_views (project_id, screen, entered_at);

-- Backfill: re-queue already processed native sessions so the worker builds their flow rows.
UPDATE replay_sessions s SET taps_processed_at = NULL, taps_attempts = 0, taps_retry_at = NULL
FROM projects p WHERE p.id = s.project_id AND p.platform <> 'web';
