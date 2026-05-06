-- 071_call_recordings.sql
-- Call recording metadata table (NEXT-SESSION-PLAN.md Phase D, decisions
-- #1, #2, 2026-05-06). Audio-only, SFU-only — see ADR notes:
--   * Recording is mandatory (compliance baseline) — there is no opt-out
--     flag on this table.
--   * P2P calls do NOT flow through the SFU and therefore have no row
--     here. The `participant_user_id` column nails down "this is one row
--     per participant per call", not per track.
--   * `s3_key` is the storage key under R2_BACKUP_BUCKET (or a
--     dedicated bucket — TBD by the recording publisher). The decryption
--     key is wrapped with the master KMS key referenced via
--     `encryption_key_id`.
--   * `ended_at` is set by the recording publisher's flush hook. NULL
--     means the upload is still in flight or failed; the GC sweep in D5
--     filters on `ended_at IS NOT NULL`.
--   * Retention is 90 days from `ended_at` (decision #2). The retention
--     index supports the GC sweep.

CREATE TABLE IF NOT EXISTS call_recordings (
    id                   uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    call_id              uuid        NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    participant_user_id  uuid        NOT NULL REFERENCES users(id),
    s3_key               text        NOT NULL,
    encryption_key_id    text        NOT NULL,
    started_at           timestamptz NOT NULL,
    ended_at             timestamptz,
    duration_sec         integer,
    size_bytes           bigint,
    upload_failed        boolean     NOT NULL DEFAULT false,
    created_at           timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_call_recordings_call
    ON call_recordings (call_id, started_at);

-- Partial index keyed on `ended_at` so the D5 retention sweep doesn't
-- fan out across in-flight uploads. The WHERE clause keeps the index
-- footprint tiny on a tenant with thousands of active calls.
CREATE INDEX IF NOT EXISTS idx_call_recordings_retention
    ON call_recordings (ended_at)
    WHERE ended_at IS NOT NULL;
