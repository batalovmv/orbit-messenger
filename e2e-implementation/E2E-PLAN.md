# Orbit Phase 7 — E2E Encryption Backend Implementation Plan

**Created**: 2026-04-10
**Branch**: `feat/phase-7-e2e` (create from master HEAD before starting)
**Source of truth**: this file + `CLAUDE.md` for project conventions
**Progress log**: `e2e-implementation/PROGRESS.md`
**Total tasks**: 28 (25 implementation + 3 checkpoints)

---

## Scope of this plan

This plan covers **backend-only** implementation for Phase 7 E2E encryption:
- Database migrations (key tables, feature flags)
- Auth service: key server endpoints, device lifecycle
- Messaging service: encrypted message storage, E2E-aware delivery
- Gateway service: push notification changes for E2E chats
- Design documentation

**NOT in scope** (will be done interactively in separate sessions):
- Frontend Signal Protocol implementation (crypto worker, X3DH, Double Ratchet)
- Frontend UI (E2E badge, Safety Numbers, key verification)
- Escrow/compliance flow (schema-ready only, flow deferred)
- Group E2E (Sender Keys) — Phase 7F
- Sealed Sender — not planned for Orbit

---

## Architecture decisions (pre-resolved — do NOT deviate)

1. **E2E scope**: DM only. Groups, bots, AI, integrations = non-E2E.
2. **Protocol**: Signal Protocol (X3DH + Double Ratchet). Server is key server only — never sees plaintext.
3. **Multi-device**: sender encrypts separately for each recipient device + own other devices. Per-device ciphertext in envelope.
4. **Key types**: Ed25519 for identity keys (signing), X25519 for prekeys (ECDH). Raw bytes in DB (BYTEA), base64url in JSON API.
5. **Envelope**: JSON with version, sender_device_id, per-recipient ciphertext blobs. Server stores as BYTEA, doesn't parse inner ciphertext.
6. **Escrow**: `compliance_keys` table created but NOT populated. Escrow flow is a separate future feature.
7. **Old messages**: NOT re-encrypted. E2E starts from activation moment.
8. **Feature flags**: `feature_flags` table gates E2E rollout. Default OFF.
9. **Push**: E2E chats get "New message" without text preview.
10. **Search**: E2E chats excluded from Meilisearch indexing. Client-side search deferred.
11. **Disappearing messages**: server-side cleanup via cron using existing `expires_at` column.

---

## Principles

1. **Orbit is a corporate messenger for 150 trusted employees.** E2E protects DM content from server admins, not from nation-state attackers. Pragmatic security > theoretical perfection.
2. **Server never decrypts.** The key server stores public keys and delivers ciphertext. Any code path where the server could read plaintext of an E2E message is a critical bug.
3. **Backward compatibility.** Non-E2E chats continue working unchanged. E2E is opt-in, gated by feature flags. No breaking changes to existing API contracts.
4. **Project conventions in CLAUDE.md are non-negotiable.** handler/service/store separation, fn-field mocks, parameterized SQL, `response.*` helpers, `apperror.*` errors, `pkg/validator` for input validation.
5. **Minimal scope per task.** Do not add "bonus" improvements. Do not refactor adjacent code. Stick to the declared file scope.

---

## Non-negotiable operational rules

You are executing this plan in an unsupervised long-running session. Follow these rules exactly.

### Pre-start setup

Before TASK-01, execute these steps:

```bash
# 1. Create feature branch from master HEAD
git checkout master
git pull origin master 2>/dev/null || true
git checkout -b feat/phase-7-e2e

# 2. Verify clean working tree
git status

# 3. Log the starting commit
echo "Base commit: $(git rev-parse --short HEAD)" >> e2e-implementation/PROGRESS.md
```

If the working tree is dirty, stash changes and log it in PROGRESS.md before proceeding.

### Workflow — one task at a time

For each task in the order listed below:

1. **Read the task block fully.** Do NOT skip to the next task until you understand the current one.
2. **Verify preconditions.** Working tree must be clean (`git status` empty). If dirty, stash or commit anything unexpected before starting (log it in PROGRESS.md first).
3. **Implement the change.** Edit only the files listed in the task scope. Do not touch adjacent code for "bonus" cleanup.
4. **Run the self-check.** Execute the exact command from the task's `Self-check` section. This is a quick sanity check before the test gate.
5. **Run the test gate.** Execute the exact command from the task's `Test gate` section. If it passes, continue. If it fails, follow the `On failure` clause.
6. **Commit the change.** Stage ONLY the files you edited (never `git add -A`). Use the commit message template from the task.
7. **Append to progress log.** Add a line to `e2e-implementation/PROGRESS.md` with: timestamp, task ID, status (DONE / SKIPPED / FAILED), commit hash, one-line note.
8. **Move to the next task.** Do not stop, do not summarise in chat, do not ask for confirmation.

### Hard rules — violating these will cause real damage

- **NEVER** use `git push`, `git push --force`, `git rebase`, `git commit --amend`, `git reset --hard`, `git checkout .`, `git clean -f`, or any destructive git operation. Local commits only. The operator handles push.
- **NEVER** skip the test gate. A commit without passing tests is a broken commit.
- **NEVER** use `git add -A` or `git add .`. Always `git add <specific files>`.
- **NEVER** edit files outside the task's declared scope. If you notice a related issue outside scope, log it in PROGRESS.md as "OBSERVED:" and continue.
- **NEVER** edit `CLAUDE.md`, `PHASES.md`, or any existing documentation files except those explicitly listed in task scope.
- **NEVER** use `AskUserQuestion` or stop to ask for clarification. When uncertain, pick a reasonable default consistent with the Architecture decisions above and log your decision in PROGRESS.md as "DECISION:".
- **NEVER** introduce new external Go dependencies (`go get <new>`) unless explicitly mentioned in the task block. All new code uses stdlib + existing deps (pgx, redis, fiber, uuid, nats).
- **NEVER** run migrations against any real database. Migration fixes are file-level only.
- **NEVER** commit secrets. No API keys, no passwords, no tokens in files.
- **NEVER** run `docker compose up` or `docker compose build`. Per-task verification uses `go build` and `go test` only.
- **ALWAYS** use parameterized SQL (`$1, $2`). Never `fmt.Sprintf` in queries.
- **ALWAYS** use `response.JSON`, `response.Error`, `response.Paginated` — never raw `c.JSON()`.
- **ALWAYS** use `apperror.*` for error returns from service layer.
- **ALWAYS** use the fn-field mock pattern for test mocks (see CLAUDE.md "Тесты" section).

### Self-check pattern

Every task has a `Self-check` section with verification commands. These run BEFORE the test gate and catch structural errors early. The self-check is mandatory — if it fails, fix the issue before running the test gate.

Typical self-checks:
- `go build ./...` — compilation check
- `go vet ./...` — static analysis
- `grep -c "functionName" file.go` — verify expected code is present
- `wc -l file.go` — verify file isn't suspiciously short or long

### Soft rules

- **Prefer** Edit over Write for existing files.
- **Prefer** Grep/Glob over Agent for simple lookups.
- **Prefer** minimal test runs (one package) over full-service runs when the change is localised.
- **Prefer** one commit per task. CHECKPOINT tasks have no commit.

### Stop conditions

Halt only on these conditions:
1. **Go toolchain broken** — `go build` returns "go: not found". Log "HALT: toolchain" and stop.
2. **Git broken** — `git commit` returns fatal error. Log "HALT: git" and stop.
3. **5 consecutive SKIPPED or FAILED tasks** — something systemic is wrong. Log "HALT: cascade failure" and stop.

Everything else is a SKIP, not a halt.

### On failure — per-task clause

Each task has an `On failure` line. Default: **rollback and skip** — `git checkout -- <scope files>`, log "SKIPPED: <reason>", move to next task.

### Progress log format

```
## 2026-04-10T14:00:00Z TASK-01 DONE a1b2c3d
Created docs/SIGNAL_PROTOCOL.md with crypto design decisions. No tests needed.
```

Keep entries terse (2-4 lines). No decorative markdown. No emojis.

---

# TASKS

Ordered by dependency: documentation → schema → auth key server → messaging E2E → gateway → disappearing messages. Each phase builds on the previous.

---

## Phase 0 — Documentation & Schema

---

## TASK-01 — Create Signal Protocol design document

**Scope**: `docs/SIGNAL_PROTOCOL.md` (new file)

### Why
Referenced by PHASES.md and TZ-ORBIT-MESSENGER.md but doesn't exist. Backend and frontend need a shared reference for envelope format, key lifecycle, and trust model.

### Change
Create `docs/SIGNAL_PROTOCOL.md` with the following sections (use the exact content below as a starting template, then fill in details):

```markdown
# Orbit Signal Protocol Design

## Trust Model
- Server stores public keys and delivers ciphertext. Server never sees plaintext of E2E messages.
- "Zero-Knowledge" means: server admin cannot read DM content. Server CAN see: chat_id, sender_id, timestamps, sequence_number, delivery state, message size.
- Compliance/Escrow: deferred. Schema ready (compliance_keys table), flow not implemented in Phase 7.
- Sealed Sender: not implemented. sender_id in metadata is acceptable for corporate audit.

## Scope
- Phase 7: E2E for DM only.
- Groups, channels, bots, AI, integrations: non-E2E.
- Old messages: not re-encrypted retroactively.

## Key Types
| Key | Algorithm | Size | Rotation |
|-----|-----------|------|----------|
| Identity Key | Ed25519 | 32B pub / 64B priv | Once per device (permanent) |
| Signed PreKey | X25519 | 32B pub / 32B priv | Weekly |
| One-Time PreKey | X25519 | 32B pub / 32B priv | Consumed on first use, replenished in batches of 100 |

## Key Lifecycle
1. Device registration: generate Identity Key Pair (Ed25519) + Signed PreKey (X25519) + 100 One-Time PreKeys (X25519)
2. Upload public parts to server via POST /api/v1/keys/*
3. Session init (X3DH): requester fetches bundle, computes shared secret
4. Message exchange: Double Ratchet — new symmetric key per message
5. Signed PreKey rotation: weekly, old signed prekey kept for 48h overlap
6. One-Time PreKey replenishment: when count < 20, client uploads new batch of 100

## Device Model
- Each browser/app instance = one device with unique device_id (UUID)
- Session (JWT) is bound to device_id
- Device has its own key material (identity key, signed prekey, one-time prekeys)
- On logout: session deleted, device keys optionally preserved for re-login
- On deactivation: all sessions and device keys purged

## Encrypted Message Envelope
Stored in messages.encrypted_content (BYTEA), transmitted as JSON:
```json
{
  "v": 1,
  "sender_device_id": "uuid-string",
  "devices": {
    "recipient-device-id-1": {
      "type": 1,
      "body": "base64url-encoded-ciphertext"
    },
    "recipient-device-id-2": {
      "type": 1,
      "body": "base64url-encoded-ciphertext"
    }
  }
}
```
- `v`: envelope version (always 1)
- `type`: 1 = PreKeyWhisperMessage (session init), 2 = WhisperMessage (normal)
- `body`: opaque ciphertext blob — server doesn't parse this
- Each entry is for one recipient device (includes sender's other devices)

## API Key Format
All key fields in JSON API use base64url encoding (RFC 4648 §5, no padding).
In database (BYTEA): raw bytes.

## Push Notifications
E2E chats (is_encrypted=true): push body = "Новое сообщение", no content preview.
Non-E2E chats: unchanged (plaintext preview up to 100 chars).

## Search
E2E chats excluded from Meilisearch indexing.
Client-side search: deferred to frontend implementation.

## Disappearing Messages
Server-side: cron job deletes messages where expires_at < NOW().
Client-side: local cleanup on timer (frontend implementation).
Timer options: 24h / 7d / 30d / Off.
Set per-chat via PUT /api/v1/chats/:id/disappearing.

## Feature Flags
feature_flags table: key TEXT PK, enabled BOOLEAN, metadata JSONB.
Flags: e2e_dm_enabled (default false).
Check in chat creation and message send paths.

## Rollout
1. e2e_dm_enabled=false — E2E code deployed but inactive
2. e2e_dm_enabled=true — new DMs can be created as E2E (opt-in by client)
3. Future: default E2E for new DMs, then group E2E
```

### Self-check
```bash
test -f docs/SIGNAL_PROTOCOL.md && wc -l docs/SIGNAL_PROTOCOL.md
# Expected: 80-150 lines
```

### Test gate
```bash
# No code to compile — verify file exists and is valid markdown
head -1 docs/SIGNAL_PROTOCOL.md | grep -q "# Orbit Signal Protocol"
```

### Commit message
`docs(e2e): add Signal Protocol design document for Phase 7`

---

## TASK-02 — Feature flags migration

**Scope**: `migrations/041_feature_flags.sql` (new file)

### Why
E2E rollout needs a server-side feature flag system. Currently no feature_flags table exists.

### Change
Create migration file:

```sql
CREATE TABLE IF NOT EXISTS feature_flags (
    key TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT false,
    description TEXT,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- E2E DM feature flag — disabled by default
INSERT INTO feature_flags (key, enabled, description)
VALUES ('e2e_dm_enabled', false, 'Enable E2E encryption for new DM chats')
ON CONFLICT (key) DO NOTHING;
```

### Self-check
```bash
# Verify migration number is correct (next after 040)
ls migrations/ | sort | tail -3
# Verify SQL syntax is valid
grep -c "CREATE TABLE" migrations/041_feature_flags.sql
# Expected: 1
```

### Test gate
```bash
# SQL syntax check — just verify the file is parseable
grep -q "PRIMARY KEY" migrations/041_feature_flags.sql && echo "OK"
```

### Commit message
`feat(db): add feature_flags table for E2E rollout gating`

---

## TASK-03 — E2E key management migrations

**Scope**: `migrations/042_e2e_keys.sql` (new file)

### Why
Signal Protocol requires server-side storage for public keys: identity keys, signed prekeys, one-time prekeys, and a transparency log for key change auditing.

### Change
Create migration file with exactly these 4 tables:

```sql
-- User device keys (identity key + signed prekey per device)
CREATE TABLE IF NOT EXISTS user_keys (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id UUID NOT NULL,
    identity_key BYTEA NOT NULL,
    signed_prekey BYTEA NOT NULL,
    signed_prekey_signature BYTEA NOT NULL,
    signed_prekey_id INT NOT NULL,
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, device_id)
);

CREATE INDEX IF NOT EXISTS idx_user_keys_user_id ON user_keys(user_id);

-- One-time prekeys (consumed atomically on session init)
CREATE TABLE IF NOT EXISTS one_time_prekeys (
    id SERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id UUID NOT NULL,
    key_id INT NOT NULL,
    public_key BYTEA NOT NULL,
    used BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_otp_user_device ON one_time_prekeys(user_id, device_id, used);

-- Key transparency log (append-only audit of key changes)
CREATE TABLE IF NOT EXISTS key_transparency_log (
    id SERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    public_key_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ktl_user ON key_transparency_log(user_id);

-- Compliance keys (escrow, schema-only — flow not implemented yet)
CREATE TABLE IF NOT EXISTS compliance_keys (
    chat_id UUID NOT NULL,
    user_id UUID NOT NULL,
    device_id UUID NOT NULL,
    encrypted_session_key BYTEA NOT NULL,
    session_version INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chat_id, user_id, device_id, session_version)
);
```

Key validation rules for TASK self-check:
- `identity_key`: Ed25519 public key = exactly 32 bytes. Enforced in Go code, not SQL.
- `signed_prekey`: X25519 public key = exactly 32 bytes. Enforced in Go code.
- `signed_prekey_signature`: Ed25519 signature = exactly 64 bytes. Enforced in Go code.
- `one_time_prekeys.public_key`: X25519 public key = exactly 32 bytes.
- `event_type` values: `identity_key_registered`, `signed_prekey_rotated`, `prekeys_uploaded`, `device_revoked`.

### Self-check
```bash
# Verify 4 CREATE TABLE statements
grep -c "CREATE TABLE" migrations/042_e2e_keys.sql
# Expected: 4

# Verify all tables reference users(id)
grep -c "REFERENCES users(id)" migrations/042_e2e_keys.sql
# Expected: 3 (compliance_keys intentionally has no FK to allow cross-service flexibility)

# Verify file numbering
ls migrations/04*.sql
```

### Test gate
```bash
grep -q "PRIMARY KEY (user_id, device_id)" migrations/042_e2e_keys.sql && \
grep -q "one_time_prekeys" migrations/042_e2e_keys.sql && \
grep -q "key_transparency_log" migrations/042_e2e_keys.sql && \
grep -q "compliance_keys" migrations/042_e2e_keys.sql && \
echo "ALL TABLES PRESENT"
```

### Commit message
`feat(db): add E2E key management tables (user_keys, prekeys, transparency_log, compliance_keys)`

---

## Phase 1 — Auth Key Server

---

## TASK-04 — Auth key models

**Scope**: `services/auth/internal/model/models.go` (edit existing)

### Why
Auth service needs model structs for key management entities.

### Change
Add the following structs to `services/auth/internal/model/models.go` AFTER the existing `Invite` struct:

```go
// E2E Key Management

type UserDeviceKeys struct {
	UserID               uuid.UUID `json:"user_id"`
	DeviceID             uuid.UUID `json:"device_id"`
	IdentityKey          []byte    `json:"identity_key"`          // Ed25519 public, 32 bytes
	SignedPreKey         []byte    `json:"signed_prekey"`         // X25519 public, 32 bytes
	SignedPreKeySignature []byte   `json:"signed_prekey_signature"` // Ed25519 sig, 64 bytes
	SignedPreKeyID       int       `json:"signed_prekey_id"`
	UploadedAt           time.Time `json:"uploaded_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type OneTimePreKey struct {
	ID        int       `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	DeviceID  uuid.UUID `json:"device_id"`
	KeyID     int       `json:"key_id"`
	PublicKey []byte    `json:"public_key"` // X25519 public, 32 bytes
	Used      bool      `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

type KeyBundle struct {
	IdentityKey          []byte `json:"identity_key"`
	SignedPreKey         []byte `json:"signed_prekey"`
	SignedPreKeySignature []byte `json:"signed_prekey_signature"`
	SignedPreKeyID       int    `json:"signed_prekey_id"`
	OneTimePreKey        []byte `json:"one_time_prekey,omitempty"` // nil if exhausted
	OneTimePreKeyID      *int   `json:"one_time_prekey_id,omitempty"`
	DeviceID             uuid.UUID `json:"device_id"`
}

type KeyTransparencyEntry struct {
	ID            int       `json:"id"`
	UserID        uuid.UUID `json:"user_id"`
	DeviceID      uuid.UUID `json:"device_id"`
	EventType     string    `json:"event_type"`
	PublicKeyHash string    `json:"public_key_hash"`
	CreatedAt     time.Time `json:"created_at"`
}
```

Also add at the top of the file, before the `User` struct, ensure the `encoding/base64` import is NOT added (we use raw bytes, encoding is in handler).

### Self-check
```bash
cd services/auth && go build ./...
# Must compile without errors

grep -c "type UserDeviceKeys struct" internal/model/models.go
# Expected: 1

grep -c "type KeyBundle struct" internal/model/models.go
# Expected: 1
```

### Test gate
```bash
cd services/auth && go build ./... && go vet ./...
```

### Commit message
`feat(auth): add E2E key management model structs`

---

## TASK-05 — Auth key store

**Scope**: `services/auth/internal/store/key_store.go` (new file)

### Why
CRUD operations for user_keys table (identity keys and signed prekeys per device).

### Change
Create `services/auth/internal/store/key_store.go` with the following interface and implementation:

```go
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/auth/internal/model"
)

type KeyStore interface {
	Upsert(ctx context.Context, keys *model.UserDeviceKeys) error
	GetByUserAndDevice(ctx context.Context, userID, deviceID uuid.UUID) (*model.UserDeviceKeys, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]model.UserDeviceKeys, error)
	DeleteByDevice(ctx context.Context, userID, deviceID uuid.UUID) error
	GetIdentityKey(ctx context.Context, userID uuid.UUID) ([]byte, error)
}
```

Implementation notes:
- `Upsert`: `INSERT INTO user_keys (...) VALUES (...) ON CONFLICT (user_id, device_id) DO UPDATE SET signed_prekey=$4, signed_prekey_signature=$5, signed_prekey_id=$6, updated_at=NOW()`
- `GetByUserAndDevice`: standard SELECT with `WHERE user_id=$1 AND device_id=$2`
- `ListByUser`: `SELECT ... FROM user_keys WHERE user_id=$1 ORDER BY uploaded_at DESC`
- `DeleteByDevice`: `DELETE FROM user_keys WHERE user_id=$1 AND device_id=$2`
- `GetIdentityKey`: `SELECT identity_key FROM user_keys WHERE user_id=$1 LIMIT 1` (returns key from any device — identity key is the same across devices for verification purposes)
- Use `pgx.ErrNoRows` → return `nil, nil` pattern (same as session_store.go)

### Self-check
```bash
cd services/auth && go build ./...

# Verify interface has exactly 5 methods
grep -c "func.*KeyStore" internal/store/key_store.go
# Expected: at least 5 (interface methods) + 5 (implementations) = 10+ lines with "KeyStore" or method names

# Verify parameterized SQL
grep -c '\$[0-9]' internal/store/key_store.go
# Expected: > 0 (parameterized queries)

# Verify NO fmt.Sprintf in queries
grep -c 'Sprintf.*SELECT\|Sprintf.*INSERT\|Sprintf.*DELETE' internal/store/key_store.go
# Expected: 0
```

### Test gate
```bash
cd services/auth && go build ./... && go vet ./...
```

### Commit message
`feat(auth): add KeyStore for E2E identity and signed prekey CRUD`

---

## TASK-06 — Auth prekey store

**Scope**: `services/auth/internal/store/prekey_store.go` (new file)

### Why
CRUD operations for one_time_prekeys table. Critical: consumption must be atomic (one prekey per session init, no double-use).

### Change
Create `services/auth/internal/store/prekey_store.go` with the following interface and implementation:

```go
type PreKeyStore interface {
	UploadBatch(ctx context.Context, userID, deviceID uuid.UUID, keys []model.OneTimePreKey) (int, error)
	ConsumeOne(ctx context.Context, userID uuid.UUID) (*model.OneTimePreKey, error)
	CountRemaining(ctx context.Context, userID uuid.UUID) (int, error)
	DeleteByDevice(ctx context.Context, userID, deviceID uuid.UUID) error
}
```

Implementation notes:
- `UploadBatch`: batch INSERT using `pgx.Batch` or multi-row INSERT. Return count of inserted keys. Limit batch size to 100 max — if `len(keys) > 100`, return error.
- `ConsumeOne`: **ATOMIC** — use `UPDATE one_time_prekeys SET used = true WHERE id = (SELECT id FROM one_time_prekeys WHERE user_id = $1 AND used = false ORDER BY id ASC LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING id, user_id, device_id, key_id, public_key`. Returns `nil, nil` if no prekeys available.
- `CountRemaining`: `SELECT COUNT(*) FROM one_time_prekeys WHERE user_id = $1 AND used = false`
- `DeleteByDevice`: `DELETE FROM one_time_prekeys WHERE user_id = $1 AND device_id = $2`

**Critical**: `ConsumeOne` MUST be atomic. The `FOR UPDATE SKIP LOCKED` prevents race conditions when two sessions init simultaneously. Do NOT use check-then-act (SELECT then UPDATE).

### Self-check
```bash
cd services/auth && go build ./...

# Verify atomic consume pattern
grep -q "SKIP LOCKED\|FOR UPDATE" internal/store/prekey_store.go
# Must match — atomic consumption is mandatory

# Verify batch limit
grep -q "100" internal/store/prekey_store.go
# Must match — batch size limit
```

### Test gate
```bash
cd services/auth && go build ./... && go vet ./...
```

### Commit message
`feat(auth): add PreKeyStore with atomic one-time prekey consumption`

---

## TASK-07 — Auth transparency log store

**Scope**: `services/auth/internal/store/transparency_store.go` (new file)

### Why
Append-only log of key change events for auditing and client-side verification.

### Change
Create `services/auth/internal/store/transparency_store.go`:

```go
type TransparencyStore interface {
	Append(ctx context.Context, entry *model.KeyTransparencyEntry) error
	ListByUser(ctx context.Context, userID uuid.UUID, limit int) ([]model.KeyTransparencyEntry, error)
}
```

Implementation notes:
- `Append`: simple INSERT. Never update or delete entries.
- `ListByUser`: `SELECT ... WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`. Default limit 100 if limit <= 0.

### Self-check
```bash
cd services/auth && go build ./...
grep -c "INSERT INTO key_transparency_log" internal/store/transparency_store.go
# Expected: 1
```

### Test gate
```bash
cd services/auth && go build ./... && go vet ./...
```

### Commit message
`feat(auth): add TransparencyStore for key change audit log`

---

## TASK-08 — Auth key service

**Scope**: `services/auth/internal/service/key_service.go` (new file)

### Why
Business logic for key management: validation, bundle assembly, transparency logging.

### Change
Create `services/auth/internal/service/key_service.go`:

```go
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/auth/internal/model"
	"github.com/mst-corp/orbit/services/auth/internal/store"
)

type KeyService struct {
	keys         store.KeyStore
	prekeys      store.PreKeyStore
	transparency store.TransparencyStore
}

func NewKeyService(keys store.KeyStore, prekeys store.PreKeyStore, transparency store.TransparencyStore) *KeyService {
	return &KeyService{keys: keys, prekeys: prekeys, transparency: transparency}
}
```

Methods to implement:

**`RegisterDeviceKeys(ctx, userID, deviceID uuid.UUID, identityKey, signedPreKey, signedPreKeySig []byte, signedPreKeyID int) error`**
- Validate key sizes: identityKey must be exactly 32 bytes, signedPreKey must be exactly 32 bytes, signedPreKeySig must be exactly 64 bytes. Return `apperror.BadRequest` on invalid sizes.
- Call `keys.Upsert()`
- Log to transparency: event_type = `identity_key_registered`, public_key_hash = SHA-256 hex of identityKey
- Log: `slog.Info("device keys registered", "user_id", userID, "device_id", deviceID)`

**`RotateSignedPreKey(ctx, userID, deviceID uuid.UUID, signedPreKey, signedPreKeySig []byte, signedPreKeyID int) error`**
- Validate: signedPreKey 32 bytes, signedPreKeySig 64 bytes.
- Get existing keys for this device. If not found, return `apperror.NotFound("device keys not registered")`
- Update signed prekey fields via `keys.Upsert()` (keeping existing identity_key)
- Log to transparency: event_type = `signed_prekey_rotated`

**`UploadOneTimePreKeys(ctx, userID, deviceID uuid.UUID, keys []model.OneTimePreKey) (int, error)`**
- Validate: each key.PublicKey must be exactly 32 bytes. Max batch 100.
- Call `prekeys.UploadBatch()`
- Log to transparency: event_type = `prekeys_uploaded`
- Return count of inserted keys

**`GetKeyBundle(ctx, targetUserID uuid.UUID) (*model.KeyBundle, error)`**
- Get keys for target user via `keys.ListByUser()`. If no keys, return `apperror.NotFound("user has no registered keys")`
- Pick the first device's keys (for now — multi-device bundle is a separate concern)
- Consume one prekey via `prekeys.ConsumeOne()`. If nil (exhausted), bundle still valid but `OneTimePreKey` field is nil
- Assemble and return KeyBundle

**`GetIdentityKey(ctx, userID uuid.UUID) ([]byte, error)`**
- Call `keys.GetIdentityKey()`. If nil, return `apperror.NotFound("user has no identity key")`

**`GetPreKeyCount(ctx, userID uuid.UUID) (int, error)`**
- Call `prekeys.CountRemaining()`

**`GetTransparencyLog(ctx, userID uuid.UUID, limit int) ([]model.KeyTransparencyEntry, error)`**
- Call `transparency.ListByUser()`

**`RevokeDevice(ctx, userID, deviceID uuid.UUID) error`**
- Delete device keys: `keys.DeleteByDevice()`
- Delete device prekeys: `prekeys.DeleteByDevice()`
- Log to transparency: event_type = `device_revoked`
- Log: `slog.Info("device revoked", "user_id", userID, "device_id", deviceID)`

### Self-check
```bash
cd services/auth && go build ./...

# Verify all 7 public methods exist
grep -c "func (s \*KeyService)" internal/service/key_service.go
# Expected: 7

# Verify key size validation
grep -c "32\|64" internal/service/key_service.go
# Expected: > 0

# Verify apperror usage
grep -c "apperror\." internal/service/key_service.go
# Expected: > 0

# Verify slog usage
grep -c "slog\." internal/service/key_service.go
# Expected: > 0
```

### Test gate
```bash
cd services/auth && go build ./... && go vet ./...
```

### Commit message
`feat(auth): add KeyService with key registration, bundle assembly, and device revocation`

---

## TASK-09 — Auth key handler (POST endpoints)

**Scope**: `services/auth/internal/handler/key_handler.go` (new file)

### Why
HTTP endpoints for key upload operations. Three POST routes.

### Change
Create `services/auth/internal/handler/key_handler.go`:

```go
package handler

import (
	"encoding/base64"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/auth/internal/service"
)

type KeyHandler struct {
	keySvc *service.KeyService
	logger *slog.Logger
}

func NewKeyHandler(keySvc *service.KeyService, logger *slog.Logger) *KeyHandler {
	return &KeyHandler{keySvc: keySvc, logger: logger}
}
```

**Route registration method:**
```go
func (h *KeyHandler) Register(router fiber.Router) {
	keys := router.Group("/keys")
	keys.Post("/identity", h.RegisterDeviceKeys)
	keys.Post("/signed-prekey", h.RotateSignedPreKey)
	keys.Post("/one-time-prekeys", h.UploadOneTimePreKeys)
	keys.Get("/:userId/bundle", h.GetKeyBundle)
	keys.Get("/:userId/identity", h.GetIdentityKey)
	keys.Get("/count", h.GetPreKeyCount)
	keys.Get("/transparency-log", h.GetTransparencyLog)
}
```

**getUserID helper** (same pattern as messaging service):
```go
func getKeyUserID(c *fiber.Ctx) (uuid.UUID, error) {
	idStr := c.Get("X-User-ID")
	if idStr == "" {
		return uuid.Nil, apperror.Unauthorized("missing user context")
	}
	return uuid.Parse(idStr)
}

func getDeviceID(c *fiber.Ctx) (uuid.UUID, error) {
	idStr := c.Get("X-Device-ID")
	if idStr == "" {
		return uuid.Nil, apperror.BadRequest("missing device ID")
	}
	return uuid.Parse(idStr)
}
```

**POST /keys/identity — RegisterDeviceKeys:**
- Parse request body:
  ```go
  type registerKeysRequest struct {
      IdentityKey          string `json:"identity_key"`           // base64url
      SignedPreKey         string `json:"signed_prekey"`          // base64url
      SignedPreKeySignature string `json:"signed_prekey_signature"` // base64url
      SignedPreKeyID       int    `json:"signed_prekey_id"`
  }
  ```
- Decode base64url fields to []byte using `base64.RawURLEncoding.DecodeString()`
- Get userID from `getKeyUserID(c)`, deviceID from `getDeviceID(c)`
- Call `keySvc.RegisterDeviceKeys(...)`
- Return `response.JSON(c, 201, fiber.Map{"status": "ok"})`

**POST /keys/signed-prekey — RotateSignedPreKey:**
- Parse:
  ```go
  type rotatePreKeyRequest struct {
      SignedPreKey         string `json:"signed_prekey"`
      SignedPreKeySignature string `json:"signed_prekey_signature"`
      SignedPreKeyID       int    `json:"signed_prekey_id"`
  }
  ```
- Decode, validate, call `keySvc.RotateSignedPreKey()`
- Return `response.JSON(c, 200, fiber.Map{"status": "ok"})`

**POST /keys/one-time-prekeys — UploadOneTimePreKeys:**
- Parse:
  ```go
  type uploadPreKeysRequest struct {
      PreKeys []preKeyItem `json:"prekeys"`
  }
  type preKeyItem struct {
      KeyID     int    `json:"key_id"`
      PublicKey string `json:"public_key"` // base64url
  }
  ```
- Validate: len(PreKeys) > 0 && len(PreKeys) <= 100
- Decode each PublicKey from base64url, build `[]model.OneTimePreKey`
- Call `keySvc.UploadOneTimePreKeys()`
- Return `response.JSON(c, 201, fiber.Map{"count": count})`

**Important**: all three POST endpoints get the caller's user_id from `X-User-ID` header and device_id from `X-Device-ID` header. These are set by gateway after JWT validation.

### Self-check
```bash
cd services/auth && go build ./...

# Verify all 7 handler methods exist
grep -c "func (h \*KeyHandler)" internal/handler/key_handler.go
# Expected: 8 (Register + 7 endpoints)

# Verify response.JSON usage (not c.JSON)
grep -c "response\.JSON\|response\.Error" internal/handler/key_handler.go
# Expected: > 0
grep -c "c\.JSON(" internal/handler/key_handler.go
# Expected: 0

# Verify base64url decoding
grep -c "base64\.RawURLEncoding" internal/handler/key_handler.go
# Expected: > 0
```

### Test gate
```bash
cd services/auth && go build ./... && go vet ./...
```

### Commit message
`feat(auth): add key handler POST endpoints (identity, signed-prekey, one-time-prekeys)`

---

## TASK-10 — Auth key handler (GET endpoints)

**Scope**: `services/auth/internal/handler/key_handler.go` (edit existing from TASK-09)

### Why
HTTP endpoints for key retrieval: bundle, identity, count, transparency log.

### Change
Add 4 GET handler methods to `key_handler.go`:

**GET /keys/:userId/bundle — GetKeyBundle:**
- Parse `userId` from URL params: `uuid.Parse(c.Params("userId"))`
- Call `keySvc.GetKeyBundle(ctx, targetUserID)`
- Encode all []byte fields to base64url in response
- Return `response.JSON(c, 200, bundleResponse)` where bundleResponse encodes bytes as base64url strings

**GET /keys/:userId/identity — GetIdentityKey:**
- Parse `userId` from URL params
- Call `keySvc.GetIdentityKey(ctx, userID)`
- Return `response.JSON(c, 200, fiber.Map{"identity_key": base64.RawURLEncoding.EncodeToString(key)})`

**GET /keys/count — GetPreKeyCount:**
- Get caller's userID from `getKeyUserID(c)`
- Call `keySvc.GetPreKeyCount(ctx, userID)`
- Return `response.JSON(c, 200, fiber.Map{"count": count})`

**GET /keys/transparency-log — GetTransparencyLog:**
- Get `userId` from query param: `c.Query("user_id")`
- Parse limit from query: `c.QueryInt("limit", 50)`
- Call `keySvc.GetTransparencyLog(ctx, userID, limit)`
- Return `response.JSON(c, 200, fiber.Map{"entries": entries})`

### Self-check
```bash
cd services/auth && go build ./...

# Verify all handler methods now present
grep -c "func (h \*KeyHandler)" internal/handler/key_handler.go
# Expected: 8 (Register + 3 POST + 4 GET)
```

### Test gate
```bash
cd services/auth && go build ./... && go vet ./...
```

### Commit message
`feat(auth): add key handler GET endpoints (bundle, identity, count, transparency-log)`

---

## TASK-11 — Auth key handler tests

**Scope**: `services/auth/internal/handler/key_handler_test.go` (new file)

### Why
Handler tests are mandatory per CLAUDE.md. Must cover happy path + auth fail + validation fail for each endpoint.

### Change
Create `services/auth/internal/handler/key_handler_test.go` using the fn-field mock pattern.

**Mock stores:**
```go
type mockKeyStore struct {
	upsertFn          func(ctx context.Context, keys *model.UserDeviceKeys) error
	getByUserDeviceFn func(ctx context.Context, userID, deviceID uuid.UUID) (*model.UserDeviceKeys, error)
	listByUserFn      func(ctx context.Context, userID uuid.UUID) ([]model.UserDeviceKeys, error)
	deleteByDeviceFn  func(ctx context.Context, userID, deviceID uuid.UUID) error
	getIdentityKeyFn  func(ctx context.Context, userID uuid.UUID) ([]byte, error)
}

type mockPreKeyStore struct {
	uploadBatchFn    func(ctx context.Context, userID, deviceID uuid.UUID, keys []model.OneTimePreKey) (int, error)
	consumeOneFn     func(ctx context.Context, userID uuid.UUID) (*model.OneTimePreKey, error)
	countRemainingFn func(ctx context.Context, userID uuid.UUID) (int, error)
	deleteByDeviceFn func(ctx context.Context, userID, deviceID uuid.UUID) error
}

type mockTransparencyStore struct {
	appendFn     func(ctx context.Context, entry *model.KeyTransparencyEntry) error
	listByUserFn func(ctx context.Context, userID uuid.UUID, limit int) ([]model.KeyTransparencyEntry, error)
}
```

Each mock method checks if fn is nil, returns zero value if so.

**Test cases (minimum required):**

1. `TestRegisterDeviceKeys_Success` — valid base64url keys, returns 201
2. `TestRegisterDeviceKeys_MissingUserID` — no X-User-ID header, returns 401
3. `TestRegisterDeviceKeys_InvalidKeySize` — identity_key != 32 bytes after decode, returns 400
4. `TestRegisterDeviceKeys_MissingDeviceID` — no X-Device-ID header, returns 400
5. `TestUploadOneTimePreKeys_Success` — 5 valid prekeys, returns 201 with count
6. `TestUploadOneTimePreKeys_TooMany` — 101 prekeys, returns 400
7. `TestUploadOneTimePreKeys_EmptyBatch` — 0 prekeys, returns 400
8. `TestGetKeyBundle_Success` — returns bundle with base64url encoded keys
9. `TestGetKeyBundle_UserNotFound` — user has no keys, returns 404
10. `TestGetPreKeyCount_Success` — returns count
11. `TestGetTransparencyLog_Success` — returns entries list

Use `httptest` and `fiber.New()` test app pattern. Set `X-User-ID` and `X-Device-ID` headers in test requests.

### Self-check
```bash
cd services/auth && go build ./...

# Count test functions
grep -c "func Test" internal/handler/key_handler_test.go
# Expected: >= 11
```

### Test gate
```bash
cd services/auth && go test ./internal/handler/... -v -count=1 2>&1 | tail -5
# Must show PASS
```

### On failure
Rollback and retry once. If tests fail on second attempt, keep partial and log.

### Commit message
`test(auth): add key handler tests for E2E key management endpoints`

---

## TASK-12 — Device lifecycle (device_id in session creation)

**Scope**: `services/auth/internal/store/session_store.go`, `services/auth/internal/service/auth_service.go`

### Why
Sessions currently don't populate device_id. E2E requires every session to be bound to a device.

### Change

**session_store.go — modify `Create` method:**

Current INSERT:
```sql
INSERT INTO sessions (user_id, token_hash, ip_address, user_agent, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, created_at
```

Change to:
```sql
INSERT INTO sessions (user_id, device_id, token_hash, ip_address, user_agent, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, created_at
```

Update the Scan to match. The `device_id` comes from `sess.DeviceID`.

**auth_service.go — modify session creation:**

Find where sessions are created (Login and Refresh flows). Before calling `sessionStore.Create()`, if `sess.DeviceID` is nil, generate a new device UUID:
```go
if sess.DeviceID == nil {
    did := uuid.New()
    sess.DeviceID = &did
}
```

This ensures every new session gets a device_id. The client can optionally send `X-Device-ID` header to reuse an existing device (for re-login on the same browser).

**Also in auth_service.go**, find the Login method. If the request includes a `device_id` field, use it. If not, generate a new one. Add `DeviceID` to the login request struct if not already present.

### Self-check
```bash
cd services/auth && go build ./...

# Verify device_id is in INSERT
grep -q "device_id" internal/store/session_store.go
# Must match

# Verify uuid.New() is called for device_id
grep -q "uuid.New()" internal/service/auth_service.go
# Must match
```

### Test gate
```bash
cd services/auth && go build ./... && go test ./internal/handler/... -count=1 2>&1 | tail -3
# Existing tests must still pass
```

### Commit message
`feat(auth): populate device_id on session creation for E2E device binding`

---

## TASK-13 — Wire DI and routes in auth cmd/main.go

**Scope**: `services/auth/cmd/main.go`

### Why
New key stores, service, and handler need to be wired into the auth service startup.

### Change

In `cmd/main.go`, after the existing DI block (around line 118-131), add:

```go
// E2E Key Management
keyStore := store.NewKeyStore(pool)
preKeyStore := store.NewPreKeyStore(pool)
transparencyStore := store.NewTransparencyStore(pool)
keySvc := service.NewKeyService(keyStore, preKeyStore, transparencyStore)
keyHandler := handler.NewKeyHandler(keySvc, logger)
```

After `authHandler.Register(app)` (around line 142), add:
```go
keyHandler.Register(app)
```

Also add the necessary imports for the new store/service/handler packages if not already present.

### Self-check
```bash
cd services/auth && go build ./...

# Verify key handler is wired
grep -q "NewKeyHandler" cmd/main.go
grep -q "keyHandler.Register" cmd/main.go
# Both must match

# Verify all 3 stores are created
grep -c "NewKeyStore\|NewPreKeyStore\|NewTransparencyStore" cmd/main.go
# Expected: 3
```

### Test gate
```bash
cd services/auth && go build ./... && go vet ./...
```

### Commit message
`feat(auth): wire E2E key management DI and routes in auth service`

---

## TASK-14 — CHECKPOINT A: Auth service verification

**Scope**: read-only verification, no code changes

### Why
Verify the entire auth key server compiles, passes all tests, and has correct structure.

### Change
No code changes. Run verification commands only.

### Verification steps (execute ALL, log results in PROGRESS.md)

```bash
# 1. Full build
cd services/auth && go build ./...

# 2. Full test suite
cd services/auth && go test ./... -count=1 -v 2>&1 | tail -20

# 3. Vet
cd services/auth && go vet ./...

# 4. Verify key handler routes exist
grep -c "keys\." internal/handler/key_handler.go
# Expected: 7 (route registrations)

# 5. Verify all store interfaces are implemented
grep -c "func.*keyStore\b" internal/store/key_store.go
grep -c "func.*preKeyStore\b" internal/store/prekey_store.go
grep -c "func.*transparencyStore\b" internal/store/transparency_store.go

# 6. Verify service methods
grep -c "func (s \*KeyService)" internal/service/key_service.go
# Expected: 7

# 7. Verify DI wiring
grep -q "NewKeyService\|NewKeyHandler" cmd/main.go && echo "DI OK"
```

If any verification fails, log the failure in PROGRESS.md with details. Do NOT halt — continue to Phase 2.

### Test gate
```bash
cd services/auth && go build ./... && go test ./... -count=1
```

### On failure
Log "CHECKPOINT A PARTIAL: <details>" and continue to TASK-15.

---

## Phase 2 — Messaging E2E Support

---

## TASK-15 — Messaging E2E models

**Scope**: `services/messaging/internal/model/models.go` (edit existing)

### Why
Message model needs to support encrypted content and the E2E envelope format.

### Change

Add to `services/messaging/internal/model/models.go`:

1. Add `EncryptedContent` field to the `Message` struct (after `Content`):
```go
EncryptedContent []byte `json:"encrypted_content,omitempty"` // E2E ciphertext envelope (BYTEA)
```

2. Add `ExpiresAt` field if not already present:
```go
ExpiresAt *time.Time `json:"expires_at,omitempty"` // disappearing messages
```

3. Add new constants for E2E:
```go
const (
	MessageTypeEncrypted = "encrypted"
)
```

4. Add envelope struct for API serialization:
```go
type EncryptedEnvelope struct {
	Version        int                         `json:"v"`
	SenderDeviceID string                      `json:"sender_device_id"`
	Devices        map[string]DeviceCiphertext `json:"devices"`
}

type DeviceCiphertext struct {
	Type int    `json:"type"` // 1=prekey, 2=message
	Body string `json:"body"` // base64url encoded ciphertext
}
```

### Self-check
```bash
cd services/messaging && go build ./...

grep -q "EncryptedContent" internal/model/models.go
grep -q "MessageTypeEncrypted" internal/model/models.go
grep -q "EncryptedEnvelope" internal/model/models.go
```

### Test gate
```bash
cd services/messaging && go build ./... && go vet ./...
```

### Commit message
`feat(messaging): add E2E encrypted message models and envelope types`

---

## TASK-16 — Messaging encrypted message store methods

**Scope**: `services/messaging/internal/store/message_store.go` (edit existing)

### Why
Store needs to support creating and reading encrypted messages (using `encrypted_content` column).

### Change

1. **Add `CreateEncrypted` method to MessageStore interface:**
```go
CreateEncrypted(ctx context.Context, msg *model.Message, envelope []byte) error
```

2. **Implement `CreateEncrypted`:**
```sql
INSERT INTO messages (chat_id, sender_id, type, content, encrypted_content, sequence_number)
VALUES ($1, $2, 'encrypted', NULL, $3, $4)
RETURNING id, is_edited, is_deleted, is_pinned, is_forwarded, is_one_time, sequence_number, created_at, viewed_at, viewed_by
```

Note: `content` is explicitly NULL for encrypted messages. The `type` is always `'encrypted'`.

3. **Modify the `messageSelectColumns` constant** (around line 22-28) to include `encrypted_content`:
Add `m.encrypted_content` to the SELECT columns. When scanning, populate `msg.EncryptedContent`.

4. **Update all Scan calls** in existing methods (`GetByID`, `GetByIDs`, `ListByChat`, etc.) to include the new `EncryptedContent` field. Use a helper function to avoid duplication:
```go
func scanMessage(row pgx.Row, msg *model.Message) error {
    return row.Scan(
        &msg.ID, &msg.ChatID, &msg.SenderID, &msg.Type, &msg.Content,
        &msg.EncryptedContent, // NEW
        // ... rest of existing fields
    )
}
```

**Important**: this is the riskiest edit in this plan because it touches existing scan paths. Be very careful to maintain the exact column order in SELECT and Scan.

### Self-check
```bash
cd services/messaging && go build ./...

# Verify CreateEncrypted exists
grep -q "CreateEncrypted" internal/store/message_store.go

# Verify encrypted_content in SELECT
grep -q "encrypted_content" internal/store/message_store.go

# Verify no broken scan (build succeeds)
```

### Test gate
```bash
cd services/messaging && go build ./... && go test ./internal/handler/... -count=1 2>&1 | tail -5
# Existing tests must still pass
```

### On failure
Rollback and retry once. This task modifies critical scan paths.

### Commit message
`feat(messaging): add encrypted message storage and retrieval support`

---

## TASK-17 — Messaging encrypted message service and handler

**Scope**: `services/messaging/internal/service/message_service.go`, `services/messaging/internal/handler/message_handler.go`

### Why
Need API endpoint for sending encrypted messages, separate from plaintext send.

### Change

**message_service.go — add `SendEncryptedMessage` method:**
```go
func (s *MessageService) SendEncryptedMessage(ctx context.Context, chatID, senderID uuid.UUID, envelope []byte, senderDeviceID string) (*model.Message, error) {
    // 1. Verify chat exists and is encrypted
    chat, err := s.chats.GetByID(ctx, chatID)
    if err != nil { return nil, fmt.Errorf("get chat: %w", err) }
    if chat == nil { return nil, apperror.NotFound("chat not found") }
    if !chat.IsEncrypted { return nil, apperror.BadRequest("chat is not E2E encrypted") }

    // 2. Verify sender is member
    isMember, _, err := s.chats.IsMember(ctx, chatID, senderID)
    if err != nil { return nil, fmt.Errorf("check membership: %w", err) }
    if !isMember { return nil, apperror.Forbidden("not a member") }

    // 3. Validate envelope size (max 256KB — generous for multi-device)
    if len(envelope) > 256*1024 { return nil, apperror.BadRequest("envelope too large") }

    // 4. Create encrypted message
    msg := &model.Message{ChatID: chatID, SenderID: senderID, Type: model.MessageTypeEncrypted}
    if err := s.messages.CreateEncrypted(ctx, msg, envelope); err != nil {
        return nil, fmt.Errorf("create encrypted message: %w", err)
    }

    // 5. Publish NATS event (with envelope, NOT plaintext)
    // The NATS payload for E2E messages includes encrypted_content
    // Subscribers (gateway WS) deliver the envelope as-is
    s.publishEncryptedMessageSent(ctx, chatID, msg, envelope, senderID)

    return msg, nil
}
```

Add `publishEncryptedMessageSent` — similar to existing `publishMessageSent` but includes `envelope` in the event data and does NOT include plaintext `content`.

**message_handler.go — add `SendEncryptedMessage` handler:**
```go
type sendEncryptedRequest struct {
    Envelope json.RawMessage `json:"envelope"` // E2E envelope JSON
}
```

- Route: `POST /chats/:id/messages/encrypted`
- Parse chat_id from URL, user_id from header
- Parse and validate request body
- Call `msgSvc.SendEncryptedMessage()`
- Return `response.JSON(c, 201, msg)`

**Register the route** in the existing `Register` method:
```go
chatGroup.Post("/:id/messages/encrypted", h.SendEncryptedMessage)
```

### Self-check
```bash
cd services/messaging && go build ./...

grep -q "SendEncryptedMessage" internal/service/message_service.go
grep -q "SendEncryptedMessage" internal/handler/message_handler.go
grep -q "messages/encrypted" internal/handler/message_handler.go
```

### Test gate
```bash
cd services/messaging && go build ./... && go test ./internal/handler/... -count=1 2>&1 | tail -5
```

### Commit message
`feat(messaging): add SendEncryptedMessage endpoint for E2E DM`

---

## TASK-18 — Skip Meilisearch indexing for E2E messages

**Scope**: `services/messaging/internal/search/indexer.go`

### Why
E2E messages must NOT be indexed in Meilisearch. The indexer currently indexes all messages.

### Change

In the `handleNewMessage` and `handleUpdatedMessage` functions, add an early return if the message type is `encrypted`:

```go
// Skip E2E encrypted messages — they have no plaintext to index
if msg.Type == "encrypted" || msg.Content == nil || *msg.Content == "" {
    return
}
```

Place this check after unmarshalling the NATS event payload and before calling `BuildMessageDocument()`.

Similarly for `handleUpdatedMessage`.

### Self-check
```bash
cd services/messaging && go build ./...

# Verify the skip check exists
grep -c "encrypted" internal/search/indexer.go
# Expected: >= 2 (one per handler)
```

### Test gate
```bash
cd services/messaging && go build ./... && go vet ./...
```

### Commit message
`feat(messaging): skip Meilisearch indexing for E2E encrypted messages`

---

## TASK-19 — Push notification: no preview for E2E chats

**Scope**: `services/gateway/internal/ws/nats_subscriber.go`

### Why
Push notifications for E2E chats must NOT include plaintext message content. Currently `buildPushPayload` includes up to 100 chars of message text.

### Change

In the `buildPushPayload` function (around line 315-333), add a check for encrypted messages:

```go
// For E2E encrypted messages, don't include content preview
if msg.Type != nil && *msg.Type == "encrypted" {
    payload.Body = "Новое сообщение"
} else {
    payload.Body = buildMessagePreview(msg.Content, msg.Type)
}
```

To support this, add a `Type` field to the `pushMessageData` struct if not already present:
```go
Type *string `json:"type"`
```

Also update the NATS event unmarshalling to include the message type field.

**Important**: the gateway does NOT need to know if a chat is encrypted. It just checks the message type. If `type == "encrypted"`, no preview. This avoids adding a chat lookup to the push path.

### Self-check
```bash
cd services/gateway && go build ./...

# Verify encrypted check in push
grep -q "encrypted" internal/ws/nats_subscriber.go
# Must match

# Verify "Новое сообщение" fallback
grep -q "Новое сообщение" internal/ws/nats_subscriber.go
# Must match (may already exist for other message types)
```

### Test gate
```bash
cd services/gateway && go build ./... && go test ./internal/ws/... -count=1 2>&1 | tail -5
```

### On failure
Rollback and skip. Gateway tests may have pre-existing issues.

### Commit message
`feat(gateway): suppress push preview for E2E encrypted messages`

---

## TASK-20 — CHECKPOINT B: Messaging and Gateway verification

**Scope**: read-only verification, no code changes

### Verification steps

```bash
# 1. Messaging full build
cd services/messaging && go build ./...

# 2. Messaging tests
cd services/messaging && go test ./... -count=1 2>&1 | tail -20

# 3. Gateway full build
cd services/gateway && go build ./...

# 4. Gateway tests
cd services/gateway && go test ./... -count=1 2>&1 | tail -20

# 5. Verify E2E message flow exists
grep -q "CreateEncrypted" services/messaging/internal/store/message_store.go
grep -q "SendEncryptedMessage" services/messaging/internal/service/message_service.go
grep -q "SendEncryptedMessage" services/messaging/internal/handler/message_handler.go

# 6. Verify Meilisearch skip
grep -q "encrypted" services/messaging/internal/search/indexer.go

# 7. Verify push no-preview
grep -q "encrypted" services/gateway/internal/ws/nats_subscriber.go
```

### Test gate
```bash
cd services/messaging && go build ./... && cd ../../services/gateway && go build ./...
```

### On failure
Log "CHECKPOINT B PARTIAL: <details>" and continue.

---

## Phase 3 — Disappearing Messages Backend

---

## TASK-21 — Disappearing messages: chat setting endpoint

**Scope**: `services/messaging/internal/handler/chat_handler.go`, `services/messaging/internal/service/chat_service.go`, `services/messaging/internal/store/chat_store.go`

### Why
Users need an API to set disappearing message timers per chat. The `expires_at` field already exists on messages but there's no way to configure the timer.

### Change

1. **Add `disappearing_timer` column to chats** — create `migrations/043_chat_disappearing_timer.sql`:
```sql
ALTER TABLE chats ADD COLUMN IF NOT EXISTS disappearing_timer INT NOT NULL DEFAULT 0;
-- 0 = off, 86400 = 24h, 604800 = 7d, 2592000 = 30d
```

2. **Add to Chat model** in messaging service:
```go
DisappearingTimer int `json:"disappearing_timer"` // seconds, 0=off
```

3. **Add endpoint** `PUT /chats/:id/disappearing`:
```go
type setDisappearingRequest struct {
    Timer int `json:"timer"` // 0, 86400, 604800, 2592000
}
```
- Validate timer is one of: 0, 86400, 604800, 2592000
- Verify caller is member with appropriate permissions
- Update chat `disappearing_timer`
- Return updated chat

4. **In SendMessage and SendEncryptedMessage**, if chat has `disappearing_timer > 0`, set `msg.ExpiresAt = time.Now().Add(time.Duration(chat.DisappearingTimer) * time.Second)` before insert.

5. **Update message INSERT** to include `expires_at` if set.

### Self-check
```bash
cd services/messaging && go build ./...
test -f migrations/043_chat_disappearing_timer.sql
grep -q "disappearing_timer" internal/model/models.go
grep -q "disappearing" internal/handler/chat_handler.go || grep -q "disappearing" internal/handler/message_handler.go
```

### Test gate
```bash
cd services/messaging && go build ./... && go vet ./...
```

### Commit message
`feat(messaging): add disappearing messages timer setting per chat`

---

## TASK-22 — Disappearing messages: cleanup cron job

**Scope**: `services/messaging/internal/service/cleanup_service.go` (new file), `services/messaging/cmd/main.go`

### Why
Expired messages need server-side cleanup. A background goroutine deletes messages past their `expires_at`.

### Change

1. Create `services/messaging/internal/service/cleanup_service.go`:

```go
package service

import (
	"context"
	"log/slog"
	"time"
)

type CleanupStore interface {
	DeleteExpired(ctx context.Context) (int64, error)
}

type CleanupService struct {
	store    CleanupStore
	interval time.Duration
}

func NewCleanupService(store CleanupStore, interval time.Duration) *CleanupService {
	return &CleanupService{store: store, interval: interval}
}

func (s *CleanupService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	slog.Info("disappearing message cleanup started", "interval", s.interval)
	for {
		select {
		case <-ctx.Done():
			slog.Info("disappearing message cleanup stopped")
			return
		case <-ticker.C:
			count, err := s.store.DeleteExpired(ctx)
			if err != nil {
				slog.Error("cleanup expired messages failed", "error", err)
				continue
			}
			if count > 0 {
				slog.Info("cleaned up expired messages", "count", count)
			}
		}
	}
}
```

2. Add `DeleteExpired` to message store:
```go
func (s *messageStore) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM messages WHERE expires_at IS NOT NULL AND expires_at < NOW()`)
	if err != nil {
		return 0, fmt.Errorf("delete expired: %w", err)
	}
	return tag.RowsAffected(), nil
}
```

3. In `cmd/main.go`, start cleanup service:
```go
cleanupSvc := service.NewCleanupService(messageStore, 1*time.Minute)
go cleanupSvc.Start(ctx)
```

### Self-check
```bash
cd services/messaging && go build ./...

grep -q "DeleteExpired" internal/store/message_store.go
grep -q "CleanupService" internal/service/cleanup_service.go
grep -q "cleanupSvc" cmd/main.go
```

### Test gate
```bash
cd services/messaging && go build ./... && go vet ./...
```

### Commit message
`feat(messaging): add background cleanup for disappearing messages`

---

## TASK-23 — E2E chat creation support

**Scope**: `services/messaging/internal/handler/chat_handler.go`, `services/messaging/internal/service/chat_service.go`, `services/messaging/internal/store/chat_store.go`

### Why
Clients need to create DM chats with `is_encrypted=true`. Currently the flag is always false.

### Change

1. **In the create DM endpoint** (look for `CreateDirectChat` or equivalent in chat_handler.go):
   - Add `is_encrypted` to the create request struct:
   ```go
   IsEncrypted bool `json:"is_encrypted"`
   ```
   - Pass `IsEncrypted` through to the service and store layer
   - In the store's INSERT, include `is_encrypted`:
   ```sql
   INSERT INTO chats (type, is_encrypted, ...) VALUES ('direct', $N, ...)
   ```

2. **Feature flag check** in the service layer:
   - Before creating an E2E chat, check if the feature flag `e2e_dm_enabled` is true
   - Add a simple feature flag check function:
   ```go
   func (s *ChatService) isFeatureEnabled(ctx context.Context, key string) bool {
       var enabled bool
       err := s.pool.QueryRow(ctx, "SELECT enabled FROM feature_flags WHERE key = $1", key).Scan(&enabled)
       if err != nil {
           return false // fail-closed
       }
       return enabled
   }
   ```
   - If `is_encrypted=true` but `e2e_dm_enabled=false`, return `apperror.BadRequest("E2E encryption is not enabled")`

3. **Verify the `is_encrypted` field is returned** in chat list and chat detail responses (it should be, since Chat model already has it).

### Self-check
```bash
cd services/messaging && go build ./...

grep -q "is_encrypted\|IsEncrypted" internal/handler/chat_handler.go
grep -q "e2e_dm_enabled" internal/service/chat_service.go
```

### Test gate
```bash
cd services/messaging && go build ./... && go test ./internal/handler/... -count=1 2>&1 | tail -5
```

### Commit message
`feat(messaging): support E2E encrypted DM creation with feature flag gating`

---

## TASK-24 — Device listing endpoint for E2E

**Scope**: `services/auth/internal/handler/key_handler.go`, `services/auth/internal/service/key_service.go`

### Why
When sending an E2E message, the client needs to know all active devices of the recipient to encrypt for each one. Need GET /keys/:userId/devices.

### Change

1. **Add `ListUserDevices` to KeyService:**
```go
func (s *KeyService) ListUserDevices(ctx context.Context, userID uuid.UUID) ([]model.UserDeviceKeys, error) {
	devices, err := s.keys.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list user devices: %w", err)
	}
	return devices, nil
}
```

2. **Add handler method:**
```go
func (h *KeyHandler) ListUserDevices(c *fiber.Ctx) error {
	userID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("invalid user ID"))
	}
	devices, err := h.keySvc.ListUserDevices(c.Context(), userID)
	if err != nil {
		return response.Error(c, err)
	}
	// Return only device_id list (not full key material)
	type deviceInfo struct {
		DeviceID  uuid.UUID `json:"device_id"`
		UploadedAt time.Time `json:"uploaded_at"`
	}
	result := make([]deviceInfo, len(devices))
	for i, d := range devices {
		result[i] = deviceInfo{DeviceID: d.DeviceID, UploadedAt: d.UploadedAt}
	}
	return response.JSON(c, 200, fiber.Map{"devices": result})
}
```

3. **Register route** in `Register` method:
```go
keys.Get("/:userId/devices", h.ListUserDevices)
```

### Self-check
```bash
cd services/auth && go build ./...

grep -q "ListUserDevices" internal/handler/key_handler.go
grep -q "ListUserDevices" internal/service/key_service.go
grep -q "devices" internal/handler/key_handler.go
```

### Test gate
```bash
cd services/auth && go build ./... && go vet ./...
```

### Commit message
`feat(auth): add device listing endpoint for E2E multi-device encryption`

---

## TASK-25 — Gateway: proxy key management routes

**Scope**: `services/gateway/internal/handler/proxy.go` or equivalent proxy configuration file

### Why
Gateway must proxy /api/v1/keys/* requests to auth service. Currently only /api/v1/auth/* is proxied.

### Change

Find the proxy configuration in gateway handler (look for how `/auth/` routes are proxied to the auth service). Add similar proxy rules for `/keys/`:

```go
// E2E Key Management (proxied to auth service)
api.All("/keys/*", proxy.Forward(authServiceURL+"/keys", proxyConfig))
```

Or if using manual proxy:
```go
api.All("/keys/*", h.proxyToAuth)
```

The exact implementation depends on how the gateway currently proxies to auth. Follow the same pattern. The key routes to proxy:
- `POST /api/v1/keys/identity` → auth:8081/keys/identity
- `POST /api/v1/keys/signed-prekey` → auth:8081/keys/signed-prekey
- `POST /api/v1/keys/one-time-prekeys` → auth:8081/keys/one-time-prekeys
- `GET /api/v1/keys/:userId/bundle` → auth:8081/keys/:userId/bundle
- `GET /api/v1/keys/:userId/identity` → auth:8081/keys/:userId/identity
- `GET /api/v1/keys/:userId/devices` → auth:8081/keys/:userId/devices
- `GET /api/v1/keys/count` → auth:8081/keys/count
- `GET /api/v1/keys/transparency-log` → auth:8081/keys/transparency-log

**Important**: These routes MUST go through the JWT auth middleware (same as other API routes). The gateway validates JWT and sets `X-User-ID` and `X-Device-ID` headers before forwarding to auth.

Add `X-Device-ID` header extraction from JWT claims or a new client header. If JWT doesn't contain device_id yet (it likely doesn't since we just added it in TASK-12), read it from a client-sent `X-Device-ID` header and pass it through. This is temporary until JWT claims are updated.

### Self-check
```bash
cd services/gateway && go build ./...

# Verify keys route is proxied
grep -q "/keys" internal/handler/proxy.go 2>/dev/null || grep -rq "/keys" internal/handler/
# Must match somewhere in gateway handler code
```

### Test gate
```bash
cd services/gateway && go build ./... && go vet ./...
```

### On failure
Rollback and skip. Gateway proxy configuration may vary from expected structure.

### Commit message
`feat(gateway): proxy E2E key management routes to auth service`

---

## TASK-26 — Gateway: proxy encrypted message route

**Scope**: `services/gateway/internal/handler/proxy.go` or equivalent

### Why
Gateway must proxy the new encrypted message endpoint to messaging service.

### Change

Add proxy rule for the encrypted message endpoint:
- `POST /api/v1/chats/:id/messages/encrypted` → messaging:8082/chats/:id/messages/encrypted

This should already be covered if the gateway proxies all `/chats/` routes to messaging. Verify this is the case. If not, add the specific rule.

Also verify that `PUT /chats/:id/disappearing` is proxied (same pattern as other chat routes).

### Self-check
```bash
cd services/gateway && go build ./...

# Verify chats routes are proxied to messaging
grep -rq "chats\|messaging" internal/handler/
```

### Test gate
```bash
cd services/gateway && go build ./... && go vet ./...
```

### Commit message
`feat(gateway): ensure encrypted message and disappearing timer routes are proxied`

### On failure
If the gateway already proxies all `/chats/*` routes, this may be a no-op. Log "TASK-26 NO-OP: chats routes already fully proxied" and commit nothing.

---

## TASK-27 — CHECKPOINT C: Full backend verification

**Scope**: read-only verification, no code changes

### Verification steps

```bash
# 1. All services build
cd services/auth && go build ./...
cd ../../services/messaging && go build ./...
cd ../../services/gateway && go build ./...

# 2. Auth tests
cd services/auth && go test ./... -count=1 2>&1 | tail -10

# 3. Messaging tests
cd services/messaging && go test ./... -count=1 2>&1 | tail -10

# 4. Gateway tests
cd services/gateway && go test ./... -count=1 2>&1 | tail -10

# 5. Migration file count
ls migrations/04*.sql | wc -l
# Expected: 3 (041, 042, 043)

# 6. Design doc exists
test -f docs/SIGNAL_PROTOCOL.md && echo "Design doc OK"

# 7. Verify feature flag table
grep -q "feature_flags" migrations/041_feature_flags.sql && echo "Feature flags OK"

# 8. Verify key tables
grep -c "CREATE TABLE" migrations/042_e2e_keys.sql
# Expected: 4

# 9. Git log — verify all commits are clean
git log --oneline feat/phase-7-e2e --not master | head -20

# 10. No uncommitted changes
git status
```

Log all results in PROGRESS.md.

### Test gate
```bash
cd services/auth && go build ./... && \
cd ../../services/messaging && go build ./... && \
cd ../../services/gateway && go build ./...
```

---

## TASK-28 — Update PHASES.md with Phase 7 progress

**Scope**: `PHASES.md`

### Why
Per CLAUDE.md rule: "Обновляй PHASES.md — отмечай [x] выполненные задачи после завершения".

### Change

In `PHASES.md` Phase 7 section (around line 1085-1177), check off the tasks that have been completed by this plan:

```
- [x] POST /keys/identity — загрузить Identity Key
- [x] POST /keys/signed-prekey — загрузить Signed PreKey
- [x] POST /keys/one-time-prekeys — загрузить batch 100 One-Time PreKeys
- [x] GET /keys/:userId/bundle — получить key bundle
- [x] GET /keys/:userId/identity — получить Identity Key
- [x] GET /keys/count — сколько One-Time PreKeys осталось
- [x] GET /keys/transparency-log — публичный лог изменений ключей
```

Also check off database tasks:
```
- [x] user_id + device_id PK
- [x] identity_key BYTEA, signed_prekey BYTEA, signed_prekey_signature BYTEA
- [x] signed_prekey_id INT, uploaded_at TIMESTAMPTZ
- [x] id SERIAL, user_id, device_id, key_id INT
- [x] public_key BYTEA, used BOOLEAN DEFAULT false
- [x] id SERIAL, user_id, event_type TEXT, public_key_hash TEXT, created_at
```

Do NOT check off frontend tasks or features that haven't been implemented (Safety Numbers, Sender Keys, etc.).

### Self-check
```bash
# Verify checkboxes were updated
grep -c "\[x\]" PHASES.md | head -1
# Should be higher than before
```

### Test gate
```bash
# PHASES.md is documentation — no code to test
head -1 PHASES.md | grep -q "# Orbit" && echo "OK"
```

### Commit message
`docs(phases): mark completed Phase 7 backend tasks`

---

# End of plan

**Total tasks**: 28
- Phase 0 (Documentation & Schema): TASK-01 to TASK-03
- Phase 1 (Auth Key Server): TASK-04 to TASK-14
- Phase 2 (Messaging E2E): TASK-15 to TASK-20
- Phase 3 (Disappearing Messages): TASK-21 to TASK-22
- Phase 4 (E2E Chat Creation & Device Listing): TASK-23 to TASK-26
- Checkpoints: TASK-14, TASK-20, TASK-27
- Final: TASK-28

**After this plan completes**, the operator will:
1. Review all commits on `feat/phase-7-e2e` branch
2. Fix any issues found during review
3. Implement frontend Signal Protocol (interactive session)
4. Merge to master when ready
