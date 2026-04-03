-- Phase 5: quiz poll explanation / solution payload

ALTER TABLE polls
    ADD COLUMN IF NOT EXISTS solution TEXT,
    ADD COLUMN IF NOT EXISTS solution_entities JSONB;
