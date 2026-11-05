-- Growth Analytics Engine (Events, Identity Aliases, Saved Funnels)
CREATE TABLE IF NOT EXISTS events (
    id            UUID        NOT NULL,
    project_id    BIGINT      NOT NULL,
    session_id    TEXT        NOT NULL DEFAULT '',
    user_id       TEXT        NOT NULL DEFAULT '',
    device_id     TEXT        NOT NULL DEFAULT '',
    actor_id      TEXT        NOT NULL, -- 'u:<user_id>' | 'd:<device_id>' | 's:<session_id>'
    event_name    TEXT        NOT NULL,
    properties    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    utm_source    TEXT        NOT NULL DEFAULT '',
    utm_medium    TEXT        NOT NULL DEFAULT '',
    utm_campaign  TEXT        NOT NULL DEFAULT '',
    browser       TEXT        NOT NULL DEFAULT '',
    os            TEXT        NOT NULL DEFAULT '',
    "timestamp"   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (project_id, "timestamp", id)
) PARTITION BY RANGE ("timestamp");

CREATE TABLE IF NOT EXISTS events_default PARTITION OF events DEFAULT;

-- Covering index for funnel conversion queries (Index-Only Scan)
CREATE INDEX IF NOT EXISTS events_funnel_covering_idx
    ON events (project_id, event_name, "timestamp" ASC)
    INCLUDE (actor_id, session_id, utm_source, utm_medium, utm_campaign, browser, os);

-- Time-series & multi-tenant index for cohort retention & event catalog
CREATE INDEX IF NOT EXISTS events_cohort_actor_time_idx
    ON events (project_id, "timestamp" ASC, actor_id, event_name);

-- GIN index for custom JSONB property filtering
CREATE INDEX IF NOT EXISTS events_properties_gin_idx
    ON events USING GIN (properties jsonb_path_ops);

-- Identity aliases for automatic identity stitching (anonymous device/session -> logged-in user)
CREATE TABLE IF NOT EXISTS identity_aliases (
    project_id  BIGINT      NOT NULL,
    anon_id     TEXT        NOT NULL, -- 'd:<device_id>' or 's:<session_id>'
    user_id     TEXT        NOT NULL, -- 'u:<user_id>'
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, anon_id)
);

CREATE INDEX IF NOT EXISTS identity_aliases_project_user_idx
    ON identity_aliases (project_id, user_id);

-- Saved funnels per project
CREATE TABLE IF NOT EXISTS saved_funnels (
    id              BIGSERIAL   PRIMARY KEY,
    project_id      BIGINT      NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    steps           JSONB       NOT NULL,
    window_seconds  BIGINT      NOT NULL DEFAULT 86400,
    counting_mode   TEXT        NOT NULL DEFAULT 'users' CHECK (counting_mode IN ('users', 'sessions')),
    breakdown_by    TEXT        NOT NULL DEFAULT '',
    created_by      BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS saved_funnels_project_idx
    ON saved_funnels (project_id, updated_at DESC);
