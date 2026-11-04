-- High-performance partitioned telemetry events (events, logs, sessions)
CREATE TABLE IF NOT EXISTS telemetry_events (
    id               UUID        NOT NULL,
    project_id       BIGINT      NOT NULL,
    kind             TEXT        NOT NULL CHECK (kind IN ('event', 'log', 'session')),
    name             TEXT        NOT NULL DEFAULT '',
    level            TEXT        NOT NULL DEFAULT '',
    message          TEXT        NOT NULL DEFAULT '',
    session_id       TEXT        NOT NULL DEFAULT '',
    user_id          TEXT        NOT NULL DEFAULT '',
    environment      TEXT        NOT NULL DEFAULT 'production',
    release_version  TEXT        NOT NULL DEFAULT '',
    client_timestamp TIMESTAMPTZ,
    event_timestamp  TIMESTAMPTZ NOT NULL,
    received_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    attributes       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    sdk_meta         JSONB       NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (received_at, id, project_id)
) PARTITION BY RANGE (received_at);

-- Default partition ensures unexpected dates never cause insert errors
CREATE TABLE IF NOT EXISTS telemetry_events_default PARTITION OF telemetry_events DEFAULT;

-- Time-series & multi-tenant indexes
CREATE INDEX IF NOT EXISTS telemetry_project_time_idx
    ON telemetry_events (project_id, event_timestamp DESC);

CREATE INDEX IF NOT EXISTS telemetry_project_kind_time_idx
    ON telemetry_events (project_id, kind, event_timestamp DESC);

CREATE INDEX IF NOT EXISTS telemetry_project_session_idx
    ON telemetry_events (project_id, session_id)
    WHERE session_id <> '';
