-- Migrate from global messages_seq to per-chat sequence numbers.
-- Adds next_sequence_number to chats table, initialized from existing data.

-- Step 1: Add the counter column to chats
ALTER TABLE chats ADD COLUMN IF NOT EXISTS next_sequence_number BIGINT NOT NULL DEFAULT 1;

-- Step 2: Initialize from existing messages
UPDATE chats c SET next_sequence_number = COALESCE(
    (SELECT MAX(m.sequence_number) + 1 FROM messages m WHERE m.chat_id = c.id),
    1
);

-- Step 3: Remove the DEFAULT nextval from messages.sequence_number
-- New messages will get sequence_number from application code (per-chat atomic increment)
ALTER TABLE messages ALTER COLUMN sequence_number DROP DEFAULT;

-- Step 4: Drop the global sequence (no longer needed)
DROP SEQUENCE IF EXISTS messages_seq;
