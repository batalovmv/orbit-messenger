-- 057_smart_notifications.sql
-- Smart Notifications: AI priority classification, user preferences, feedback loop

-- Notification priority feedback for ML training
CREATE TABLE IF NOT EXISTS notification_priority_feedback (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    classified_priority TEXT NOT NULL CHECK (classified_priority IN ('urgent', 'important', 'normal', 'low')),
    user_override_priority TEXT NOT NULL CHECK (user_override_priority IN ('urgent', 'important', 'normal', 'low')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_npf_user_message ON notification_priority_feedback(user_id, message_id);
CREATE INDEX IF NOT EXISTS idx_npf_user_created ON notification_priority_feedback(user_id, created_at DESC);

-- User notification priority mode
ALTER TABLE users ADD COLUMN IF NOT EXISTS notification_priority_mode TEXT NOT NULL DEFAULT 'smart'
    CHECK (notification_priority_mode IN ('smart', 'all', 'off'));

-- Per-chat notification priority override
CREATE TABLE IF NOT EXISTS chat_notification_overrides (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    priority_override TEXT NOT NULL CHECK (priority_override IN ('urgent', 'important', 'normal', 'low')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, chat_id)
);
