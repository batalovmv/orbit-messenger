-- Persist per-user translation preferences so "Show Translate Button" and
-- "Translate Entire Chats" toggles survive page reloads.

ALTER TABLE user_settings
  ADD COLUMN IF NOT EXISTS can_translate BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE user_settings
  ADD COLUMN IF NOT EXISTS can_translate_chats BOOLEAN NOT NULL DEFAULT FALSE;
