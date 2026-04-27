# ADR 001 — No End-to-End Encryption

**Status:** ACCEPTED (rolled back 2026-04-16, migration 053).
**⚠ Do not revive without an explicit, documented decision.**

## Context

The original TZ (Phase 7) mandated Signal Protocol E2E between clients: per-device
identity keys, prekeys, sealed sender, key transparency log, disappearing messages.
The feature was partially built (migrations 046, 047, 050, 051 added
`compliance_keys`, `key_transparency_log`, `one_time_prekeys`, `user_keys`,
`chats.is_encrypted`, `chats.disappearing_timer`, `messages.encrypted_content`,
`messages.expires_at`, `media.is_encrypted`).

In practice E2E never reached production:
- Orbit is a **corporate** messenger — administration must retain access for
  compliance, audit, and offboarding. Sealed sender + per-device keys actively
  fight that requirement.
- Multi-device sync, search, server-side AI features (Live Translate, Smart
  Notifications, transcription) all need plaintext on the server.
- Operational cost of key transparency + prekey rotation was disproportionate
  for a 150-user installation.

## Decision

Use **AES-256-GCM at-rest only**, applied in the messaging-store layer. Key
material lives in the deployment KMS; the service holds a data-encryption key
loaded from env at boot. No client-side encryption, no sealed sender, no
disappearing-message timers.

The compliance model is explicit: **administration has full read access** to
all chats via the `compliance` and `superadmin` system roles
(see ADR 004).

## Consequences

- All E2E remnants dropped in migration `053_drop_e2e_remnants` (2026-04-16).
  Migrations 046/047/050/051 are intentionally absent — see `migrations/CHANGELOG.md`.
- Feature flag `e2e_dm_enabled` (seeded by mig 045) stays `false` permanently.
- Server-side features (translate cache, smart-notify classifier, Whisper
  transcription, full-text search) work because content is decryptable.
- Compliance audit (`audit_log`, `bot_audit_log`) can reference message
  content without key-escrow gymnastics.
- Trade-off accepted: compromise of the messaging-service host or its KMS key
  exposes message history. Mitigated by KMS rotation + WAL/PITR scope, not by
  cryptographic isolation from the operator.

**Do not reintroduce Signal Protocol or any per-device key system without
revisiting the corporate-access requirement first.**
