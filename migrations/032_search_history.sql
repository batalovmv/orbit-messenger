-- Phase 4 debt: search history for recent queries
CREATE TABLE IF NOT EXISTS search_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    query TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT 'global',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, query)
);

CREATE INDEX IF NOT EXISTS idx_search_history_user_created
  ON search_history(user_id, created_at DESC);
