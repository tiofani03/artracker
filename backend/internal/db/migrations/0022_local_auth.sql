-- Local email/password login next to Google SSO.
-- A password credential is users.password_hash IS NOT NULL, matched by lower(email); a Google user keeps
-- its row and gains a hash when it registers through an invitation. Rows created by the local flow itself
-- use sso_provider='local' and sso_id=lower(email).
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash TEXT;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_sso_provider_check;
ALTER TABLE users ADD CONSTRAINT users_sso_provider_check CHECK (sso_provider IN ('google', 'local'));

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_local_has_password;
ALTER TABLE users ADD CONSTRAINT users_local_has_password CHECK (sso_provider <> 'local' OR password_hash IS NOT NULL);

-- At most one password account per email, case-insensitively.
CREATE UNIQUE INDEX IF NOT EXISTS users_password_email_uidx ON users (lower(email)) WHERE password_hash IS NOT NULL;
