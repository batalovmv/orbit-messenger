-- Phase 8E.1: Extend bots with TG BotFather parity fields.
-- Adds privacy mode, group permissions, about text, inline placeholder, menu button.

ALTER TABLE bots
    ADD COLUMN is_privacy_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN can_join_groups BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN can_read_all_group_messages BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN about_text TEXT,
    ADD COLUMN inline_placeholder TEXT,
    ADD COLUMN menu_button JSONB;

-- Backfill about_text from description (TG about is shown on bot profile, limited to 120 chars).
UPDATE bots
SET about_text = LEFT(description, 120)
WHERE about_text IS NULL AND description IS NOT NULL AND description <> '';
