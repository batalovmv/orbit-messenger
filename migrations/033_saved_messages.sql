-- Phase 5 debt: saved messages lookup (self-chat)
CREATE TABLE IF NOT EXISTS saved_messages_lookup (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE
);
