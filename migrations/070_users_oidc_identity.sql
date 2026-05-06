-- 070_users_oidc_identity.sql
-- OIDC SSO (ADR 006): users may be linked to a single corporate OIDC
-- identity. `oidc_subject` is the `sub` claim from the id_token,
-- `oidc_provider` matches the env-configured provider key (e.g.
-- "google"). Both nullable so the existing password+invite users
-- continue to work unchanged.
--
-- The partial unique index enforces "one OIDC identity per user, but
-- many users may have no identity". WHERE clause keeps the index small
-- and avoids polluting it with NULL rows on a 100k-user tenant.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS oidc_subject  TEXT,
    ADD COLUMN IF NOT EXISTS oidc_provider TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_oidc_identity
    ON users (oidc_provider, oidc_subject)
    WHERE oidc_subject IS NOT NULL;
