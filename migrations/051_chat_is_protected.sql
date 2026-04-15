-- Phase 7.1 follow-up: mark a chat as "protected" so the frontend
-- disables forward/copy/save on messages inside it. Enforcement is
-- client-side only for now; the backend just stores the bit and echoes
-- it back in chat responses (and in the chat.updated NATS event).

ALTER TABLE chats
  ADD COLUMN IF NOT EXISTS is_protected BOOLEAN NOT NULL DEFAULT false;
