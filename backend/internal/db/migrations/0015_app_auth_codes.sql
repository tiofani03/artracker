-- One-time codes for the native app (Tauri) login. The OAuth callback runs in the
-- system browser, so it hands the app a short-lived code over a deep link; the app
-- redeems it together with its PKCE verifier. Only the hash of the code is stored.
CREATE TABLE IF NOT EXISTS app_auth_codes (
    id             BIGSERIAL PRIMARY KEY,
    code_hash      TEXT NOT NULL UNIQUE,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_challenge TEXT NOT NULL,
    expires_at     TIMESTAMPTZ NOT NULL,
    used_at        TIMESTAMPTZ NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS app_auth_codes_expires_idx ON app_auth_codes (expires_at);
