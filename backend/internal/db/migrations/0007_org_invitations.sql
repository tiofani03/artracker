-- Organization member invitations & team sharing
CREATE TABLE IF NOT EXISTS organization_invitations (
    id          BIGSERIAL PRIMARY KEY,
    org_id      BIGINT      NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email       TEXT        NOT NULL,
    role        TEXT        NOT NULL CHECK (role IN ('admin', 'member', 'viewer')),
    token_hash  TEXT        NOT NULL UNIQUE,
    invited_by  BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    accepted_by BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_org_invitations_lookup 
    ON organization_invitations (org_id, email) 
    WHERE accepted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_org_invitations_token 
    ON organization_invitations (token_hash);
