-- Who minted each API key. NULL for keys created before this migration and
-- for keys whose creator was deleted. Used to revoke a member's mcp keys when
-- they leave the organization or become a viewer.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS created_by BIGINT NULL REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS api_keys_created_by_idx ON api_keys (created_by) WHERE created_by IS NOT NULL;
