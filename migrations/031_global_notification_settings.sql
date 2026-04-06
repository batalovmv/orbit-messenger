-- Phase 5 debt: global notification settings per chat type
ALTER TABLE user_settings
  ADD COLUMN IF NOT EXISTS notify_users_muted BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS notify_groups_muted BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS notify_channels_muted BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS notify_users_preview BOOLEAN NOT NULL DEFAULT true,
  ADD COLUMN IF NOT EXISTS notify_groups_preview BOOLEAN NOT NULL DEFAULT true,
  ADD COLUMN IF NOT EXISTS notify_channels_preview BOOLEAN NOT NULL DEFAULT true;
