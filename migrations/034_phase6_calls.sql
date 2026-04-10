-- Phase 6: Voice & Video Calls

CREATE TABLE IF NOT EXISTS calls (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type            TEXT NOT NULL CHECK (type IN ('voice', 'video')),
    mode            TEXT NOT NULL CHECK (mode IN ('p2p', 'group')),
    chat_id         UUID NOT NULL REFERENCES chats(id),
    initiator_id    UUID NOT NULL REFERENCES users(id),
    status          TEXT NOT NULL DEFAULT 'ringing' CHECK (status IN ('ringing', 'active', 'ended', 'missed', 'declined')),
    started_at      TIMESTAMPTZ,
    ended_at        TIMESTAMPTZ,
    duration_seconds INT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS call_participants (
    call_id          UUID NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    user_id          UUID NOT NULL REFERENCES users(id),
    joined_at        TIMESTAMPTZ DEFAULT NOW(),
    left_at          TIMESTAMPTZ,
    is_muted         BOOLEAN NOT NULL DEFAULT false,
    is_camera_off    BOOLEAN NOT NULL DEFAULT false,
    is_screen_sharing BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (call_id, user_id)
);

-- Fast lookup of active calls in a chat (prevent duplicate active calls)
CREATE INDEX IF NOT EXISTS idx_calls_chat_active ON calls (chat_id) WHERE status IN ('ringing', 'active');

-- Call history queries by user (via participants)
CREATE INDEX IF NOT EXISTS idx_call_participants_user ON call_participants (user_id);

-- Call history ordering
CREATE INDEX IF NOT EXISTS idx_calls_created_at ON calls (created_at DESC);
