CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT UNIQUE NOT NULL,
    password_hash   TEXT NOT NULL,
    phone           TEXT UNIQUE,
    display_name    TEXT NOT NULL,
    avatar_url      TEXT,
    bio             TEXT,
    status          TEXT NOT NULL DEFAULT 'offline' CHECK (status IN ('online', 'offline', 'recently')),
    custom_status       TEXT,
    custom_status_emoji TEXT,
    role            TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member')),
    totp_secret     TEXT,
    totp_enabled    BOOLEAN NOT NULL DEFAULT false,
    invited_by      UUID REFERENCES users(id),
    invite_code     TEXT,
    last_seen_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_users_email ON users(email);
CREATE TABLE devices (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_name     TEXT,
    device_type     TEXT CHECK (device_type IN ('web', 'desktop', 'ios', 'android')),
    identity_key    BYTEA,
    push_token      TEXT,
    push_type       TEXT CHECK (push_type IN ('vapid', 'fcm', 'apns')),
    last_active_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id   UUID REFERENCES devices(id) ON DELETE SET NULL,
    token_hash  TEXT NOT NULL,
    ip_address  INET,
    user_agent  TEXT,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_token ON sessions(token_hash);
CREATE TABLE invites (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code        TEXT UNIQUE NOT NULL,
    created_by  UUID REFERENCES users(id),
    email       TEXT,
    role        TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member')),
    max_uses    INT NOT NULL DEFAULT 1,
    use_count   INT NOT NULL DEFAULT 0,
    used_by     UUID REFERENCES users(id),
    used_at     TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ,
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE chats (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type            TEXT NOT NULL CHECK (type IN ('direct', 'group', 'channel')),
    name            TEXT,
    description     TEXT,
    avatar_url      TEXT,
    created_by      UUID REFERENCES users(id),
    is_encrypted    BOOLEAN NOT NULL DEFAULT false,
    max_members     INT NOT NULL DEFAULT 200000,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE chat_members (
    chat_id             UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role                TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'admin', 'member', 'readonly', 'banned')),
    last_read_message_id UUID,
    joined_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    muted_until         TIMESTAMPTZ,
    notification_level  TEXT NOT NULL DEFAULT 'all' CHECK (notification_level IN ('all', 'mentions', 'none')),
    PRIMARY KEY (chat_id, user_id)
);

CREATE INDEX idx_chat_members_user ON chat_members(user_id);
CREATE TABLE direct_chat_lookup (
    user1_id    UUID NOT NULL,
    user2_id    UUID NOT NULL,
    chat_id     UUID NOT NULL REFERENCES chats(id),
    PRIMARY KEY (user1_id, user2_id),
    CONSTRAINT direct_chat_canonical_order CHECK (user1_id < user2_id)
);
CREATE SEQUENCE messages_seq;

CREATE TABLE messages (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_id             UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    sender_id           UUID REFERENCES users(id),
    type                TEXT NOT NULL DEFAULT 'text' CHECK (type IN ('text', 'photo', 'video', 'file', 'voice', 'videonote', 'sticker', 'poll', 'system')),
    content             TEXT,
    encrypted_content   BYTEA,
    reply_to_id         UUID REFERENCES messages(id),
    is_edited           BOOLEAN NOT NULL DEFAULT false,
    is_deleted          BOOLEAN NOT NULL DEFAULT false,
    is_pinned           BOOLEAN NOT NULL DEFAULT false,
    is_forwarded        BOOLEAN NOT NULL DEFAULT false,
    forwarded_from      UUID REFERENCES users(id),
    thread_id           UUID,
    expires_at          TIMESTAMPTZ,
    sequence_number     BIGINT NOT NULL DEFAULT nextval('messages_seq'),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    edited_at           TIMESTAMPTZ
);

CREATE INDEX idx_messages_chat_seq ON messages(chat_id, sequence_number DESC);
CREATE INDEX idx_messages_chat_created ON messages(chat_id, created_at DESC);
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_chats_updated_at
    BEFORE UPDATE ON chats
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();
-- Fix missing foreign keys, ON DELETE behaviors, and indexes

-- 1. devices: add index for user_id lookups (FK exists but no index)
CREATE INDEX IF NOT EXISTS idx_devices_user ON devices(user_id);

-- 2. invites: add ON DELETE SET NULL for created_by and used_by
ALTER TABLE invites
    DROP CONSTRAINT IF EXISTS invites_created_by_fkey,
    ADD CONSTRAINT invites_created_by_fkey
        FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE invites
    DROP CONSTRAINT IF EXISTS invites_used_by_fkey,
    ADD CONSTRAINT invites_used_by_fkey
        FOREIGN KEY (used_by) REFERENCES users(id) ON DELETE SET NULL;

-- 3. chat_members: add FK for last_read_message_id
ALTER TABLE chat_members
    ADD CONSTRAINT chat_members_last_read_message_id_fkey
        FOREIGN KEY (last_read_message_id) REFERENCES messages(id) ON DELETE SET NULL;

-- 4. direct_chat_lookup: add FK for user1_id and user2_id
ALTER TABLE direct_chat_lookup
    ADD CONSTRAINT direct_chat_lookup_user1_fkey
        FOREIGN KEY (user1_id) REFERENCES users(id) ON DELETE CASCADE,
    ADD CONSTRAINT direct_chat_lookup_user2_fkey
        FOREIGN KEY (user2_id) REFERENCES users(id) ON DELETE CASCADE;

-- 5. messages: add ON DELETE SET NULL for sender_id and forwarded_from
ALTER TABLE messages
    DROP CONSTRAINT IF EXISTS messages_sender_id_fkey,
    ADD CONSTRAINT messages_sender_id_fkey
        FOREIGN KEY (sender_id) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE messages
    DROP CONSTRAINT IF EXISTS messages_forwarded_from_fkey,
    ADD CONSTRAINT messages_forwarded_from_fkey
        FOREIGN KEY (forwarded_from) REFERENCES users(id) ON DELETE SET NULL;
-- Add entities column to messages for rich text formatting (bold, italic, code, etc.)
ALTER TABLE messages ADD COLUMN IF NOT EXISTS entities JSONB;
-- Fix direct_chat_lookup.chat_id to cascade on chat deletion
ALTER TABLE direct_chat_lookup
    DROP CONSTRAINT IF EXISTS direct_chat_lookup_chat_id_fkey,
    ADD CONSTRAINT direct_chat_lookup_chat_id_fkey
        FOREIGN KEY (chat_id) REFERENCES chats(id) ON DELETE CASCADE;
