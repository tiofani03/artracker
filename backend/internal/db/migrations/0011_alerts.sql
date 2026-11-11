-- Alerting: per-project rules, Telegram channels, link codes and incidents.

CREATE TABLE IF NOT EXISTS alert_rules (
    project_id           BIGINT      NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    kind                 TEXT        NOT NULL CHECK (kind IN ('new_issue', 'regression', 'issue_spike')),
    enabled              BOOLEAN     NOT NULL DEFAULT true,
    environment          TEXT        NOT NULL DEFAULT 'production',
    spike_multiplier     DOUBLE PRECISION NOT NULL DEFAULT 5,
    spike_min_count      INT         NOT NULL DEFAULT 20,
    spike_window_minutes INT         NOT NULL DEFAULT 60,
    cooldown_minutes     INT         NOT NULL DEFAULT 60,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, kind)
);

CREATE TABLE IF NOT EXISTS alert_channels (
    id               BIGSERIAL   PRIMARY KEY,
    project_id       BIGINT      NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    kind             TEXT        NOT NULL DEFAULT 'telegram' CHECK (kind IN ('telegram')),
    telegram_chat_id BIGINT      NOT NULL,
    title            TEXT        NOT NULL DEFAULT '',
    chat_type        TEXT        NOT NULL DEFAULT '',
    enabled          BOOLEAN     NOT NULL DEFAULT true,
    disabled_reason  TEXT        NOT NULL DEFAULT '',
    last_sent_at     TIMESTAMPTZ,
    last_error       TEXT        NOT NULL DEFAULT '',
    created_by       BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, telegram_chat_id)
);

CREATE TABLE IF NOT EXISTS alert_link_codes (
    code       TEXT        PRIMARY KEY,
    project_id BIGINT      NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_by BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS alert_incidents (
    id                    BIGSERIAL   PRIMARY KEY,
    project_id            BIGINT      NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    rule_kind             TEXT        NOT NULL CHECK (rule_kind IN ('new_issue', 'regression', 'issue_spike')),
    issue_id              BIGINT      NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    status                TEXT        NOT NULL DEFAULT 'firing' CHECK (status IN ('firing', 'resolved')),
    title                 TEXT        NOT NULL DEFAULT '',
    environment           TEXT        NOT NULL DEFAULT '',
    release_version       TEXT        NOT NULL DEFAULT '',
    session_id            TEXT        NOT NULL DEFAULT '',
    detail                JSONB       NOT NULL DEFAULT '{}'::jsonb,
    fired_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at           TIMESTAMPTZ,
    notify_state          TEXT        NOT NULL DEFAULT 'pending' CHECK (notify_state IN ('pending', 'sent', 'failed', 'skipped')),
    notify_attempts       INT         NOT NULL DEFAULT 0,
    next_attempt_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    recovery_notify_state TEXT        NOT NULL DEFAULT 'none' CHECK (recovery_notify_state IN ('none', 'pending', 'sent', 'failed', 'skipped'))
);

CREATE UNIQUE INDEX IF NOT EXISTS alert_incidents_one_firing
    ON alert_incidents (project_id, rule_kind, issue_id) WHERE status = 'firing';
CREATE INDEX IF NOT EXISTS alert_incidents_project_status_idx
    ON alert_incidents (project_id, status, fired_at DESC);
CREATE INDEX IF NOT EXISTS alert_incidents_pending_idx
    ON alert_incidents (next_attempt_at) WHERE notify_state = 'pending' OR recovery_notify_state = 'pending';

ALTER TABLE issue_events ADD COLUMN IF NOT EXISTS environment TEXT NOT NULL DEFAULT '';

INSERT INTO alert_rules(project_id, kind)
SELECT p.id, k FROM projects p CROSS JOIN unnest(ARRAY['new_issue','regression','issue_spike']) k
ON CONFLICT DO NOTHING;
