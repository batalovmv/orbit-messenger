-- Migration 030: Audit fixes
-- 1. Index on messages.sender_id for "messages by user" queries
-- 2. Length constraints on polls.solution and sticker_packs.short_name
-- 3. Add updated_at to chat_invite_links

-- Index on messages.sender_id
CREATE INDEX IF NOT EXISTS idx_messages_sender_id ON messages (sender_id);

-- Index on messages.reply_to_id
CREATE INDEX IF NOT EXISTS idx_messages_reply_to_id ON messages (reply_to_id) WHERE reply_to_id IS NOT NULL;

-- Length constraint on polls.solution (max 1024 chars)
ALTER TABLE polls ADD CONSTRAINT chk_polls_solution_length CHECK (length(solution) <= 1024);

-- Length constraint on sticker_packs.short_name (max 64 chars)
ALTER TABLE sticker_packs ADD CONSTRAINT chk_sticker_packs_short_name_length CHECK (length(short_name) <= 64);

-- Add updated_at to chat_invite_links
ALTER TABLE chat_invite_links ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();

-- Apply the updated_at trigger (uses the trigger from migration 010)
DROP TRIGGER IF EXISTS set_updated_at ON chat_invite_links;
CREATE TRIGGER set_updated_at BEFORE UPDATE ON chat_invite_links
FOR EACH ROW EXECUTE FUNCTION update_updated_at();
