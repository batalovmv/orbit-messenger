-- Migration 036: RBAC system — hierarchical roles, audit log, user deactivation

-- 1. Expand users.role to include new system roles
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
DO $$
BEGIN
    ALTER TABLE users ADD CONSTRAINT users_role_check
        CHECK (role IN ('superadmin', 'compliance', 'admin', 'member'));
EXCEPTION
    WHEN duplicate_object THEN
        NULL;
END $$;

-- 2. Promote existing admins to superadmin
UPDATE users SET role = 'superadmin' WHERE role = 'admin';

-- 3. User deactivation support
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE users ADD COLUMN IF NOT EXISTS deactivated_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS deactivated_by UUID REFERENCES users(id);

CREATE INDEX IF NOT EXISTS idx_users_is_active ON users(is_active) WHERE NOT is_active;

-- 4. Expand invites.role to support new roles
ALTER TABLE invites DROP CONSTRAINT IF EXISTS invites_role_check;
DO $$
BEGIN
    ALTER TABLE invites ADD CONSTRAINT invites_role_check
        CHECK (role IN ('superadmin', 'compliance', 'admin', 'member'));
EXCEPTION
    WHEN duplicate_object THEN
        NULL;
END $$;

-- 5. Append-only audit log
CREATE TABLE IF NOT EXISTS audit_log (
    id          BIGSERIAL PRIMARY KEY,
    actor_id    UUID NOT NULL REFERENCES users(id),
    action      TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id   TEXT,
    details     JSONB DEFAULT '{}',
    ip_address  INET,
    user_agent  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_log(actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_target ON audit_log(target_type, target_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log(created_at DESC);

-- 6. Append-only enforcement via triggers (prevent UPDATE and DELETE)
CREATE OR REPLACE FUNCTION prevent_audit_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only: % not allowed', TG_OP;
END;
$$ LANGUAGE plpgsql;

DO $$
BEGIN
    CREATE TRIGGER audit_log_no_update
        BEFORE UPDATE ON audit_log
        FOR EACH ROW EXECUTE FUNCTION prevent_audit_mutation();
EXCEPTION
    WHEN duplicate_object THEN
        NULL;
END $$;

DO $$
BEGIN
    CREATE TRIGGER audit_log_no_delete
        BEFORE DELETE ON audit_log
        FOR EACH ROW EXECUTE FUNCTION prevent_audit_mutation();
EXCEPTION
    WHEN duplicate_object THEN
        NULL;
END $$;
