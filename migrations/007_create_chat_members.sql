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
