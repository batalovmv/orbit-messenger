-- 053: Remove Phase 7 E2E encryption remnants
--
-- Phase 7 (Signal Protocol E2E) was reverted in favour of server-side at-rest
-- encryption (AES-256-GCM in the messaging store layer). The tables and columns
-- below were created by migrations 046, 047, 050, 051 and inline in 006/009
-- but are no longer referenced by any service code.
--
-- Safe to run: all columns have DEFAULT values, no application code reads them,
-- tables are empty (E2E was never shipped to production users).

-- ── Tables ────────────────────────────────────────────────────────────────
DROP TABLE IF EXISTS compliance_keys CASCADE;
DROP TABLE IF EXISTS key_transparency_log CASCADE;
DROP TABLE IF EXISTS one_time_prekeys CASCADE;
DROP TABLE IF EXISTS user_keys CASCADE;

-- ── Columns on chats ──────────────────────────────────────────────────────
ALTER TABLE chats DROP COLUMN IF EXISTS is_encrypted;
ALTER TABLE chats DROP COLUMN IF EXISTS disappearing_timer;

-- ── Columns on messages ───────────────────────────────────────────────────
ALTER TABLE messages DROP COLUMN IF EXISTS encrypted_content;
ALTER TABLE messages DROP COLUMN IF EXISTS expires_at;

-- ── Columns on media ──────────────────────────────────────────────────────
DROP INDEX IF EXISTS idx_media_is_encrypted;
ALTER TABLE media DROP COLUMN IF EXISTS is_encrypted;
