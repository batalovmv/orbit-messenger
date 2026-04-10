-- User device keys (identity key + signed prekey per device)
CREATE TABLE IF NOT EXISTS user_keys (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id UUID NOT NULL,
    identity_key BYTEA NOT NULL,
    signed_prekey BYTEA NOT NULL,
    signed_prekey_signature BYTEA NOT NULL,
    signed_prekey_id INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, device_id)
);

CREATE INDEX IF NOT EXISTS idx_user_keys_user_id ON user_keys(user_id);

-- One-time prekeys (consumed atomically on session init)
CREATE TABLE IF NOT EXISTS one_time_prekeys (
    id SERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id UUID NOT NULL,
    key_id INT NOT NULL,
    public_key BYTEA NOT NULL,
    used BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, device_id, key_id)
);

CREATE INDEX IF NOT EXISTS idx_otp_user_device ON one_time_prekeys(user_id, device_id, used);

-- Key transparency log (append-only audit of key changes)
CREATE TABLE IF NOT EXISTS key_transparency_log (
    id SERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    public_key_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ktl_user ON key_transparency_log(user_id);

DO $$ BEGIN
    CREATE TRIGGER trg_user_keys_updated_at
        BEFORE UPDATE ON user_keys
        FOR EACH ROW EXECUTE FUNCTION update_updated_at();
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TRIGGER trg_one_time_prekeys_updated_at
        BEFORE UPDATE ON one_time_prekeys
        FOR EACH ROW EXECUTE FUNCTION update_updated_at();
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- Compliance keys (escrow, schema-only - flow not implemented yet)
CREATE TABLE IF NOT EXISTS compliance_keys (
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    user_id UUID NOT NULL,
    device_id UUID NOT NULL,
    encrypted_session_key BYTEA NOT NULL,
    session_version INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chat_id, user_id, device_id, session_version)
);
