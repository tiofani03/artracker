-- People: per end-user profiles and per-session device context, built from ingested telemetry.
-- "users" is the team-member table; end users of the instrumented app live here.
CREATE TABLE IF NOT EXISTS user_profiles (
    project_id      BIGINT      NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id         TEXT        NOT NULL,
    traits          JSONB       NOT NULL DEFAULT '{}'::jsonb,
    first_seen      TIMESTAMPTZ NOT NULL,
    last_seen       TIMESTAMPTZ NOT NULL,
    last_session_id TEXT        NOT NULL DEFAULT '',
    last_context    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- event time of the newest $identify merged into traits: an older identify (e.g. from an offline device) never
    -- overwrites newer traits
    traits_updated_at TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX IF NOT EXISTS user_profiles_project_last_seen_idx
    ON user_profiles (project_id, last_seen DESC);

-- One row per session, anonymous ones included (user_id = ''), so a replay can always show its device context.
CREATE TABLE IF NOT EXISTS session_contexts (
    project_id  BIGINT      NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    session_id  TEXT        NOT NULL,
    user_id     TEXT        NOT NULL DEFAULT '',
    first_seen  TIMESTAMPTZ NOT NULL,
    last_seen   TIMESTAMPTZ NOT NULL,
    has_error   BOOLEAN     NOT NULL DEFAULT false,
    context     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, session_id)
);

CREATE INDEX IF NOT EXISTS session_contexts_project_user_idx
    ON session_contexts (project_id, user_id, last_seen DESC) WHERE user_id <> '';

-- Backfill from what already exists so People is not empty on the first deploy.
-- Replay sessions: user, time bounds and error flag.
INSERT INTO session_contexts (project_id, session_id, user_id, first_seen, last_seen, has_error)
SELECT project_id, id, user_id, start_time, COALESCE(end_time, updated_at, start_time), has_error
FROM replay_sessions
ON CONFLICT (project_id, session_id) DO NOTHING;

-- Device context from the session-start telemetry of the last 30 days.
INSERT INTO session_contexts (project_id, session_id, user_id, first_seen, last_seen, context)
SELECT DISTINCT ON (project_id, session_id)
       project_id, session_id, user_id, event_timestamp, event_timestamp, attributes
FROM telemetry_events
WHERE kind = 'session' AND name IN ('session_start', 'session.start') AND session_id <> ''
  AND event_timestamp > now() - interval '30 days'
  AND received_at > now() - interval '31 days' -- prunes partitions (the table is partitioned by received_at)
  AND project_id IN (SELECT id FROM projects)
ORDER BY project_id, session_id, event_timestamp
ON CONFLICT (project_id, session_id) DO UPDATE SET
    context = EXCLUDED.context,
    user_id = CASE WHEN session_contexts.user_id = '' THEN EXCLUDED.user_id ELSE session_contexts.user_id END;

-- Error flag for telemetry-only sessions of the same window.
UPDATE session_contexts sc SET has_error = true
FROM (
    SELECT DISTINCT project_id, session_id
    FROM telemetry_events
    WHERE level IN ('error', 'fatal') AND session_id <> ''
      AND event_timestamp > now() - interval '30 days'
      AND received_at > now() - interval '31 days'
) e
WHERE sc.project_id = e.project_id AND sc.session_id = e.session_id AND NOT sc.has_error;

INSERT INTO user_profiles (project_id, user_id, first_seen, last_seen, last_session_id, last_context)
SELECT DISTINCT ON (project_id, user_id)
       project_id, user_id,
       min(first_seen) OVER (PARTITION BY project_id, user_id),
       max(last_seen) OVER (PARTITION BY project_id, user_id),
       session_id, context
FROM session_contexts
WHERE user_id <> ''
ORDER BY project_id, user_id, last_seen DESC
ON CONFLICT (project_id, user_id) DO NOTHING;
