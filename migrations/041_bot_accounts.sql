-- Phase 8: Add account_type and username to users for bot/system identity support

ALTER TABLE users ADD COLUMN account_type TEXT NOT NULL DEFAULT 'human'
    CHECK (account_type IN ('human', 'bot', 'system'));

ALTER TABLE users ADD COLUMN username TEXT;

-- Unique index on username, allowing NULLs (humans may not have username)
CREATE UNIQUE INDEX idx_users_username ON users (username) WHERE username IS NOT NULL;

-- Index for filtering by account_type
CREATE INDEX idx_users_account_type ON users (account_type) WHERE account_type != 'human';
