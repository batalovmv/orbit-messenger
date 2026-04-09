-- Migration 037: Call rating (Phase 6 Stage 5)
-- Allows participants to rate a finished call 1-5 with an optional comment.
-- One rating per user per call, enforced via atomic UPDATE WHERE rated_by IS NULL.
--
-- Each service runs the migrator on startup, so every DDL statement here must
-- be idempotent — otherwise auth/calls/messaging/media crash after the first
-- service applies the migration.

ALTER TABLE calls ADD COLUMN IF NOT EXISTS rating         INT CHECK (rating IS NULL OR (rating >= 1 AND rating <= 5));
ALTER TABLE calls ADD COLUMN IF NOT EXISTS rating_comment TEXT;
ALTER TABLE calls ADD COLUMN IF NOT EXISTS rated_by       UUID REFERENCES users(id);
ALTER TABLE calls ADD COLUMN IF NOT EXISTS rated_at       TIMESTAMPTZ;

-- Analytics/reporting: quickly find rated calls and average scores.
CREATE INDEX IF NOT EXISTS idx_calls_rated ON calls (rated_at DESC) WHERE rating IS NOT NULL;
