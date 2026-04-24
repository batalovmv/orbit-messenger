-- 059_chat_drafts.sql: Add draft text to chat_members for per-user per-chat drafts
ALTER TABLE chat_members ADD COLUMN draft_text TEXT;
ALTER TABLE chat_members ADD COLUMN draft_date TIMESTAMPTZ;
