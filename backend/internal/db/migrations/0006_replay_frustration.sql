-- Add dead click frustration flag to replay_sessions
ALTER TABLE replay_sessions
    ADD COLUMN IF NOT EXISTS has_dead_click BOOLEAN NOT NULL DEFAULT false;

DROP INDEX IF EXISTS idx_replay_sessions_project_issues;
CREATE INDEX IF NOT EXISTS idx_replay_sessions_project_issues
    ON replay_sessions (project_id, start_time DESC)
    WHERE has_error = true OR has_rage_click = true OR has_dead_click = true;
