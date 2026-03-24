-- Fix missing foreign keys, ON DELETE behaviors, and indexes

-- 1. devices: add index for user_id lookups (FK exists but no index)
CREATE INDEX IF NOT EXISTS idx_devices_user ON devices(user_id);

-- 2. invites: add ON DELETE SET NULL for created_by and used_by
ALTER TABLE invites
    DROP CONSTRAINT IF EXISTS invites_created_by_fkey,
    ADD CONSTRAINT invites_created_by_fkey
        FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE invites
    DROP CONSTRAINT IF EXISTS invites_used_by_fkey,
    ADD CONSTRAINT invites_used_by_fkey
        FOREIGN KEY (used_by) REFERENCES users(id) ON DELETE SET NULL;

-- 3. chat_members: add FK for last_read_message_id
ALTER TABLE chat_members
    ADD CONSTRAINT chat_members_last_read_message_id_fkey
        FOREIGN KEY (last_read_message_id) REFERENCES messages(id) ON DELETE SET NULL;

-- 4. direct_chat_lookup: add FK for user1_id and user2_id
ALTER TABLE direct_chat_lookup
    ADD CONSTRAINT direct_chat_lookup_user1_fkey
        FOREIGN KEY (user1_id) REFERENCES users(id) ON DELETE CASCADE,
    ADD CONSTRAINT direct_chat_lookup_user2_fkey
        FOREIGN KEY (user2_id) REFERENCES users(id) ON DELETE CASCADE;

-- 5. messages: add ON DELETE SET NULL for sender_id and forwarded_from
ALTER TABLE messages
    DROP CONSTRAINT IF EXISTS messages_sender_id_fkey,
    ADD CONSTRAINT messages_sender_id_fkey
        FOREIGN KEY (sender_id) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE messages
    DROP CONSTRAINT IF EXISTS messages_forwarded_from_fkey,
    ADD CONSTRAINT messages_forwarded_from_fkey
        FOREIGN KEY (forwarded_from) REFERENCES users(id) ON DELETE SET NULL;
