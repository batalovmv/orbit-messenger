-- Copyright (C) 2024 MST Corp. All rights reserved.
-- SPDX-License-Identifier: GPL-3.0-or-later
--
-- Migration 066: maintenance mode flag + audit log text-search support.
--
-- 1. Seed `maintenance_mode` row in feature_flags. The flag is the single
--    source of truth for the "технические работы" banner. Metadata holds:
--      {
--        "message":      "идут технические работы",
--        "block_writes": false,
--        "since":        "2026-04-27T...Z",
--        "updated_by":   "<actor uuid>"
--      }
--    enabled=false by default, so deploying this migration is a no-op for users.
--
-- 2. The audit log search uses ILIKE (small on-prem volume — see design memo).
--    pg_trgm GIN indexes are deferred until we observe slow queries on a
--    larger deployment. This migration just records the decision.

INSERT INTO feature_flags (key, enabled, description, metadata)
VALUES (
    'maintenance_mode',
    FALSE,
    'System-wide maintenance mode. When enabled the web client shows a banner; if metadata.block_writes=true the gateway also rejects mutating requests for non-superadmin users.',
    '{"message": "", "block_writes": false}'::jsonb
)
ON CONFLICT (key) DO NOTHING;
