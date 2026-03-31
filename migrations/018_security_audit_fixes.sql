-- 018_security_audit_fixes.sql
-- Fixes from security audit: missing indexes, constraints, ON DELETE policies, updated_at columns.

-- C-1: Prevent duplicate sequence numbers per chat (cursor-based pagination integrity)
ALTER TABLE messages ADD CONSTRAINT messages_chat_seq_unique UNIQUE (chat_id, sequence_number);

-- C-3: Index on direct_chat_lookup.user2_id for DM lookup (PK only covers user1_id prefix)
CREATE INDEX IF NOT EXISTS idx_direct_chat_lookup_user2 ON direct_chat_lookup(user2_id);

-- I-1: Index on chats.created_by FK
CREATE INDEX IF NOT EXISTS idx_chats_created_by ON chats(created_by);

-- I-2: chats.created_by — add ON DELETE SET NULL (was missing, blocks user deletion)
ALTER TABLE chats ALTER COLUMN created_by DROP NOT NULL;
ALTER TABLE chats DROP CONSTRAINT IF EXISTS chats_created_by_fkey;
ALTER TABLE chats ADD CONSTRAINT chats_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;

-- I-3: chat_invite_links.creator_id — change ON DELETE CASCADE to SET NULL
-- (CASCADE silently destroys shared invite links when a user is deleted)
ALTER TABLE chat_invite_links ALTER COLUMN creator_id DROP NOT NULL;
ALTER TABLE chat_invite_links DROP CONSTRAINT IF EXISTS chat_invite_links_creator_id_fkey;
ALTER TABLE chat_invite_links ADD CONSTRAINT chat_invite_links_creator_id_fkey
    FOREIGN KEY (creator_id) REFERENCES users(id) ON DELETE SET NULL;

-- I-4: chat_join_requests — add reviewed_at, updated_at, index, and fix ON DELETE on reviewed_by
ALTER TABLE chat_join_requests ADD COLUMN IF NOT EXISTS reviewed_at TIMESTAMPTZ;
ALTER TABLE chat_join_requests ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE INDEX IF NOT EXISTS idx_join_requests_reviewed_by ON chat_join_requests(reviewed_by);

ALTER TABLE chat_join_requests DROP CONSTRAINT IF EXISTS chat_join_requests_reviewed_by_fkey;
ALTER TABLE chat_join_requests ADD CONSTRAINT chat_join_requests_reviewed_by_fkey
    FOREIGN KEY (reviewed_by) REFERENCES users(id) ON DELETE SET NULL;

CREATE OR REPLACE TRIGGER trg_join_requests_updated_at
    BEFORE UPDATE ON chat_join_requests
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- I-5: Unique constraint on media.r2_key to prevent duplicate R2 references
ALTER TABLE media ADD CONSTRAINT media_r2_key_unique UNIQUE (r2_key);

-- I-6: messages.thread_id — add FK and partial index
ALTER TABLE messages ADD CONSTRAINT messages_thread_id_fkey
    FOREIGN KEY (thread_id) REFERENCES messages(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_messages_thread_id ON messages(thread_id) WHERE thread_id IS NOT NULL;

-- I-7: devices — add updated_at column and trigger
ALTER TABLE devices ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE OR REPLACE TRIGGER trg_devices_updated_at
    BEFORE UPDATE ON devices
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- C-2: media — add updated_at column and trigger (mutable table: processing_status changes)
ALTER TABLE media ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE OR REPLACE TRIGGER trg_media_updated_at
    BEFORE UPDATE ON media
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();
