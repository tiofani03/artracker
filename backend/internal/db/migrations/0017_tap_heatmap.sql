-- Tap heatmap: taps extracted from artracker-screen-v1 replay chunks by a background worker.
ALTER TABLE replay_sessions
    ADD COLUMN IF NOT EXISTS taps_processed_at     TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS taps_processed_chunks INT         NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS taps_unrecognized     INT         NOT NULL DEFAULT 0;

-- sessions the worker still has to (re)process
CREATE INDEX IF NOT EXISTS replay_sessions_taps_pending_idx
    ON replay_sessions (updated_at)
    WHERE taps_processed_at IS NULL OR chunk_count > taps_processed_chunks;

ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS heatmap_excluded_screens TEXT[] NOT NULL DEFAULT '{}';

CREATE TABLE IF NOT EXISTS screen_taps (
    project_id    BIGINT      NOT NULL,
    session_id    TEXT        NOT NULL,
    screen        TEXT        NOT NULL,
    ts            TIMESTAMPTZ NOT NULL,
    x_norm        REAL        NOT NULL,
    y_norm        REAL        NOT NULL,
    vw            REAL        NOT NULL,
    vh            REAL        NOT NULL,
    aspect_bucket REAL        NOT NULL,
    platform      TEXT        NOT NULL DEFAULT '',
    app_version   TEXT        NOT NULL DEFAULT '',
    landscape     BOOLEAN     NOT NULL DEFAULT false,
    rage          BOOLEAN     NOT NULL DEFAULT false,
    FOREIGN KEY (project_id, session_id) REFERENCES replay_sessions (project_id, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS screen_taps_project_screen_ts_idx ON screen_taps (project_id, screen, ts);

CREATE TABLE IF NOT EXISTS screen_visits (
    project_id    BIGINT      NOT NULL,
    session_id    TEXT        NOT NULL,
    screen        TEXT        NOT NULL,
    ts            TIMESTAMPTZ NOT NULL,
    platform      TEXT        NOT NULL DEFAULT '',
    app_version   TEXT        NOT NULL DEFAULT '',
    landscape     BOOLEAN     NOT NULL DEFAULT false,
    aspect_bucket REAL        NOT NULL,
    PRIMARY KEY (project_id, session_id, screen, landscape, aspect_bucket),
    FOREIGN KEY (project_id, session_id) REFERENCES replay_sessions (project_id, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS screen_visits_project_screen_ts_idx ON screen_visits (project_id, screen, ts);

-- one representative screenshot frame per screen/platform/version/orientation/aspect
CREATE TABLE IF NOT EXISTS screen_backgrounds (
    project_id    BIGINT      NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    screen        TEXT        NOT NULL,
    platform      TEXT        NOT NULL DEFAULT '',
    app_version   TEXT        NOT NULL DEFAULT '',
    landscape     BOOLEAN     NOT NULL DEFAULT false,
    aspect_bucket REAL        NOT NULL,
    session_id    TEXT        NOT NULL,
    chunk_seq     INT         NOT NULL,
    frame_index   INT         NOT NULL,
    vw            REAL        NOT NULL,
    vh            REAL        NOT NULL,
    masked        INT         NOT NULL DEFAULT 0,
    captured_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (project_id, screen, platform, app_version, landscape, aspect_bucket)
);
