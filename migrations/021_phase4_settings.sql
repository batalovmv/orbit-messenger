-- Phase 4: Search, Notifications & Settings
-- Tables: privacy_settings, blocked_users, user_settings, notification_settings, push_subscriptions

-- Privacy settings (who can see what)
CREATE TABLE IF NOT EXISTS privacy_settings (
    user_id UUID REFERENCES users(id) ON DELETE CASCADE PRIMARY KEY,
    last_seen TEXT NOT NULL DEFAULT 'everyone',       -- everyone / contacts / nobody
    avatar TEXT NOT NULL DEFAULT 'everyone',           -- everyone / contacts / nobody
    phone TEXT NOT NULL DEFAULT 'contacts',            -- everyone / contacts / nobody
    calls TEXT NOT NULL DEFAULT 'everyone',            -- everyone / contacts / nobody
    groups TEXT NOT NULL DEFAULT 'everyone',           -- everyone / contacts / nobody
    forwarded TEXT NOT NULL DEFAULT 'everyone',        -- everyone / contacts / nobody
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Blocked users
CREATE TABLE IF NOT EXISTS blocked_users (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, blocked_user_id),
    CHECK (user_id != blocked_user_id)
);

CREATE INDEX IF NOT EXISTS idx_blocked_users_blocked ON blocked_users(blocked_user_id);

-- User settings (appearance, behavior)
CREATE TABLE IF NOT EXISTS user_settings (
    user_id UUID REFERENCES users(id) ON DELETE CASCADE PRIMARY KEY,
    theme TEXT NOT NULL DEFAULT 'auto',               -- auto / light / dark
    language TEXT NOT NULL DEFAULT 'ru',
    font_size INT NOT NULL DEFAULT 16,
    send_by_enter BOOLEAN NOT NULL DEFAULT true,
    dnd_from TIME,
    dnd_until TIME,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Per-chat notification settings
CREATE TABLE IF NOT EXISTS notification_settings (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    muted_until TIMESTAMPTZ,
    sound TEXT NOT NULL DEFAULT 'default',
    show_preview BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, chat_id)
);

-- Push subscriptions (Web Push VAPID)
CREATE TABLE IF NOT EXISTS push_subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint TEXT NOT NULL,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    user_agent TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, endpoint)
);

CREATE INDEX IF NOT EXISTS idx_push_subscriptions_user ON push_subscriptions(user_id);
