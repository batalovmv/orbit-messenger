-- ============================================================
-- Phase 2: Groups, Channels, Permissions, Invite Links
-- ============================================================

-- 1. chat_members: add permissions bitmask + custom title for admins
ALTER TABLE chat_members
  ADD COLUMN IF NOT EXISTS permissions BIGINT DEFAULT 0,
  ADD COLUMN IF NOT EXISTS custom_title TEXT;

-- 2. chats: add group/channel settings
ALTER TABLE chats
  ADD COLUMN IF NOT EXISTS default_permissions BIGINT DEFAULT 255,
  ADD COLUMN IF NOT EXISTS slow_mode_seconds INT DEFAULT 0,
  ADD COLUMN IF NOT EXISTS is_signatures BOOLEAN DEFAULT false;

-- 3. Invite links table
CREATE TABLE IF NOT EXISTS chat_invite_links (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
  creator_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  hash TEXT NOT NULL UNIQUE,
  title TEXT,
  expire_at TIMESTAMPTZ,
  usage_limit INT DEFAULT 0,
  usage_count INT NOT NULL DEFAULT 0,
  requires_approval BOOLEAN NOT NULL DEFAULT false,
  is_revoked BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_invite_links_chat ON chat_invite_links(chat_id);
CREATE INDEX IF NOT EXISTS idx_invite_links_hash ON chat_invite_links(hash);

-- 4. Join requests table
CREATE TABLE IF NOT EXISTS chat_join_requests (
  chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  message TEXT,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
  reviewed_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (chat_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_join_requests_status ON chat_join_requests(chat_id, status);

-- 5. Set channels to read-only by default (default_permissions = 0)
UPDATE chats SET default_permissions = 0 WHERE type = 'channel';
