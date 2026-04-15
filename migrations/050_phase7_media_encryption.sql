-- Phase 7.1: Media encryption support
--
-- When `is_encrypted = true`:
--   * The R2 object contains opaque AES-256-GCM ciphertext (nonce + tag inside
--     the envelope sent via encrypted message; server never sees the key).
--   * mime_type is forced to 'application/octet-stream'.
--   * thumbnail_r2_key, medium_r2_key, width, height, duration_seconds and
--     waveform_data stay NULL because the server cannot inspect plaintext.
--   * Media service MUST skip every processing step (EXIF strip, WebP
--     conversion, frame extraction, waveform analysis) — upload goes straight
--     to R2 as-is.
--
-- Backwards compatible: default = false. Existing Phase 3 media rows remain
-- unencrypted and keep the standard processing pipeline.

ALTER TABLE media
  ADD COLUMN is_encrypted BOOLEAN NOT NULL DEFAULT false;

-- Optional hint for future-media queries — keeps the column cheap to filter on
-- when we eventually add admin dashboards that count encrypted storage usage.
CREATE INDEX IF NOT EXISTS idx_media_is_encrypted ON media(is_encrypted)
  WHERE is_encrypted = true;
