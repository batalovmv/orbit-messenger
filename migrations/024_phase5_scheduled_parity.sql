ALTER TABLE scheduled_messages
    ADD COLUMN IF NOT EXISTS reply_to_id UUID REFERENCES messages(id) ON DELETE SET NULL;

ALTER TABLE scheduled_messages
    ADD COLUMN IF NOT EXISTS media_ids UUID[];

ALTER TABLE scheduled_messages
    ADD COLUMN IF NOT EXISTS is_spoiler BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE scheduled_messages
    ADD COLUMN IF NOT EXISTS poll_payload JSONB;
