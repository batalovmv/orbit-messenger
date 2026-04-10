-- Phase 8: Add reply_markup (inline keyboards) and via_bot_id to messages

ALTER TABLE messages ADD COLUMN reply_markup JSONB;
ALTER TABLE messages ADD COLUMN via_bot_id UUID REFERENCES users(id) ON DELETE SET NULL;
