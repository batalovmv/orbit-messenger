-- Remove channel support: Orbit is a corporate messenger, only DM + group chats needed

-- Delete channel notification settings (may not cascade)
DELETE FROM notification_settings WHERE chat_id IN (SELECT id FROM chats WHERE type = 'channel');

-- Delete channel data (CASCADE handles messages, chat_members, invite_links, join_requests)
DELETE FROM chats WHERE type = 'channel';

-- Remove channel from type constraint
ALTER TABLE chats DROP CONSTRAINT IF EXISTS chats_type_check;
ALTER TABLE chats ADD CONSTRAINT chats_type_check CHECK (type IN ('direct', 'group'));

-- Drop channel-only columns
ALTER TABLE chats DROP COLUMN IF EXISTS is_signatures;

-- Drop channel notification columns from user_settings
ALTER TABLE user_settings DROP COLUMN IF EXISTS notify_channels_muted;
ALTER TABLE user_settings DROP COLUMN IF EXISTS notify_channels_preview;
