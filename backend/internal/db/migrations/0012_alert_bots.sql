-- Alerting: one encrypted Telegram bot per organisation, and manual channels
-- addressed by a destination string. Additive and re-runnable; existing
-- channels become origin 'linked' and keep using telegram_chat_id.

CREATE TABLE IF NOT EXISTS alert_bots (
    org_id            BIGINT      PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    token_ciphertext  BYTEA       NOT NULL,
    key_id            TEXT        NOT NULL,
    bot_id            BIGINT      NOT NULL DEFAULT 0,
    bot_username      TEXT        NOT NULL,
    status            TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'invalid')),
    status_reason     TEXT        NOT NULL DEFAULT '',
    detail_level      TEXT        NOT NULL DEFAULT 'minimal' CHECK (detail_level IN ('minimal', 'full')),
    created_by        BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_validated_at TIMESTAMPTZ
);

ALTER TABLE alert_channels ADD COLUMN IF NOT EXISTS origin TEXT NOT NULL DEFAULT 'linked'
    CHECK (origin IN ('linked', 'manual'));
ALTER TABLE alert_channels ADD COLUMN IF NOT EXISTS destination TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_channels ALTER COLUMN telegram_chat_id DROP NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS alert_channels_manual_destination
    ON alert_channels (project_id, destination) WHERE origin = 'manual';
