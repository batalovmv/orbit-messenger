-- Ensure a saved-messages chat maps to only one user.

DELETE FROM saved_messages_lookup older
USING saved_messages_lookup newer
WHERE older.chat_id = newer.chat_id
  AND older.user_id > newer.user_id;

DO $$
BEGIN
    ALTER TABLE saved_messages_lookup
        ADD CONSTRAINT saved_messages_lookup_chat_id_key UNIQUE (chat_id);
EXCEPTION
    WHEN duplicate_object THEN
        NULL;
END $$;
