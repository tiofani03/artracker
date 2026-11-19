-- App start profiling: one row per measured app start, plus the raw profile
-- (symbolicated asynchronously) and the per-function stats derived from it.
CREATE TABLE IF NOT EXISTS app_starts (
    id           BIGSERIAL   PRIMARY KEY,
    project_id   BIGINT      NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    start_id     TEXT        NOT NULL,
    session_id   TEXT        NOT NULL DEFAULT '',
    platform     TEXT        NOT NULL,
    environment  TEXT        NOT NULL DEFAULT 'production',
    type         TEXT        NOT NULL CHECK (type IN ('cold', 'warm', 'hot')),
    duration_ms  INT         NOT NULL,
    phases       JSONB       NOT NULL DEFAULT '[]',
    first_screen TEXT        NOT NULL DEFAULT '',
    app_version  TEXT        NOT NULL DEFAULT '',
    build        TEXT        NOT NULL DEFAULT '',
    os_version   TEXT        NOT NULL DEFAULT '',
    device_model TEXT        NOT NULL DEFAULT '',
    ram_mb       INT,
    trampoline   BOOLEAN     NOT NULL DEFAULT false,
    profiled     BOOLEAN     NOT NULL DEFAULT false,
    started_at   TIMESTAMPTZ NOT NULL,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, start_id)
);
CREATE INDEX IF NOT EXISTS app_starts_project_started_idx ON app_starts (project_id, started_at);
CREATE INDEX IF NOT EXISTS app_starts_project_dims_idx ON app_starts (project_id, platform, type, app_version, started_at);

-- image_uuids lets a dSYM upload re-queue matching profiles without decompressing raw.
CREATE TABLE IF NOT EXISTS app_start_profiles (
    project_id      BIGINT      NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    start_id        TEXT        NOT NULL,
    platform        TEXT        NOT NULL,
    environment     TEXT        NOT NULL DEFAULT 'production',
    app_version     TEXT        NOT NULL DEFAULT '',
    build           TEXT        NOT NULL DEFAULT '',
    image_uuids     TEXT[]      NOT NULL DEFAULT '{}',
    raw             BYTEA       NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'missing_symbols', 'failed')),
    attempts        INT         NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ DEFAULT now(),
    symbolicated    JSONB,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, start_id)
);
CREATE INDEX IF NOT EXISTS app_start_profiles_status_next_idx ON app_start_profiles (status, next_attempt_at);

CREATE TABLE IF NOT EXISTS app_start_fn_stats (
    project_id    BIGINT      NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    start_id      TEXT        NOT NULL,
    platform      TEXT        NOT NULL,
    environment   TEXT        NOT NULL,
    app_version   TEXT        NOT NULL,
    function      TEXT        NOT NULL,
    is_app        BOOLEAN     NOT NULL,
    self_ms       INT         NOT NULL,
    total_ms      INT         NOT NULL,
    attributed_ms INT         NOT NULL,
    started_at    TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (project_id, start_id, function)
);
CREATE INDEX IF NOT EXISTS app_start_fn_stats_dims_idx ON app_start_fn_stats (project_id, platform, environment, app_version, started_at);
