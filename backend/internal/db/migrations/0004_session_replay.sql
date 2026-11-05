-- Session Replay metadata & chunks storage schema
CREATE TABLE IF NOT EXISTS replay_sessions (
    id               TEXT        NOT NULL, -- client session ID (e.g. sess_01j8xyz...)
    project_id       BIGINT      NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id          TEXT        NOT NULL DEFAULT '',
    environment      TEXT        NOT NULL DEFAULT 'production',
    start_time       TIMESTAMPTZ NOT NULL DEFAULT now(),
    end_time         TIMESTAMPTZ,
    duration_ms      BIGINT      NOT NULL DEFAULT 0,
    chunk_count      INT         NOT NULL DEFAULT 0,
    event_count      BIGINT      NOT NULL DEFAULT 0,
    total_size_bytes BIGINT      NOT NULL DEFAULT 0,
    has_error        BOOLEAN     NOT NULL DEFAULT false,
    has_rage_click   BOOLEAN     NOT NULL DEFAULT false,
    storage_prefix   TEXT        NOT NULL DEFAULT '', -- e.g. "projects/1/sessions/sess_123/"
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, id)
);

CREATE INDEX IF NOT EXISTS idx_replay_sessions_project_time 
    ON replay_sessions (project_id, start_time DESC);

CREATE INDEX IF NOT EXISTS idx_replay_sessions_project_issues 
    ON replay_sessions (project_id, start_time DESC) 
    WHERE has_error = true OR has_rage_click = true;

CREATE INDEX IF NOT EXISTS idx_replay_sessions_project_user 
    ON replay_sessions (project_id, user_id, start_time DESC) 
    WHERE user_id <> '';

CREATE TABLE IF NOT EXISTS replay_chunks (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id  BIGINT      NOT NULL,
    session_id  TEXT        NOT NULL,
    chunk_seq   INT         NOT NULL CHECK (chunk_seq >= 0),
    object_key  TEXT        NOT NULL,
    size_bytes  BIGINT      NOT NULL DEFAULT 0,
    event_count INT         NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (project_id, session_id) REFERENCES replay_sessions(project_id, id) ON DELETE CASCADE,
    UNIQUE (project_id, session_id, chunk_seq)
);

CREATE INDEX IF NOT EXISTS idx_replay_chunks_lookup 
    ON replay_chunks (project_id, session_id, chunk_seq ASC);
