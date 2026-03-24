-- Fix direct_chat_lookup.chat_id to cascade on chat deletion
ALTER TABLE direct_chat_lookup
    DROP CONSTRAINT IF EXISTS direct_chat_lookup_chat_id_fkey,
    ADD CONSTRAINT direct_chat_lookup_chat_id_fkey
        FOREIGN KEY (chat_id) REFERENCES chats(id) ON DELETE CASCADE;
