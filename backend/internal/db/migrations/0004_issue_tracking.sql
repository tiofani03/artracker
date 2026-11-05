-- Error tracking & crash diagnostics: grouped issues, per-occurrence events,
-- and per-release source maps for stack trace demangling.

CREATE TABLE IF NOT EXISTS issues (
    id          BIGSERIAL PRIMARY KEY,
    project_id  BIGINT      NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    fingerprint TEXT        NOT NULL,
    title       TEXT        NOT NULL,
    type        TEXT        NOT NULL DEFAULT 'error',
    status      TEXT        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'ignored')),
    first_seen  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen   TIMESTAMPTZ NOT NULL DEFAULT now(),
    count       BIGINT      NOT NULL DEFAULT 1,
    assigned_to BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    UNIQUE (project_id, fingerprint)
);

CREATE INDEX IF NOT EXISTS issues_project_status_idx ON issues (project_id, status, last_seen DESC);
CREATE INDEX IF NOT EXISTS issues_project_count_idx ON issues (project_id, count DESC);

CREATE TABLE IF NOT EXISTS issue_events (
    id              BIGSERIAL PRIMARY KEY,
    issue_id        BIGINT      NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    session_id      TEXT        NOT NULL DEFAULT '',
    user_id         TEXT        NOT NULL DEFAULT '',
    stack_trace     TEXT        NOT NULL DEFAULT '',
    message         TEXT        NOT NULL DEFAULT '',
    breadcrumbs     JSONB       NOT NULL DEFAULT '[]'::jsonb,
    device_info     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    release_version TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS issue_events_issue_idx ON issue_events (issue_id, created_at DESC);

-- Source maps are stored per project + release version + output file so the
-- same minified file from different releases demangles against the right map.
CREATE TABLE IF NOT EXISTS source_maps (
    id              BIGSERIAL PRIMARY KEY,
    project_id      BIGINT      NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    release_version TEXT        NOT NULL,
    file_name       TEXT        NOT NULL,
    content         BYTEA       NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, release_version, file_name)
);

CREATE INDEX IF NOT EXISTS source_maps_project_release_idx ON source_maps (project_id, release_version);
