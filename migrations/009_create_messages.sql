CREATE SEQUENCE messages_seq;

CREATE TABLE messages (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_id             UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    sender_id           UUID REFERENCES users(id),
    type                TEXT NOT NULL DEFAULT 'text' CHECK (type IN ('text', 'photo', 'video', 'file', 'voice', 'videonote', 'sticker', 'poll', 'system')),
    content             TEXT,
    reply_to_id         UUID REFERENCES messages(id),
    is_edited           BOOLEAN NOT NULL DEFAULT false,
    is_deleted          BOOLEAN NOT NULL DEFAULT false,
    is_pinned           BOOLEAN NOT NULL DEFAULT false,
    is_forwarded        BOOLEAN NOT NULL DEFAULT false,
    forwarded_from      UUID REFERENCES users(id),
    thread_id           UUID,
    sequence_number     BIGINT NOT NULL DEFAULT nextval('messages_seq'),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    edited_at           TIMESTAMPTZ
);

CREATE INDEX idx_messages_chat_seq ON messages(chat_id, sequence_number DESC);
CREATE INDEX idx_messages_chat_created ON messages(chat_id, created_at DESC);
