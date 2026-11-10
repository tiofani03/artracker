-- Per-project switch for screenshot-mode mobile replay (artracker-screen-v1).
-- Default true: the SDKs already require an explicit opt-in before anything is captured.
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS replay_screenshot_enabled BOOLEAN NOT NULL DEFAULT true;
