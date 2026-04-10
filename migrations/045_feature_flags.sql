CREATE TABLE IF NOT EXISTS feature_flags (
    key TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT false,
    description TEXT,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    CREATE TRIGGER trg_feature_flags_updated_at
        BEFORE UPDATE ON feature_flags
        FOR EACH ROW EXECUTE FUNCTION update_updated_at();
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- E2E DM feature flag - disabled by default
INSERT INTO feature_flags (key, enabled, description)
VALUES ('e2e_dm_enabled', false, 'Enable E2E encryption for new DM chats')
ON CONFLICT (key) DO NOTHING;
