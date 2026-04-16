-- Phase 8A: AI service usage accounting.
-- Idempotent: safe to run multiple times.

CREATE TABLE IF NOT EXISTS ai_usage (
    id SERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint TEXT NOT NULL,
    model TEXT NOT NULL,
    input_tokens INT NOT NULL DEFAULT 0,
    output_tokens INT NOT NULL DEFAULT 0,
    cost_cents INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_usage_user_created
    ON ai_usage(user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ai_usage_endpoint_created
    ON ai_usage(endpoint, created_at DESC);
