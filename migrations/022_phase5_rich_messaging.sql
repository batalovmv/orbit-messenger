-- Phase 5: Rich Messaging — reactions, stickers, GIF, polls, scheduled messages

-- ============================================================================
-- Reactions
-- ============================================================================

CREATE TABLE IF NOT EXISTS message_reactions (
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    emoji      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (message_id, user_id, emoji)
);

CREATE INDEX IF NOT EXISTS idx_message_reactions_message ON message_reactions(message_id);
CREATE INDEX IF NOT EXISTS idx_message_reactions_user    ON message_reactions(user_id);

CREATE TABLE IF NOT EXISTS chat_available_reactions (
    chat_id        UUID PRIMARY KEY REFERENCES chats(id) ON DELETE CASCADE,
    mode           TEXT NOT NULL DEFAULT 'all' CHECK (mode IN ('all', 'selected', 'none')),
    allowed_emojis TEXT[],
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$
BEGIN
    CREATE TRIGGER trg_chat_available_reactions_updated_at
        BEFORE UPDATE ON chat_available_reactions
        FOR EACH ROW EXECUTE FUNCTION update_updated_at();
EXCEPTION
    WHEN duplicate_object THEN
        NULL;
END $$;

-- ============================================================================
-- Sticker packs & stickers
-- ============================================================================

CREATE TABLE IF NOT EXISTS sticker_packs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title         TEXT NOT NULL,
    short_name    TEXT UNIQUE NOT NULL,
    author_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    thumbnail_url TEXT,
    is_official   BOOLEAN NOT NULL DEFAULT FALSE,
    is_animated   BOOLEAN NOT NULL DEFAULT FALSE,
    sticker_count INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$
BEGIN
    CREATE TRIGGER trg_sticker_packs_updated_at
        BEFORE UPDATE ON sticker_packs
        FOR EACH ROW EXECUTE FUNCTION update_updated_at();
EXCEPTION
    WHEN duplicate_object THEN
        NULL;
END $$;

CREATE TABLE IF NOT EXISTS stickers (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pack_id   UUID NOT NULL REFERENCES sticker_packs(id) ON DELETE CASCADE,
    emoji     TEXT,
    file_url  TEXT NOT NULL,
    file_type TEXT NOT NULL DEFAULT 'webp' CHECK (file_type IN ('webp', 'tgs', 'webm')),
    width     INT,
    height    INT,
    position  INT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_stickers_pack ON stickers(pack_id, position);

CREATE TABLE IF NOT EXISTS user_installed_stickers (
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pack_id      UUID NOT NULL REFERENCES sticker_packs(id) ON DELETE CASCADE,
    position     INT NOT NULL DEFAULT 0,
    installed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, pack_id)
);

CREATE INDEX IF NOT EXISTS idx_user_installed_stickers_user ON user_installed_stickers(user_id, position);

CREATE TABLE IF NOT EXISTS recent_stickers (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sticker_id UUID NOT NULL REFERENCES stickers(id) ON DELETE CASCADE,
    used_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, sticker_id)
);

CREATE INDEX IF NOT EXISTS idx_recent_stickers_user ON recent_stickers(user_id, used_at DESC);

-- ============================================================================
-- Saved GIFs
-- ============================================================================

CREATE TABLE IF NOT EXISTS saved_gifs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenor_id   TEXT NOT NULL,
    url        TEXT NOT NULL,
    preview_url TEXT,
    width      INT,
    height     INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, tenor_id)
);

CREATE INDEX IF NOT EXISTS idx_saved_gifs_user ON saved_gifs(user_id, created_at DESC);

-- ============================================================================
-- Polls
-- ============================================================================

CREATE TABLE IF NOT EXISTS polls (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id     UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    question       TEXT NOT NULL,
    is_anonymous   BOOLEAN NOT NULL DEFAULT TRUE,
    is_multiple    BOOLEAN NOT NULL DEFAULT FALSE,
    is_quiz        BOOLEAN NOT NULL DEFAULT FALSE,
    correct_option INT,
    is_closed      BOOLEAN NOT NULL DEFAULT FALSE,
    close_at       TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_polls_message ON polls(message_id);

CREATE TABLE IF NOT EXISTS poll_options (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    poll_id  UUID NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
    text     TEXT NOT NULL,
    position INT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_poll_options_poll ON poll_options(poll_id, position);

CREATE TABLE IF NOT EXISTS poll_votes (
    poll_id   UUID NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
    option_id UUID NOT NULL REFERENCES poll_options(id) ON DELETE CASCADE,
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    voted_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (poll_id, user_id, option_id)
);

CREATE INDEX IF NOT EXISTS idx_poll_votes_option ON poll_votes(option_id);

-- ============================================================================
-- Scheduled messages
-- ============================================================================

CREATE TABLE IF NOT EXISTS scheduled_messages (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_id      UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    sender_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content      TEXT,
    entities     JSONB,
    type         TEXT NOT NULL DEFAULT 'text' CHECK (type IN ('text', 'photo', 'video', 'file', 'voice', 'videonote', 'sticker', 'poll', 'system')),
    scheduled_at TIMESTAMPTZ NOT NULL,
    is_sent      BOOLEAN NOT NULL DEFAULT FALSE,
    sent_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_scheduled_messages_pending ON scheduled_messages(scheduled_at)
    WHERE is_sent = FALSE;
CREATE INDEX IF NOT EXISTS idx_scheduled_messages_user_chat ON scheduled_messages(sender_id, chat_id)
    WHERE is_sent = FALSE;

DO $$
BEGIN
    CREATE TRIGGER trg_scheduled_messages_updated_at
        BEFORE UPDATE ON scheduled_messages
        FOR EACH ROW EXECUTE FUNCTION update_updated_at();
EXCEPTION
    WHEN duplicate_object THEN
        NULL;
END $$;
