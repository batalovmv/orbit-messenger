-- Migration 037: Call rating (Phase 6 Stage 5)
-- Allows participants to rate a finished call 1-5 with an optional comment.
-- One rating per user per call, enforced via atomic UPDATE WHERE rated_by IS NULL.

ALTER TABLE calls ADD COLUMN rating         INT CHECK (rating IS NULL OR (rating >= 1 AND rating <= 5));
ALTER TABLE calls ADD COLUMN rating_comment TEXT;
ALTER TABLE calls ADD COLUMN rated_by       UUID REFERENCES users(id);
ALTER TABLE calls ADD COLUMN rated_at       TIMESTAMPTZ;

-- Analytics/reporting: quickly find rated calls and average scores.
CREATE INDEX idx_calls_rated ON calls (rated_at DESC) WHERE rating IS NOT NULL;
