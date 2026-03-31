-- Migration 020: Security audit fixes (v2)
-- Fixes: default_permissions=255 → 15 for groups, permissions sentinel 0 → -1

-- Fix #4: Change default_permissions column default from 255 (AllPermissions) to 15 (DefaultGroupPermissions).
-- 255 grants CanDeleteMessages, CanBanUsers, CanInviteViaLink to regular members — not intended.
ALTER TABLE chats ALTER COLUMN default_permissions SET DEFAULT 15;

-- Data fix: correct existing group chats that inherited the wrong default.
UPDATE chats SET default_permissions = 15 WHERE type = 'group' AND default_permissions = 255;

-- Fix #3: Change permissions sentinel from 0 ("use defaults") to -1 ("unset").
-- This allows explicitly setting permissions=0 to revoke all capabilities from an admin.
ALTER TABLE chat_members ALTER COLUMN permissions SET DEFAULT -1;

-- Data fix: migrate existing unset permissions (0) to -1.
-- Only for members whose permissions were never explicitly customized.
UPDATE chat_members SET permissions = -1 WHERE permissions = 0;
