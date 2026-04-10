# Phase 8: Bots & Integrations — Autonomous Execution Plan

## Operational Rules

1. **Read CLAUDE.md first** — all project conventions apply. This plan supplements, not replaces, CLAUDE.md.
2. **Execute tasks in order**. Skip any task marked DONE in `phase-8-plan/PROGRESS.md`.
3. **After completing each task**, append its status to `phase-8-plan/PROGRESS.md` immediately. Format:
   ```
   ## TASK-NN: Short title
   Status: DONE
   Files: list of created/modified files
   ```
4. **CHECKPOINT tasks**: run the specified verification commands. If anything fails, fix it before proceeding. Record fix details in PROGRESS.md.
5. **Commit at every CHECKPOINT** with message format: `feat(phase8): TASK-XX..YY — description`. Stage only files created/modified by those tasks.
6. **Never modify files outside the task scope** unless fixing a compilation error from a previous task.
7. **Follow existing patterns exactly** — each task references a pattern file. Read it before writing.
8. **SQL migrations**: write exact DDL as specified. Do NOT improvise column types or constraints.
9. **Go code**: use `pkg/apperror`, `pkg/response`, `pkg/validator`, `pkg/config`, `pkg/permissions` as documented in CLAUDE.md. Never use `c.JSON()` directly.
10. **Error handling**: never `_ = err`. Always handle or return errors.
11. **Tests**: use fn-field mock pattern, NOT testify/mock or mockgen.
12. **Imports**: shared packages at `github.com/mst-corp/orbit/pkg/<package>`.
13. **After editing go.mod**, run `cd <service> && go mod tidy` to generate/update go.sum.
14. **getUserID pattern**: read `X-User-ID` from header, parse as UUID. See CLAUDE.md for exact implementation.
15. **Store pattern**: public interface, private struct, constructor returns interface. See `services/messaging/internal/store/message_store.go` for reference.
16. **Service pattern**: public struct with private fields, constructor returns pointer. See `services/messaging/internal/service/message_service.go`.
17. **Handler pattern**: public struct, `Register(router fiber.Router)` method. See `services/messaging/internal/handler/message_handler.go`.

## Architecture Context

### Current State
- Latest migration: **040**
- Users table: NO `account_type`, NO `username` column
- Messages table: NO `reply_markup`, NO `via_bot_id` column
- Gateway proxies: auth→8081, media→8083, calls→8084, messaging→8082 (catch-all)
- Bots service (8086): health-only stub, NOT in docker-compose
- Integrations service (8087): health-only stub, NOT in docker-compose
- System permissions: bits 0–10 used (AllSysPermissions = 2047)
- NATS: ephemeral pub/sub, no JetStream streams for bots/integrations yet

### Key Design Decisions
1. **Bot = service account user**: `users.account_type = 'bot'`, with details in `bots` table. Bot messages use `messages.sender_id = bot.user_id`.
2. **Bots service calls messaging service** via internal HTTP (X-Internal-Token) to send/edit/delete messages. Never writes to messages table directly.
3. **Bot API** lives inside bots service at path `/bot/:token/method`. Gateway proxies `/api/v1/bot/:token/*` to bots service without JWT.
4. **Integration webhooks** arrive at `/api/v1/webhooks/in/:connectorId`. Gateway proxies to integrations service without JWT. HMAC verification happens in integrations service.
5. **Admin endpoints** for bot/integration management are JWT-authenticated, proxied through gateway at `/api/v1/bots/*` and `/api/v1/integrations/*`.
6. **Webhook delivery**: bots service subscribes to NATS chat events, delivers to bot webhook URLs with retry.
7. **Integrations deliver messages** by calling messaging service internally (same pattern as bots).

---

## TASK-01: Migration 041 — Add account_type and username to users

**Create** `migrations/041_bot_accounts.sql`

```sql
-- Phase 8: Add account_type and username to users for bot/system identity support

ALTER TABLE users ADD COLUMN account_type TEXT NOT NULL DEFAULT 'human'
    CHECK (account_type IN ('human', 'bot', 'system'));

ALTER TABLE users ADD COLUMN username TEXT;

-- Unique index on username, allowing NULLs (humans may not have username)
CREATE UNIQUE INDEX idx_users_username ON users (username) WHERE username IS NOT NULL;

-- Index for filtering by account_type
CREATE INDEX idx_users_account_type ON users (account_type) WHERE account_type != 'human';
```

**Verify**: Read the file, confirm syntax is valid PostgreSQL.

---

## TASK-02: Migration 042 — Create bots tables

**Create** `migrations/042_bots.sql`

```sql
-- Phase 8: Bot identity, tokens, commands, and chat installations

CREATE TABLE bots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    owner_id UUID NOT NULL REFERENCES users(id),
    description TEXT,
    short_description TEXT,
    is_system BOOLEAN NOT NULL DEFAULT false,
    is_inline BOOLEAN NOT NULL DEFAULT false,
    webhook_url TEXT,
    webhook_secret_hash TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_bots_owner ON bots (owner_id);
CREATE INDEX idx_bots_active ON bots (is_active) WHERE is_active = true;

CREATE TABLE bot_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    token_prefix TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_bot_tokens_hash ON bot_tokens (token_hash) WHERE is_active = true;
CREATE INDEX idx_bot_tokens_bot ON bot_tokens (bot_id);

CREATE TABLE bot_commands (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    command TEXT NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (bot_id, command)
);

CREATE TABLE bot_installations (
    bot_id UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    installed_by UUID NOT NULL REFERENCES users(id),
    scopes BIGINT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bot_id, chat_id)
);

CREATE INDEX idx_bot_installations_chat ON bot_installations (chat_id) WHERE is_active = true;
```

**Verify**: Read the file, confirm syntax.

---

## TASK-03: Migration 043 — Create integrations tables

**Create** `migrations/043_integrations.sql`

```sql
-- Phase 8: Integration connectors, routing rules, and delivery tracking

CREATE TABLE integration_connectors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('inbound_webhook', 'outbound_webhook', 'polling')),
    bot_id UUID REFERENCES bots(id) ON DELETE SET NULL,
    config JSONB NOT NULL DEFAULT '{}',
    secret_hash TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE integration_routes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connector_id UUID NOT NULL REFERENCES integration_connectors(id) ON DELETE CASCADE,
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    event_filter TEXT,
    template TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (connector_id, chat_id)
);

CREATE TABLE integration_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connector_id UUID NOT NULL REFERENCES integration_connectors(id) ON DELETE CASCADE,
    route_id UUID REFERENCES integration_routes(id) ON DELETE SET NULL,
    external_event_id TEXT,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'delivered', 'failed', 'dead_letter')),
    orbit_message_id UUID,
    correlation_key TEXT,
    attempt_count INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5,
    last_error TEXT,
    next_retry_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_deliveries_pending ON integration_deliveries (next_retry_at)
    WHERE status IN ('pending', 'failed');
CREATE INDEX idx_deliveries_correlation ON integration_deliveries (connector_id, correlation_key)
    WHERE correlation_key IS NOT NULL;
CREATE INDEX idx_deliveries_external ON integration_deliveries (connector_id, external_event_id)
    WHERE external_event_id IS NOT NULL;
CREATE INDEX idx_deliveries_connector ON integration_deliveries (connector_id, created_at DESC);
```

**Verify**: Read the file, confirm syntax.

---

## TASK-04: Migration 044 — Message bot extensions

**Create** `migrations/044_message_bot_extensions.sql`

```sql
-- Phase 8: Add reply_markup (inline keyboards) and via_bot_id to messages

ALTER TABLE messages ADD COLUMN reply_markup JSONB;
ALTER TABLE messages ADD COLUMN via_bot_id UUID REFERENCES users(id) ON DELETE SET NULL;
```

**Verify**: Read the file, confirm syntax.

---

## TASK-05: System permissions — add bot/integration management

**Modify** `pkg/permissions/system.go`

Add new permission constants (bits 11–13):

```go
SysManageBots         int64 = 1 << 11 // 2048 — create/delete bots, rotate tokens
SysManageIntegrations int64 = 1 << 12 // 4096 — create/modify connectors, routes
SysViewBotLogs        int64 = 1 << 13 // 8192 — view bot delivery logs, integration logs
```

Update `AllSysPermissions` to include the new bits (should be `1<<14 - 1 = 16383`).

Update role mappings:
- `superadmin`: `AllSysPermissions`
- `admin`: add `SysManageBots | SysManageIntegrations | SysViewBotLogs` to existing permissions
- `compliance`: add `SysViewBotLogs` to existing permissions
- `member`: unchanged (0)

**Pattern**: Read current `pkg/permissions/system.go` fully before editing. Preserve existing constants and logic.

**Verify**: `cd pkg && go build ./...`

---

## TASK-06: CHECKPOINT

**Run**:
```bash
cd D:/job/orbit/pkg && go build ./... && go vet ./...
```

Verify all 4 migration files exist and are syntactically valid SQL.

**Commit**: `feat(phase8): TASK-01..05 — schema migrations and system permissions for bots/integrations`

---

## TASK-07: Docker-compose and env — add bots + integrations services

**Modify** `docker-compose.yml`:

Add `bots` service block after `calls` service (follow the exact pattern of `calls` service):
```yaml
  bots:
    build:
      context: .
      dockerfile: services/bots/Dockerfile
    ports:
      - "127.0.0.1:8086:8086"
    environment:
      PORT: "8086"
      DATABASE_URL: postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable
      REDIS_URL: redis://:${REDIS_PASSWORD}@redis:6379/0
      ORBIT_NATS_URL: nats://${NATS_TOKEN}@nats:4222
      INTERNAL_SECRET: ${INTERNAL_SECRET}
      MESSAGING_SERVICE_URL: http://messaging:8082
      MEDIA_SERVICE_URL: http://media:8083
      BOT_TOKEN_SECRET: ${BOT_TOKEN_SECRET}
    depends_on:
      - postgres
      - redis
      - nats
      - messaging
    restart: unless-stopped
```

Add `integrations` service block after `bots`:
```yaml
  integrations:
    build:
      context: .
      dockerfile: services/integrations/Dockerfile
    ports:
      - "127.0.0.1:8087:8087"
    environment:
      PORT: "8087"
      DATABASE_URL: postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable
      REDIS_URL: redis://:${REDIS_PASSWORD}@redis:6379/0
      ORBIT_NATS_URL: nats://${NATS_TOKEN}@nats:4222
      INTERNAL_SECRET: ${INTERNAL_SECRET}
      MESSAGING_SERVICE_URL: http://messaging:8082
    depends_on:
      - postgres
      - redis
      - nats
      - messaging
    restart: unless-stopped
```

**Modify** `.env.example`:

Add after the NATS section:
```
# Bots service
BOT_TOKEN_SECRET=change-me-to-a-random-64-char-string

# Service URLs (used by gateway for proxying)
BOTS_SERVICE_URL=http://localhost:8086
INTEGRATIONS_SERVICE_URL=http://localhost:8087
```

**Verify**: Read both files back to confirm valid YAML and no syntax errors.

---

## TASK-08: CHECKPOINT

**Run**: Verify docker-compose.yml is valid:
```bash
cd D:/job/orbit && docker compose config --quiet 2>&1 || echo "docker-compose validation failed"
```

(If docker is not available, just verify the YAML is syntactically correct by reading it.)

**Commit**: `feat(phase8): TASK-07 — docker-compose and env for bots/integrations`

---

## TASK-09: Bots service go.mod

**Modify** `services/bots/go.mod`:

Replace the stub go.mod with proper dependencies. Follow `services/messaging/go.mod` as pattern.

```
module github.com/mst-corp/orbit/services/bots

go 1.24

replace github.com/mst-corp/orbit/pkg => ../../pkg

require (
    github.com/gofiber/fiber/v2 v2.52.6
    github.com/google/uuid v1.6.0
    github.com/jackc/pgx/v5 v5.7.2
    github.com/mst-corp/orbit/pkg v0.0.0
    github.com/nats-io/nats.go v1.37.0
    github.com/redis/go-redis/v9 v9.7.0
    golang.org/x/crypto v0.31.0
)
```

**Run**: `cd services/bots && go mod tidy`

**Verify**: `cd services/bots && go build ./cmd/...` (should still build with existing stub main.go)

---

## TASK-10: Bots model package

**Create** `services/bots/internal/model/models.go`

Define these types (follow `services/messaging/internal/model/models.go` patterns):

```go
package model

// Bot represents a bot identity linked to a user account
type Bot struct {
    ID                uuid.UUID  `json:"id"`
    UserID            uuid.UUID  `json:"user_id"`
    OwnerID           uuid.UUID  `json:"owner_id"`
    Username          string     `json:"username"`
    DisplayName       string     `json:"display_name"`
    AvatarURL         *string    `json:"avatar_url,omitempty"`
    Description       *string    `json:"description,omitempty"`
    ShortDescription  *string    `json:"short_description,omitempty"`
    IsSystem          bool       `json:"is_system"`
    IsInline          bool       `json:"is_inline"`
    WebhookURL        *string    `json:"webhook_url,omitempty"`
    IsActive          bool       `json:"is_active"`
    CreatedAt         time.Time  `json:"created_at"`
    UpdatedAt         time.Time  `json:"updated_at"`
}

// BotToken — only token_prefix is exposed, never the hash
type BotToken struct {
    ID          uuid.UUID  `json:"id"`
    BotID       uuid.UUID  `json:"bot_id"`
    TokenPrefix string     `json:"token_prefix"`
    IsActive    bool       `json:"is_active"`
    LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
    CreatedAt   time.Time  `json:"created_at"`
}

// BotCommand — slash command registered for a bot
type BotCommand struct {
    ID          uuid.UUID `json:"id"`
    BotID       uuid.UUID `json:"bot_id"`
    Command     string    `json:"command"`
    Description string    `json:"description"`
    CreatedAt   time.Time `json:"created_at"`
}

// BotInstallation — bot installed in a specific chat
type BotInstallation struct {
    BotID       uuid.UUID `json:"bot_id"`
    ChatID      uuid.UUID `json:"chat_id"`
    InstalledBy uuid.UUID `json:"installed_by"`
    Scopes      int64     `json:"scopes"`
    IsActive    bool      `json:"is_active"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

Also define:
- Bot installation scope constants as `int64` bitmask: `ScopePostMessages = 1 << 0`, `ScopeReadCommands = 1 << 1`, `ScopeReceiveCallbacks = 1 << 2`, `ScopeReadMessages = 1 << 3`
- Sentinel errors: `ErrBotNotFound`, `ErrBotAlreadyExists`, `ErrTokenNotFound`, `ErrBotAlreadyInstalled`, `ErrBotNotInstalled`, `ErrInvalidToken`
- `CreateBotRequest` struct: `Username`, `DisplayName`, `Description`, `ShortDescription`

**Verify**: `cd services/bots && go build ./internal/model/...`

---

## TASK-11: Bot store

**Create** `services/bots/internal/store/bot_store.go`

**Pattern**: Follow `services/messaging/internal/store/message_store.go`

Interface `BotStore`:
```go
type BotStore interface {
    Create(ctx context.Context, bot *model.Bot) error
    GetByID(ctx context.Context, id uuid.UUID) (*model.Bot, error)
    GetByUserID(ctx context.Context, userID uuid.UUID) (*model.Bot, error)
    GetByUsername(ctx context.Context, username string) (*model.Bot, error)
    List(ctx context.Context, ownerID *uuid.UUID, limit int, offset int) ([]model.Bot, int, error)
    Update(ctx context.Context, bot *model.Bot) error
    Delete(ctx context.Context, id uuid.UUID) error
    CreateBotUser(ctx context.Context, username, displayName string) (uuid.UUID, error)
}
```

Implementation notes:
- `CreateBotUser`: INSERT into `users` table with `account_type = 'bot'`, `email` as generated `bot-{uuid}@orbit.internal`, `password_hash` as empty (bots don't login), `role = 'member'`. Return the created user ID.
- `Create`: INSERT into `bots` table. The `user_id` comes from `CreateBotUser`.
- `GetByID`: SELECT from `bots` JOIN `users` to get `username`, `display_name`, `avatar_url`.
- `List`: SELECT with optional `owner_id` filter, ORDER BY `created_at DESC`, with total count.
- `Delete`: DELETE from `bots` (CASCADE will delete tokens, commands, installations). Also DELETE from `users` WHERE `id = bot.user_id`.

**Verify**: `cd services/bots && go build ./internal/store/...`

---

## TASK-12: Token store

**Create** `services/bots/internal/store/token_store.go`

Interface `TokenStore`:
```go
type TokenStore interface {
    Create(ctx context.Context, botID uuid.UUID, tokenHash, tokenPrefix string) (*model.BotToken, error)
    GetByHash(ctx context.Context, tokenHash string) (*model.BotToken, error)
    RevokeAllForBot(ctx context.Context, botID uuid.UUID) error
    UpdateLastUsed(ctx context.Context, tokenID uuid.UUID) error
}
```

Implementation notes:
- `Create`: INSERT into `bot_tokens` with `is_active = true`. Before inserting, revoke all existing active tokens for this bot (one active token at a time).
- `GetByHash`: SELECT from `bot_tokens` WHERE `token_hash = $1 AND is_active = true`. This is the hot path for Bot API auth — must be indexed.
- `RevokeAllForBot`: UPDATE `bot_tokens` SET `is_active = false` WHERE `bot_id = $1`.
- `UpdateLastUsed`: UPDATE `bot_tokens` SET `last_used_at = NOW()` WHERE `id = $1`.

**Verify**: `cd services/bots && go build ./internal/store/...`

---

## TASK-13: Command store

**Create** `services/bots/internal/store/command_store.go`

Interface `CommandStore`:
```go
type CommandStore interface {
    SetCommands(ctx context.Context, botID uuid.UUID, commands []model.BotCommand) error
    GetCommands(ctx context.Context, botID uuid.UUID) ([]model.BotCommand, error)
    DeleteAllForBot(ctx context.Context, botID uuid.UUID) error
}
```

Implementation notes:
- `SetCommands`: DELETE all existing commands for bot, then batch INSERT new ones. Use a transaction.
- `GetCommands`: SELECT from `bot_commands` WHERE `bot_id = $1` ORDER BY `command`.

**Verify**: `cd services/bots && go build ./internal/store/...`

---

## TASK-14: Installation store

**Create** `services/bots/internal/store/installation_store.go`

Interface `InstallationStore`:
```go
type InstallationStore interface {
    Install(ctx context.Context, inst *model.BotInstallation) error
    Uninstall(ctx context.Context, botID, chatID uuid.UUID) error
    GetByBotAndChat(ctx context.Context, botID, chatID uuid.UUID) (*model.BotInstallation, error)
    ListByChat(ctx context.Context, chatID uuid.UUID) ([]model.BotInstallation, error)
    ListByBot(ctx context.Context, botID uuid.UUID) ([]model.BotInstallation, error)
    ListChatsWithWebhookBots(ctx context.Context, chatID uuid.UUID) ([]WebhookBotInfo, error)
}
```

Define `WebhookBotInfo` struct in the store or model package:
```go
type WebhookBotInfo struct {
    BotID      uuid.UUID
    UserID     uuid.UUID
    WebhookURL string
    Scopes     int64
}
```

Implementation notes:
- `Install`: INSERT into `bot_installations`. Also add bot as `chat_members` with role `'member'` (using bot's `user_id`). Use a transaction.
- `Uninstall`: UPDATE `bot_installations` SET `is_active = false`. Also DELETE from `chat_members` WHERE `user_id = bot.user_id AND chat_id = $2`. Use a transaction.
- `ListChatsWithWebhookBots`: JOIN `bot_installations` with `bots` WHERE `bots.webhook_url IS NOT NULL AND bot_installations.chat_id = $1 AND bot_installations.is_active = true`. This is used by the webhook delivery worker.

**Verify**: `cd services/bots && go build ./internal/store/...`

---

## TASK-15: CHECKPOINT

**Run**:
```bash
cd D:/job/orbit/services/bots && go build ./... && go vet ./...
```

Fix any compilation errors.

**Commit**: `feat(phase8): TASK-09..14 — bots service model and stores`

---

## TASK-16: Bot service layer

**Create** `services/bots/internal/service/bot_service.go`

**Pattern**: Follow `services/messaging/internal/service/message_service.go`

```go
type BotService struct {
    bots          store.BotStore
    tokens        store.TokenStore
    commands      store.CommandStore
    installations store.InstallationStore
    tokenSecret   string // used as HMAC key for token generation
}

func NewBotService(bots store.BotStore, tokens store.TokenStore, commands store.CommandStore, installations store.InstallationStore, tokenSecret string) *BotService
```

Methods:
- `CreateBot(ctx, ownerID, req model.CreateBotRequest) (*model.Bot, string, error)` — creates user, creates bot, generates token, returns (bot, rawToken, error). Raw token format: `bot_{botID}_{random32hex}`. Store SHA-256 hash. The raw token is returned ONCE and never stored.
- `GetBot(ctx, id) (*model.Bot, error)`
- `ListBots(ctx, ownerID *uuid.UUID, limit, offset int) ([]model.Bot, int, error)`
- `UpdateBot(ctx, id uuid.UUID, updates) (*model.Bot, error)`
- `DeleteBot(ctx, id uuid.UUID) error`
- `RotateToken(ctx, botID uuid.UUID) (string, error)` — revokes old, creates new, returns raw token
- `ValidateToken(ctx, rawToken string) (*model.Bot, error)` — hash the token, look up in token_store, return bot. Update last_used_at.
- `SetCommands(ctx, botID, commands []model.BotCommand) error`
- `GetCommands(ctx, botID) ([]model.BotCommand, error)`
- `InstallBot(ctx, botID, chatID, installedBy uuid.UUID, scopes int64) error`
- `UninstallBot(ctx, botID, chatID uuid.UUID) error`
- `ListChatBots(ctx, chatID uuid.UUID) ([]model.BotInstallation, error)`

Permission checks:
- Only owner or admin/superadmin can update/delete bot
- Only chat owner/admin can install/uninstall bot in chat
- System bots cannot be deleted

Token generation helper:
```go
func generateToken(botID uuid.UUID, secret string) (raw string, hash string) {
    random := make([]byte, 32)
    crypto/rand.Read(random)
    raw = fmt.Sprintf("bot_%s_%s", botID.String()[:8], hex.EncodeToString(random))
    h := sha256.Sum256([]byte(raw))
    hash = hex.EncodeToString(h[:])
    return raw, hash
}
```

**Verify**: `cd services/bots && go build ./internal/service/...`

---

## TASK-17: Bot handler — CRUD endpoints

**Create** `services/bots/internal/handler/bot_handler.go`

**Pattern**: Follow `services/messaging/internal/handler/message_handler.go`

```go
type BotHandler struct {
    svc    *service.BotService
    logger *slog.Logger
}

func NewBotHandler(svc *service.BotService, logger *slog.Logger) *BotHandler

func (h *BotHandler) Register(router fiber.Router) {
    router.Post("/bots", h.createBot)
    router.Get("/bots", h.listBots)
    router.Get("/bots/:id", h.getBot)
    router.Patch("/bots/:id", h.updateBot)
    router.Delete("/bots/:id", h.deleteBot)
}
```

Each handler:
1. Extract `userID` and `userRole` from headers (getUserID pattern)
2. Check system permission: `permissions.HasSysPermission(userRole, permissions.SysManageBots)`
3. Parse request body / params
4. Validate input with `pkg/validator`
5. Call service method
6. Return via `response.JSON` or `response.Error`

For `createBot`: return `{"bot": {...}, "token": "bot_xxx_yyy"}` — token is included only on creation.

**Verify**: `cd services/bots && go build ./internal/handler/...`

---

## TASK-18: Token handler

**Create** `services/bots/internal/handler/token_handler.go`

Register on the same router (call from BotHandler.Register or separately):
```go
router.Post("/bots/:id/token/rotate", h.rotateToken)
```

`rotateToken`:
1. Require `SysManageBots` permission OR bot ownership
2. Call `svc.RotateToken(ctx, botID)`
3. Return `{"token": "bot_xxx_newyyy"}`

**Verify**: `cd services/bots && go build ./internal/handler/...`

---

## TASK-19: Command handler

**Create** `services/bots/internal/handler/command_handler.go`

```go
router.Put("/bots/:id/commands", h.setCommands)    // replace all commands
router.Get("/bots/:id/commands", h.getCommands)     // list commands
```

`setCommands` body: `{"commands": [{"command": "start", "description": "Start the bot"}, ...]}`

Validate: command max 32 chars, description max 256 chars, max 100 commands per bot. Command must match `^[a-z0-9_]+$`.

**Verify**: `cd services/bots && go build ./internal/handler/...`

---

## TASK-20: Installation handler

**Create** `services/bots/internal/handler/installation_handler.go`

```go
router.Post("/bots/:id/install", h.installBot)      // install bot in chat
router.Delete("/bots/:id/install", h.uninstallBot)   // remove bot from chat
router.Get("/chats/:chatId/bots", h.listChatBots)    // list bots in chat
```

`installBot` body: `{"chat_id": "uuid", "scopes": 3}` (bitmask)

Permission: caller must be owner/admin of the target chat. Check via `X-User-Role` or by querying messaging service for chat membership.

For v1, simplify: trust `X-User-Role` for admin/superadmin. For regular users, the installation endpoint requires `SysManageBots` permission. Chat-level permission checking can be added later.

**Verify**: `cd services/bots && go build ./internal/handler/...`

---

## TASK-21: Bots cmd/main.go — full wiring

**Replace** `services/bots/cmd/main.go`

**Pattern**: Follow `services/messaging/cmd/main.go` structure:

1. Load config: `PORT`, `DATABASE_URL`, `REDIS_URL`, `ORBIT_NATS_URL`, `INTERNAL_SECRET`, `MESSAGING_SERVICE_URL`, `MEDIA_SERVICE_URL`, `BOT_TOKEN_SECRET`
2. Connect pgxpool
3. Connect Redis
4. Connect NATS
5. Create stores: `NewBotStore(pool)`, `NewTokenStore(pool)`, `NewCommandStore(pool)`, `NewInstallationStore(pool)`
6. Create service: `NewBotService(stores..., botTokenSecret)`
7. Create Fiber app with standard config
8. Register health endpoint: `GET /health`
9. Create API group: `app.Group("/api/v1")`
10. Register BotHandler on API group
11. Start server on PORT (default 8086)
12. Graceful shutdown on SIGTERM/SIGINT

Keep the health endpoint as-is. The bot API routes (token-authenticated) will be added in a later task.

**Verify**: `cd services/bots && go build ./cmd/...`

---

## TASK-22: CHECKPOINT

**Run**:
```bash
cd D:/job/orbit/services/bots && go build ./... && go vet ./...
```

**Commit**: `feat(phase8): TASK-16..21 — bots service layer, handlers, and wiring`

---

## TASK-23: Messaging client for inter-service calls

**Create** `services/bots/internal/client/messaging_client.go`

This client allows the bots service to send/edit/delete messages by calling the messaging service's internal HTTP API.

```go
package client

type MessagingClient struct {
    baseURL       string // e.g., "http://messaging:8082"
    internalToken string
    httpClient    *http.Client
}

func NewMessagingClient(baseURL, internalToken string) *MessagingClient

// SendMessage sends a message to a chat as the given bot user
func (c *MessagingClient) SendMessage(ctx context.Context, botUserID, chatID uuid.UUID, content string, msgType string, replyMarkup json.RawMessage, replyToID *uuid.UUID) (*MessageResponse, error)

// EditMessage edits an existing message
func (c *MessagingClient) EditMessage(ctx context.Context, botUserID, messageID uuid.UUID, content string, replyMarkup json.RawMessage) (*MessageResponse, error)

// DeleteMessage deletes a message
func (c *MessagingClient) DeleteMessage(ctx context.Context, botUserID, messageID uuid.UUID) error
```

Implementation:
- Set headers: `X-Internal-Token`, `X-User-ID` (bot's user_id), `X-User-Role: member`, `Content-Type: application/json`
- POST to `{baseURL}/api/v1/chats/{chatID}/messages` for send
- PATCH to `{baseURL}/api/v1/messages/{messageID}` for edit
- DELETE to `{baseURL}/api/v1/messages/{messageID}` for delete
- Use `http.Client` with 10s timeout
- Parse response, return structured result or error

Define `MessageResponse`:
```go
type MessageResponse struct {
    ID             uuid.UUID       `json:"id"`
    ChatID         uuid.UUID       `json:"chat_id"`
    Content        string          `json:"content"`
    Type           string          `json:"type"`
    SequenceNumber int64           `json:"sequence_number"`
    CreatedAt      time.Time       `json:"created_at"`
}
```

**Verify**: `cd services/bots && go build ./internal/client/...`

---

## TASK-24: Bot API auth middleware

**Create** `services/bots/internal/botapi/middleware.go`

This middleware extracts the bot token from the URL path, validates it, and sets bot info in Fiber locals.

```go
package botapi

func TokenAuthMiddleware(svc *service.BotService) fiber.Handler {
    return func(c *fiber.Ctx) error {
        token := c.Params("token")
        if token == "" {
            return response.Error(c, apperror.Unauthorized("Missing bot token"))
        }

        bot, err := svc.ValidateToken(c.Context(), token)
        if err != nil {
            return response.Error(c, apperror.Unauthorized("Invalid bot token"))
        }

        if !bot.IsActive {
            return response.Error(c, apperror.Forbidden("Bot is deactivated"))
        }

        c.Locals("bot", bot)
        c.Locals("bot_user_id", bot.UserID)
        return c.Next()
    }
}
```

**Verify**: `cd services/bots && go build ./internal/botapi/...`

---

## TASK-25: Bot API models

**Create** `services/bots/internal/botapi/models.go`

Define Telegram-compatible request/response types for the Bot API:

```go
package botapi

// SendMessageRequest — Telegram-compatible
type SendMessageRequest struct {
    ChatID      string          `json:"chat_id"`      // UUID string
    Text        string          `json:"text"`
    ReplyMarkup json.RawMessage `json:"reply_markup,omitempty"` // InlineKeyboardMarkup
    ReplyToMessageID *string    `json:"reply_to_message_id,omitempty"`
}

// EditMessageRequest
type EditMessageRequest struct {
    ChatID    string          `json:"chat_id"`
    MessageID string          `json:"message_id"`
    Text      string          `json:"text"`
    ReplyMarkup json.RawMessage `json:"reply_markup,omitempty"`
}

// DeleteMessageRequest
type DeleteMessageRequest struct {
    ChatID    string `json:"chat_id"`
    MessageID string `json:"message_id"`
}

// AnswerCallbackQueryRequest
type AnswerCallbackQueryRequest struct {
    CallbackQueryID string `json:"callback_query_id"`
    Text            string `json:"text,omitempty"`
    ShowAlert       bool   `json:"show_alert,omitempty"`
}

// SetWebhookRequest
type SetWebhookRequest struct {
    URL    string `json:"url"`
    Secret string `json:"secret,omitempty"`
}

// BotAPIResponse — standard wrapper
type BotAPIResponse struct {
    OK          bool        `json:"ok"`
    Result      interface{} `json:"result,omitempty"`
    Description string      `json:"description,omitempty"`
    ErrorCode   int         `json:"error_code,omitempty"`
}

// InlineKeyboardMarkup
type InlineKeyboardMarkup struct {
    InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// InlineKeyboardButton
type InlineKeyboardButton struct {
    Text         string `json:"text"`
    CallbackData string `json:"callback_data,omitempty"`
    URL          string `json:"url,omitempty"`
}

// Update — delivered to bot webhooks or via getUpdates
type Update struct {
    UpdateID      int64          `json:"update_id"`
    Message       *APIMessage    `json:"message,omitempty"`
    CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

// APIMessage — simplified message for bot consumption
type APIMessage struct {
    MessageID      string    `json:"message_id"`
    ChatID         string    `json:"chat_id"`
    FromID         string    `json:"from_id"`
    FromName       string    `json:"from_name"`
    Text           string    `json:"text"`
    Date           int64     `json:"date"` // unix timestamp
    ReplyToMessage *APIMessage `json:"reply_to_message,omitempty"`
}

// CallbackQuery
type CallbackQuery struct {
    ID      string     `json:"id"`
    FromID  string     `json:"from_id"`
    Message *APIMessage `json:"message,omitempty"`
    Data    string     `json:"data"`
}
```

**Verify**: `cd services/bots && go build ./internal/botapi/...`

---

## TASK-26: Bot API handler — getMe, sendMessage, editMessage, deleteMessage

**Create** `services/bots/internal/botapi/handler.go`

```go
type BotAPIHandler struct {
    svc       *service.BotService
    msgClient *client.MessagingClient
    logger    *slog.Logger
}

func NewBotAPIHandler(svc *service.BotService, msgClient *client.MessagingClient, logger *slog.Logger) *BotAPIHandler

func (h *BotAPIHandler) Register(router fiber.Router) {
    // All routes are under /bot/:token/ — middleware already validated token
    router.Get("/getMe", h.getMe)
    router.Post("/sendMessage", h.sendMessage)
    router.Post("/editMessageText", h.editMessageText)
    router.Post("/deleteMessage", h.deleteMessage)
}
```

`getMe`: return bot info from `c.Locals("bot")`.

`sendMessage`:
1. Parse `SendMessageRequest`
2. Validate: `chat_id` required, `text` required (max 4096 chars)
3. Check bot is installed in this chat (query installation store)
4. Call `msgClient.SendMessage(ctx, botUserID, chatID, text, "text", replyMarkup, replyToID)`
5. Return `BotAPIResponse{OK: true, Result: message}`

`editMessageText`:
1. Parse `EditMessageRequest`
2. Call `msgClient.EditMessage(ctx, botUserID, messageID, text, replyMarkup)`
3. Return response

`deleteMessage`:
1. Parse `DeleteMessageRequest`
2. Call `msgClient.DeleteMessage(ctx, botUserID, messageID)`
3. Return `BotAPIResponse{OK: true, Result: true}`

All errors return `BotAPIResponse{OK: false, ErrorCode: statusCode, Description: message}`.

**Verify**: `cd services/bots && go build ./internal/botapi/...`

---

## TASK-27: Bot API callback handler

**Create** `services/bots/internal/botapi/callback_handler.go`

Add to BotAPIHandler.Register:
```go
router.Post("/answerCallbackQuery", h.answerCallbackQuery)
```

`answerCallbackQuery`:
1. Parse `AnswerCallbackQueryRequest`
2. For v1, simply acknowledge the callback. Store acknowledgment in Redis with TTL 60s: key `callback_ack:{callbackQueryID}`, value `{text, show_alert}`.
3. Return `BotAPIResponse{OK: true, Result: true}`

Note: The actual callback delivery to the user's UI happens through the WebSocket layer. For v1, callback acknowledgment is a no-op on the server side — the frontend handles the UI response locally.

**Verify**: `cd services/bots && go build ./internal/botapi/...`

---

## TASK-28: Bot API webhook handler

**Create** `services/bots/internal/botapi/webhook_handler.go`

Add to BotAPIHandler.Register:
```go
router.Post("/setWebhook", h.setWebhook)
router.Post("/deleteWebhook", h.deleteWebhook)
router.Post("/getWebhookInfo", h.getWebhookInfo)
```

`setWebhook`:
1. Parse `SetWebhookRequest`
2. Validate URL: must be HTTPS (unless localhost for dev), max 2048 chars. Reject private IPs (10.x, 172.16-31.x, 192.168.x, 127.x, ::1) for SSRF protection.
3. If `secret` provided, hash with SHA-256 and store as `webhook_secret_hash` on bots table.
4. Update `bots.webhook_url` via bot store.
5. Return `BotAPIResponse{OK: true, Result: true}`

`deleteWebhook`:
1. Set `bots.webhook_url = NULL`, `webhook_secret_hash = NULL`
2. Return success

`getWebhookInfo`:
1. Return `{"url": "...", "has_custom_certificate": false, "pending_update_count": 0}`

**Verify**: `cd services/bots && go build ./internal/botapi/...`

---

## TASK-29: Bot API updates handler

**Create** `services/bots/internal/botapi/updates_handler.go`

Add to BotAPIHandler.Register:
```go
router.Post("/getUpdates", h.getUpdates)
```

`getUpdates`:
1. Parse optional `offset` (int64), `limit` (int, default 100, max 100), `timeout` (int seconds, default 0, max 50)
2. Bot must NOT have a webhook set (return error if webhook_url is set)
3. Read updates from Redis list: key `bot_updates:{botID}`
4. If `timeout > 0` and no updates, use Redis BLPOP with timeout for long polling
5. If `offset` provided, remove all updates with `update_id < offset` from the queue
6. Return `BotAPIResponse{OK: true, Result: []Update{...}}`

Redis key pattern for updates: `bot_updates:{botID}` — a Redis List. Updates are JSON-encoded `Update` structs. TTL on individual updates is managed by the update queue service (TASK-32).

**Verify**: `cd services/bots && go build ./internal/botapi/...`

---

## TASK-30: CHECKPOINT

**Run**:
```bash
cd D:/job/orbit/services/bots && go build ./... && go vet ./...
```

Also add bot API route registration to `cmd/main.go`:
```go
// Bot API routes (token-authenticated, no JWT)
botAPIGroup := app.Group("/bot/:token", botapi.TokenAuthMiddleware(botService))
botAPIHandler := botapi.NewBotAPIHandler(botService, msgClient, logger)
botAPIHandler.Register(botAPIGroup)
```

Where `msgClient = client.NewMessagingClient(messagingURL, internalSecret)`.

**Commit**: `feat(phase8): TASK-23..29 — Bot API layer (sendMessage, editMessage, webhooks, getUpdates)`

---

## TASK-31: NATS subscriber for bot events

**Create** `services/bots/internal/service/nats_subscriber.go`

Subscribe to NATS subjects to capture events relevant to installed bots:

```go
type BotNATSSubscriber struct {
    nc            *nats.Conn
    installations store.InstallationStore
    webhookWorker *WebhookWorker
    updateQueue   *UpdateQueue
    logger        *slog.Logger
}

func NewBotNATSSubscriber(nc *nats.Conn, installations store.InstallationStore, webhookWorker *WebhookWorker, updateQueue *UpdateQueue, logger *slog.Logger) *BotNATSSubscriber

func (s *BotNATSSubscriber) Start() error
```

Subscribe to:
- `orbit.chat.*.message.new` — new message in a chat
- `orbit.chat.*.member.*` — member changes (bot added/removed)

Handler logic:
1. Parse `NATSEvent` from message data
2. Extract `chatID` from subject (third segment)
3. Query `installations.ListChatsWithWebhookBots(ctx, chatID)` to find bots installed in this chat
4. Skip if sender_id matches bot's user_id (don't echo bot's own messages back)
5. For each installed bot:
   - If bot has webhook: enqueue for webhook delivery via `webhookWorker.Enqueue(bot, update)`
   - If bot has NO webhook (getUpdates mode): push to `updateQueue.Push(botID, update)`

**Verify**: `cd services/bots && go build ./internal/service/...`

---

## TASK-32: Webhook delivery worker

**Create** `services/bots/internal/service/webhook_worker.go`

```go
type WebhookWorker struct {
    redis  *redis.Client
    logger *slog.Logger
}

func NewWebhookWorker(redis *redis.Client, logger *slog.Logger) *WebhookWorker

// Enqueue adds an update to the webhook delivery queue
func (w *WebhookWorker) Enqueue(botID uuid.UUID, webhookURL string, secretHash string, update botapi.Update) error

// Start begins processing the delivery queue
func (w *WebhookWorker) Start(ctx context.Context)

// deliverWebhook sends an HTTP POST to the webhook URL
func (w *WebhookWorker) deliverWebhook(webhookURL string, secretHash string, payload []byte) error
```

Implementation:
- Use Redis List `webhook_queue:{botID}` for pending deliveries
- Worker goroutine: BLPOP from queue, process delivery
- HTTP POST to webhook URL with `Content-Type: application/json`
- If webhook has a secret, include `X-Orbit-Signature` header: HMAC-SHA256 of the body using the secret
- Retry on failure: 3 attempts with exponential backoff (1s, 5s, 25s)
- On final failure: log error, increment failure counter in Redis
- `http.Client` with 10s timeout
- SSRF protection: resolve URL, reject private IPs before connecting

**Verify**: `cd services/bots && go build ./internal/service/...`

---

## TASK-33: Update queue for getUpdates

**Create** `services/bots/internal/service/update_queue.go`

```go
type UpdateQueue struct {
    redis *redis.Client
}

func NewUpdateQueue(redis *redis.Client) *UpdateQueue

// Push adds an update to the bot's update queue
func (q *UpdateQueue) Push(botID uuid.UUID, update botapi.Update) error

// Pop retrieves and removes updates from the queue
func (q *UpdateQueue) Pop(ctx context.Context, botID uuid.UUID, limit int, timeout time.Duration) ([]botapi.Update, error)

// Ack removes all updates with update_id < offset
func (q *UpdateQueue) Ack(botID uuid.UUID, offset int64) error
```

Implementation:
- Redis key: `bot_updates:{botID}` (List)
- `Push`: RPUSH JSON-encoded update. Set TTL on first push (24h).
- `Pop`: If `timeout > 0`, use BLPOP. Otherwise LRANGE + LTRIM.
- Update IDs: use Redis INCR on `bot_update_seq:{botID}` to generate monotonic update_id.
- Max queue size: 100 updates per bot. LTRIM after push.

**Verify**: `cd services/bots && go build ./internal/service/...`

---

## TASK-34: CHECKPOINT

**Run**:
```bash
cd D:/job/orbit/services/bots && go build ./... && go vet ./...
```

Wire NATS subscriber and workers into `cmd/main.go`:
```go
updateQueue := service.NewUpdateQueue(redisClient)
webhookWorker := service.NewWebhookWorker(redisClient, logger)
natsSubscriber := service.NewBotNATSSubscriber(nc, installationStore, webhookWorker, updateQueue, logger)
natsSubscriber.Start()
go webhookWorker.Start(ctx)
```

Also pass `updateQueue` to the BotAPIHandler for getUpdates.

**Commit**: `feat(phase8): TASK-31..33 — bot NATS subscriber, webhook delivery, update queue`

---

## TASK-35: Mock stores for bots tests

**Create** `services/bots/internal/handler/mock_stores_test.go`

**Pattern**: Follow `services/messaging/internal/handler/mock_stores_test.go` (fn-field pattern)

Create mock implementations for all 4 store interfaces:
- `mockBotStore` with fn-fields for each method
- `mockTokenStore`
- `mockCommandStore`
- `mockInstallationStore`

Each mock method checks if the fn-field is set, calls it if so, otherwise returns zero values.

**Verify**: `cd services/bots && go build ./internal/handler/...`

---

## TASK-36: Bot handler tests

**Create** `services/bots/internal/handler/bot_handler_test.go`

**Pattern**: Follow existing test patterns in the project.

Write tests for BotHandler:
1. `TestCreateBot_Success` — valid request, returns bot + token
2. `TestCreateBot_Unauthorized` — missing user context → 401
3. `TestCreateBot_Forbidden` — member role without SysManageBots → 403
4. `TestCreateBot_ValidationError` — missing username → 400
5. `TestGetBot_Success` — returns bot info
6. `TestGetBot_NotFound` — invalid ID → 404
7. `TestDeleteBot_Success` — admin deletes bot
8. `TestDeleteBot_SystemBot` — cannot delete system bot → 403

Use `httptest` + `fiber.Test()` or create Fiber app with handler registered, send requests, assert status and body.

**Verify**: `cd services/bots && go test ./internal/handler/... -v`

---

## TASK-37: CHECKPOINT

**Run**:
```bash
cd D:/job/orbit/services/bots && go test ./... -v
```

Fix any test failures.

**Commit**: `feat(phase8): TASK-35..36 — bots handler tests`

---

## TASK-38: Integrations service go.mod

**Modify** `services/integrations/go.mod`:

Same pattern as bots go.mod:
```
module github.com/mst-corp/orbit/services/integrations

go 1.24

replace github.com/mst-corp/orbit/pkg => ../../pkg

require (
    github.com/gofiber/fiber/v2 v2.52.6
    github.com/google/uuid v1.6.0
    github.com/jackc/pgx/v5 v5.7.2
    github.com/mst-corp/orbit/pkg v0.0.0
    github.com/nats-io/nats.go v1.37.0
    github.com/redis/go-redis/v9 v9.7.0
    golang.org/x/crypto v0.31.0
)
```

**Run**: `cd services/integrations && go mod tidy`

**Verify**: `cd services/integrations && go build ./cmd/...`

---

## TASK-39: Integrations model package

**Create** `services/integrations/internal/model/models.go`

```go
package model

type Connector struct {
    ID          uuid.UUID  `json:"id"`
    Name        string     `json:"name"`
    DisplayName string     `json:"display_name"`
    Type        string     `json:"type"` // inbound_webhook, outbound_webhook, polling
    BotID       *uuid.UUID `json:"bot_id,omitempty"`
    Config      JSONB      `json:"config"`
    IsActive    bool       `json:"is_active"`
    CreatedBy   uuid.UUID  `json:"created_by"`
    CreatedAt   time.Time  `json:"created_at"`
    UpdatedAt   time.Time  `json:"updated_at"`
}

type Route struct {
    ID          uuid.UUID  `json:"id"`
    ConnectorID uuid.UUID  `json:"connector_id"`
    ChatID      uuid.UUID  `json:"chat_id"`
    EventFilter *string    `json:"event_filter,omitempty"`
    Template    *string    `json:"template,omitempty"`
    IsActive    bool       `json:"is_active"`
    CreatedAt   time.Time  `json:"created_at"`
    UpdatedAt   time.Time  `json:"updated_at"`
}

type Delivery struct {
    ID              uuid.UUID  `json:"id"`
    ConnectorID     uuid.UUID  `json:"connector_id"`
    RouteID         *uuid.UUID `json:"route_id,omitempty"`
    ExternalEventID *string    `json:"external_event_id,omitempty"`
    EventType       string     `json:"event_type"`
    Payload         JSONB      `json:"payload"`
    Status          string     `json:"status"` // pending, delivered, failed, dead_letter
    OrbitMessageID  *uuid.UUID `json:"orbit_message_id,omitempty"`
    CorrelationKey  *string    `json:"correlation_key,omitempty"`
    AttemptCount    int        `json:"attempt_count"`
    MaxAttempts     int        `json:"max_attempts"`
    LastError       *string    `json:"last_error,omitempty"`
    NextRetryAt     *time.Time `json:"next_retry_at,omitempty"`
    DeliveredAt     *time.Time `json:"delivered_at,omitempty"`
    CreatedAt       time.Time  `json:"created_at"`
}

// JSONB is a helper for JSONB columns
type JSONB json.RawMessage
// Implement sql.Scanner and driver.Valuer for JSONB

// Sentinel errors
var (
    ErrConnectorNotFound = errors.New("connector not found")
    ErrRouteNotFound     = errors.New("route not found")
    ErrDuplicateRoute    = errors.New("route already exists for this connector and chat")
    ErrInvalidSignature  = errors.New("invalid webhook signature")
)

// CreateConnectorRequest
type CreateConnectorRequest struct {
    Name        string  `json:"name"`
    DisplayName string  `json:"display_name"`
    Type        string  `json:"type"`
    BotID       *string `json:"bot_id,omitempty"`
}
```

**Verify**: `cd services/integrations && go build ./internal/model/...`

---

## TASK-40: Connector store

**Create** `services/integrations/internal/store/connector_store.go`

Interface `ConnectorStore`:
```go
type ConnectorStore interface {
    Create(ctx context.Context, c *model.Connector) error
    GetByID(ctx context.Context, id uuid.UUID) (*model.Connector, error)
    GetByName(ctx context.Context, name string) (*model.Connector, error)
    List(ctx context.Context, limit, offset int) ([]model.Connector, int, error)
    Update(ctx context.Context, c *model.Connector) error
    Delete(ctx context.Context, id uuid.UUID) error
    GetSecretHash(ctx context.Context, id uuid.UUID) (string, error)
    SetSecretHash(ctx context.Context, id uuid.UUID, hash string) error
}
```

Implementation: standard pgx pattern. `GetSecretHash` is a separate method to avoid leaking the hash in normal reads.

**Verify**: `cd services/integrations && go build ./internal/store/...`

---

## TASK-41: Route store

**Create** `services/integrations/internal/store/route_store.go`

Interface `RouteStore`:
```go
type RouteStore interface {
    Create(ctx context.Context, r *model.Route) error
    GetByID(ctx context.Context, id uuid.UUID) (*model.Route, error)
    ListByConnector(ctx context.Context, connectorID uuid.UUID) ([]model.Route, error)
    ListByChat(ctx context.Context, chatID uuid.UUID) ([]model.Route, error)
    Update(ctx context.Context, r *model.Route) error
    Delete(ctx context.Context, id uuid.UUID) error
    FindMatchingRoutes(ctx context.Context, connectorID uuid.UUID, eventType string) ([]model.Route, error)
}
```

`FindMatchingRoutes`: SELECT routes WHERE `connector_id = $1 AND is_active = true` and optionally filter by `event_filter` pattern match against `eventType`. For v1, simple LIKE or exact match is fine.

**Verify**: `cd services/integrations && go build ./internal/store/...`

---

## TASK-42: Delivery store

**Create** `services/integrations/internal/store/delivery_store.go`

Interface `DeliveryStore`:
```go
type DeliveryStore interface {
    Create(ctx context.Context, d *model.Delivery) error
    GetByID(ctx context.Context, id uuid.UUID) (*model.Delivery, error)
    ListByConnector(ctx context.Context, connectorID uuid.UUID, limit, offset int) ([]model.Delivery, int, error)
    UpdateStatus(ctx context.Context, id uuid.UUID, status string, lastError *string, nextRetryAt *time.Time, orbitMessageID *uuid.UUID) error
    GetPendingRetries(ctx context.Context, limit int) ([]model.Delivery, error)
    FindByCorrelation(ctx context.Context, connectorID uuid.UUID, correlationKey string) (*model.Delivery, error)
    FindByExternalID(ctx context.Context, connectorID uuid.UUID, externalEventID string) (*model.Delivery, error)
    MarkDeadLetter(ctx context.Context, id uuid.UUID, lastError string) error
}
```

`GetPendingRetries`: SELECT WHERE `status IN ('pending', 'failed') AND next_retry_at <= NOW()` ORDER BY `next_retry_at` LIMIT $1.

`FindByCorrelation`: used for the edit-in-place pattern — find existing delivery with same correlation_key to update the message instead of creating a new one.

**Verify**: `cd services/integrations && go build ./internal/store/...`

---

## TASK-43: CHECKPOINT

**Run**:
```bash
cd D:/job/orbit/services/integrations && go build ./... && go vet ./...
```

**Commit**: `feat(phase8): TASK-38..42 — integrations service model and stores`

---

## TASK-44: Integration service layer

**Create** `services/integrations/internal/service/integration_service.go`

```go
type IntegrationService struct {
    connectors store.ConnectorStore
    routes     store.RouteStore
    deliveries store.DeliveryStore
    msgClient  *client.MessagingClient // same client type as bots service
    logger     *slog.Logger
}

func NewIntegrationService(connectors store.ConnectorStore, routes store.RouteStore, deliveries store.DeliveryStore, msgClient *client.MessagingClient, logger *slog.Logger) *IntegrationService
```

Methods:
- `CreateConnector(ctx, createdBy, req) (*model.Connector, string, error)` — create connector, generate webhook secret (random 32 bytes hex), hash it, store hash, return (connector, rawSecret, error). Secret shown once.
- `GetConnector(ctx, id) (*model.Connector, error)`
- `ListConnectors(ctx, limit, offset) ([]model.Connector, int, error)`
- `UpdateConnector(ctx, id, updates) (*model.Connector, error)`
- `DeleteConnector(ctx, id) error`
- `RotateSecret(ctx, id) (string, error)` — new secret, hash, store
- `CreateRoute(ctx, route) (*model.Route, error)`
- `DeleteRoute(ctx, id) error`
- `ProcessInboundWebhook(ctx, connectorID, eventType, payload, signature, correlationKey, externalEventID) error` — main entry point for inbound webhooks:
  1. Verify HMAC signature if connector has secret_hash
  2. Check idempotency via `FindByExternalID` — if already delivered, skip
  3. Find matching routes via `FindMatchingRoutes`
  4. For each route: create delivery record, format message from template + payload, send to chat via msgClient
  5. If `correlationKey` provided: find existing delivery with same key, edit existing message instead of sending new
  6. Update delivery status to 'delivered' or 'failed'

**Verify**: `cd services/integrations && go build ./internal/service/...`

---

## TASK-45: Delivery retry worker

**Create** `services/integrations/internal/service/delivery_worker.go`

```go
type DeliveryWorker struct {
    deliveries store.DeliveryStore
    msgClient  *client.MessagingClient
    logger     *slog.Logger
}

func NewDeliveryWorker(deliveries store.DeliveryStore, msgClient *client.MessagingClient, logger *slog.Logger) *DeliveryWorker

func (w *DeliveryWorker) Start(ctx context.Context)
```

Worker loop:
1. Every 10 seconds: query `GetPendingRetries(limit=50)`
2. For each delivery: attempt redelivery
3. On success: update status to 'delivered'
4. On failure: increment attempt_count, set next_retry_at with exponential backoff (30s, 2m, 10m, 1h, 6h)
5. If attempt_count >= max_attempts: call `MarkDeadLetter`
6. Log all attempts with delivery ID, connector ID, attempt count, error

**Verify**: `cd services/integrations && go build ./internal/service/...`

---

## TASK-46: Connector management handler

**Create** `services/integrations/internal/handler/connector_handler.go`

```go
type ConnectorHandler struct {
    svc    *service.IntegrationService
    logger *slog.Logger
}

func (h *ConnectorHandler) Register(router fiber.Router) {
    router.Post("/integrations/connectors", h.createConnector)
    router.Get("/integrations/connectors", h.listConnectors)
    router.Get("/integrations/connectors/:id", h.getConnector)
    router.Patch("/integrations/connectors/:id", h.updateConnector)
    router.Delete("/integrations/connectors/:id", h.deleteConnector)
    router.Post("/integrations/connectors/:id/rotate-secret", h.rotateSecret)
    router.Post("/integrations/connectors/:id/routes", h.createRoute)
    router.Delete("/integrations/routes/:id", h.deleteRoute)
    router.Get("/integrations/connectors/:id/routes", h.listRoutes)
}
```

All endpoints require JWT auth + `SysManageIntegrations` permission (checked via `X-User-Role` header and `permissions.HasSysPermission`).

For `createConnector`: validate name (alphanumeric + hyphens, 3-64 chars), display_name (1-128 chars), type (must be valid enum).

Return secret only on create and rotate.

**Verify**: `cd services/integrations && go build ./internal/handler/...`

---

## TASK-47: Inbound webhook handler

**Create** `services/integrations/internal/handler/webhook_handler.go`

```go
func (h *ConnectorHandler) RegisterPublic(router fiber.Router) {
    router.Post("/webhooks/in/:connectorId", h.receiveWebhook)
}
```

`receiveWebhook`:
1. Extract `connectorId` from URL params
2. Get connector from store
3. Check connector is active
4. Read raw body
5. If connector has `secret_hash`: verify HMAC-SHA256 signature from `X-Orbit-Signature` header. Signature = hex(HMAC-SHA256(secret, body)). Also check `X-Orbit-Timestamp` for replay protection (reject if older than 5 minutes).
6. Parse body as JSON
7. Extract `event_type` from JSON (field name: `event` or `type` or `event_type` — try all three)
8. Extract optional `correlation_key` and `external_event_id` from JSON
9. Call `svc.ProcessInboundWebhook(ctx, connectorID, eventType, payload, signature, correlationKey, externalEventID)`
10. Return `200 {"ok": true}`

Rate limit: 60 requests/minute per connector (use Redis key `ratelimit:webhook:{connectorID}`).

**Verify**: `cd services/integrations && go build ./internal/handler/...`

---

## TASK-48: Delivery log handler

**Create** `services/integrations/internal/handler/delivery_handler.go`

Add to ConnectorHandler.Register:
```go
router.Get("/integrations/connectors/:id/deliveries", h.listDeliveries)
router.Get("/integrations/deliveries/:id", h.getDelivery)
router.Post("/integrations/deliveries/:id/retry", h.retryDelivery)
```

Requires `SysViewBotLogs` or `SysManageIntegrations` permission.

`listDeliveries`: paginated list with offset/limit, filter by status.
`retryDelivery`: reset delivery status to 'pending', set next_retry_at = NOW(), reset attempt_count.

**Verify**: `cd services/integrations && go build ./internal/handler/...`

---

## TASK-49: Integrations cmd/main.go — full wiring

**Replace** `services/integrations/cmd/main.go`

**Pattern**: Same as bots cmd/main.go (TASK-21).

1. Load config: `PORT`, `DATABASE_URL`, `REDIS_URL`, `ORBIT_NATS_URL`, `INTERNAL_SECRET`, `MESSAGING_SERVICE_URL`
2. Connect pgxpool, Redis, NATS
3. Create stores
4. Create messaging client (same `client.MessagingClient` type — copy from bots or extract to shared location)
5. Create service + delivery worker
6. Create Fiber app
7. Register health endpoint
8. Create API group, register ConnectorHandler (JWT-authenticated routes)
9. Register public webhook route: `app.Post("/api/v1/webhooks/in/:connectorId", ...)`
10. Start delivery worker goroutine
11. Start server, graceful shutdown

Note: The `client.MessagingClient` needs to exist in the integrations service too. Either:
- Copy the file from `services/bots/internal/client/` to `services/integrations/internal/client/`
- Or create a minimal version in integrations

For now, copy the file. Both services need it independently.

**Verify**: `cd services/integrations && go build ./cmd/...`

---

## TASK-50: CHECKPOINT

**Run**:
```bash
cd D:/job/orbit/services/integrations && go build ./... && go vet ./...
```

**Commit**: `feat(phase8): TASK-44..49 — integrations service layer, handlers, and wiring`

---

## TASK-51: Gateway proxy — add bots and integrations routes

**Modify** `services/gateway/internal/handler/proxy.go`:

1. Add to `ProxyConfig`:
```go
BotsServiceURL         string
IntegrationsServiceURL string
```

2. In `SetupProxy`, add BEFORE the catch-all `apiGroup.All("/*", ...)`:

```go
// Bots management (JWT-authenticated)
apiGroup.All("/bots/*", func(c *fiber.Ctx) error {
    return doProxy(c, cfg.BotsServiceURL, cfg.InternalSecret)
})
apiGroup.All("/bots", func(c *fiber.Ctx) error {
    return doProxy(c, cfg.BotsServiceURL, cfg.InternalSecret)
})

// Integrations management (JWT-authenticated)
apiGroup.All("/integrations/*", func(c *fiber.Ctx) error {
    return doProxy(c, cfg.IntegrationsServiceURL, cfg.InternalSecret)
})
apiGroup.All("/integrations", func(c *fiber.Ctx) error {
    return doProxy(c, cfg.IntegrationsServiceURL, cfg.InternalSecret)
})
```

3. Add PUBLIC routes (no JWT) BEFORE the apiGroup definition (same section as invite proxy):

```go
// Bot API — token-authenticated, no JWT
app.All("/api/v1/bot/:token/*", func(c *fiber.Ctx) error {
    return doProxy(c, cfg.BotsServiceURL)  // no internal secret — bots service handles auth via token
})

// Integration webhooks — HMAC-authenticated, no JWT
app.Post("/api/v1/webhooks/in/:connectorId", func(c *fiber.Ctx) error {
    return doProxy(c, cfg.IntegrationsServiceURL)  // no internal secret — integrations service verifies HMAC
})
```

**Modify** `services/gateway/cmd/main.go`:

Add env loading:
```go
botsURL := config.EnvOr("BOTS_SERVICE_URL", config.EnvOr("BOTS_URL", "http://localhost:8086"))
integrationsURL := config.EnvOr("INTEGRATIONS_SERVICE_URL", config.EnvOr("INTEGRATIONS_URL", "http://localhost:8087"))
```

Pass to ProxyConfig:
```go
BotsServiceURL:         botsURL,
IntegrationsServiceURL: integrationsURL,
```

Add to docker-compose.yml gateway environment:
```yaml
BOTS_SERVICE_URL: http://bots:8086
INTEGRATIONS_SERVICE_URL: http://integrations:8087
```

**Verify**: `cd services/gateway && go build ./... && go vet ./...`

---

## TASK-52: Gateway WS event types for bots

**Modify** `services/gateway/internal/ws/events.go`

Add bot-related event type constants:
```go
// Bot events
EventBotInstalled   = "bot_installed"
EventBotUninstalled = "bot_uninstalled"
EventCallbackQuery  = "callback_query"
```

These events will be published by the bots service via NATS and delivered to clients via WebSocket.

**Modify** `services/gateway/internal/ws/nats_subscriber.go`:

Add subscription in `Start()`:
```go
s.nc.Subscribe("orbit.chat.*.bot.*", s.handleEvent)
```

**Verify**: `cd services/gateway && go build ./... && go vet ./...`

---

## TASK-53: CHECKPOINT

**Run**:
```bash
cd D:/job/orbit/services/gateway && go build ./... && go vet ./...
```

**Commit**: `feat(phase8): TASK-51..52 — gateway proxy routes and WS events for bots/integrations`

---

## TASK-54: Integrations handler tests

**Create** `services/integrations/internal/handler/mock_stores_test.go`

Fn-field mocks for ConnectorStore, RouteStore, DeliveryStore.

**Create** `services/integrations/internal/handler/connector_handler_test.go`

Tests:
1. `TestCreateConnector_Success`
2. `TestCreateConnector_Unauthorized` — no SysManageIntegrations → 403
3. `TestCreateConnector_ValidationError` — invalid name → 400
4. `TestReceiveWebhook_Success` — valid payload, correct HMAC
5. `TestReceiveWebhook_InvalidSignature` — wrong HMAC → 401
6. `TestReceiveWebhook_ConnectorNotFound` → 404
7. `TestListDeliveries_Success`

**Verify**: `cd services/integrations && go test ./internal/handler/... -v`

---

## TASK-55: CHECKPOINT

**Run**:
```bash
cd D:/job/orbit/services/integrations && go test ./... -v
```

**Commit**: `feat(phase8): TASK-54 — integrations handler tests`

---

## TASK-56: Frontend Saturn types for bots and integrations

**Modify** `web/src/api/saturn/types.ts`

Add at the end of the file:

```typescript
// === Bots ===

export interface SaturnBot {
  id: string;
  user_id: string;
  owner_id: string;
  username: string;
  display_name: string;
  avatar_url?: string;
  description?: string;
  short_description?: string;
  is_system: boolean;
  is_inline: boolean;
  webhook_url?: string;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface SaturnBotCommand {
  id: string;
  bot_id: string;
  command: string;
  description: string;
}

export interface SaturnBotInstallation {
  bot_id: string;
  chat_id: string;
  installed_by: string;
  scopes: number;
  is_active: boolean;
  created_at: string;
}

export interface SaturnBotCreateResponse {
  bot: SaturnBot;
  token: string;
}

// === Integrations ===

export interface SaturnIntegrationConnector {
  id: string;
  name: string;
  display_name: string;
  type: 'inbound_webhook' | 'outbound_webhook' | 'polling';
  bot_id?: string;
  config: Record<string, unknown>;
  is_active: boolean;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface SaturnIntegrationRoute {
  id: string;
  connector_id: string;
  chat_id: string;
  event_filter?: string;
  template?: string;
  is_active: boolean;
}

export interface SaturnIntegrationDelivery {
  id: string;
  connector_id: string;
  event_type: string;
  status: 'pending' | 'delivered' | 'failed' | 'dead_letter';
  correlation_key?: string;
  attempt_count: number;
  last_error?: string;
  delivered_at?: string;
  created_at: string;
}

export interface SaturnConnectorCreateResponse {
  connector: SaturnIntegrationConnector;
  secret: string;
}
```

**Verify**: `cd web && npx tsc --noEmit --pretty 2>&1 | head -20` (check for type errors)

---

## TASK-57: Frontend Saturn methods for bots

**Create** `web/src/api/saturn/methods/bots.ts`

```typescript
import { request } from '../client';
import type {
  SaturnBot, SaturnBotCommand, SaturnBotInstallation, SaturnBotCreateResponse,
} from '../types';

export async function fetchBots(limit = 50, offset = 0) {
  return request<{ data: SaturnBot[]; total: number }>('GET', `/bots?limit=${limit}&offset=${offset}`);
}

export async function fetchBot(botId: string) {
  return request<SaturnBot>('GET', `/bots/${botId}`);
}

export async function createBot(data: { username: string; display_name: string; description?: string }) {
  return request<SaturnBotCreateResponse>('POST', '/bots', data);
}

export async function updateBot(botId: string, data: Partial<{ display_name: string; description: string; short_description: string }>) {
  return request<SaturnBot>('PATCH', `/bots/${botId}`, data);
}

export async function deleteBot(botId: string) {
  return request<void>('DELETE', `/bots/${botId}`);
}

export async function rotateToken(botId: string) {
  return request<{ token: string }>('POST', `/bots/${botId}/token/rotate`);
}

export async function setBotCommands(botId: string, commands: Array<{ command: string; description: string }>) {
  return request<SaturnBotCommand[]>('PUT', `/bots/${botId}/commands`, { commands });
}

export async function fetchBotCommands(botId: string) {
  return request<SaturnBotCommand[]>('GET', `/bots/${botId}/commands`);
}

export async function installBot(botId: string, chatId: string, scopes: number) {
  return request<SaturnBotInstallation>('POST', `/bots/${botId}/install`, { chat_id: chatId, scopes });
}

export async function uninstallBot(botId: string, chatId: string) {
  return request<void>('DELETE', `/bots/${botId}/install`, { chat_id: chatId });
}

export async function fetchChatBots(chatId: string) {
  return request<SaturnBotInstallation[]>('GET', `/chats/${chatId}/bots`);
}
```

**Verify**: `cd web && npx tsc --noEmit --pretty 2>&1 | head -20`

---

## TASK-58: Frontend Saturn methods for integrations

**Create** `web/src/api/saturn/methods/integrations.ts`

```typescript
import { request } from '../client';
import type {
  SaturnIntegrationConnector, SaturnIntegrationRoute,
  SaturnIntegrationDelivery, SaturnConnectorCreateResponse,
} from '../types';

export async function fetchConnectors(limit = 50, offset = 0) {
  return request<{ data: SaturnIntegrationConnector[]; total: number }>('GET', `/integrations/connectors?limit=${limit}&offset=${offset}`);
}

export async function fetchConnector(connectorId: string) {
  return request<SaturnIntegrationConnector>('GET', `/integrations/connectors/${connectorId}`);
}

export async function createConnector(data: { name: string; display_name: string; type: string; bot_id?: string }) {
  return request<SaturnConnectorCreateResponse>('POST', '/integrations/connectors', data);
}

export async function updateConnector(connectorId: string, data: Partial<{ display_name: string; is_active: boolean; config: Record<string, unknown> }>) {
  return request<SaturnIntegrationConnector>('PATCH', `/integrations/connectors/${connectorId}`, data);
}

export async function deleteConnector(connectorId: string) {
  return request<void>('DELETE', `/integrations/connectors/${connectorId}`);
}

export async function rotateConnectorSecret(connectorId: string) {
  return request<{ secret: string }>('POST', `/integrations/connectors/${connectorId}/rotate-secret`);
}

export async function fetchRoutes(connectorId: string) {
  return request<SaturnIntegrationRoute[]>('GET', `/integrations/connectors/${connectorId}/routes`);
}

export async function createRoute(connectorId: string, data: { chat_id: string; event_filter?: string; template?: string }) {
  return request<SaturnIntegrationRoute>('POST', `/integrations/connectors/${connectorId}/routes`, data);
}

export async function deleteRoute(routeId: string) {
  return request<void>('DELETE', `/integrations/routes/${routeId}`);
}

export async function fetchDeliveries(connectorId: string, limit = 50, offset = 0, status?: string) {
  let url = `/integrations/connectors/${connectorId}/deliveries?limit=${limit}&offset=${offset}`;
  if (status) url += `&status=${status}`;
  return request<{ data: SaturnIntegrationDelivery[]; total: number }>('GET', url);
}

export async function retryDelivery(deliveryId: string) {
  return request<void>('POST', `/integrations/deliveries/${deliveryId}/retry`);
}
```

**Verify**: `cd web && npx tsc --noEmit --pretty 2>&1 | head -20`

---

## TASK-59: Wire new methods into Saturn index

**Modify** `web/src/api/saturn/methods/index.ts`

1. Add imports at the top:
```typescript
import * as botsApi from './bots';
import * as integrationsApi from './integrations';
```

2. Replace the stub implementations:
- Replace `fetchPopularAppBots` stub with: `return botsApi.fetchBots().then(r => r?.data)`
- Replace `loadAttachBots` stub with: `return botsApi.fetchBots().then(r => r?.data)`

3. Export the new modules so they're accessible:
```typescript
export { botsApi, integrationsApi };
```

Note: The full TG Web A bot action wiring (bots.ts actions) is NOT in scope for this plan. That requires deeper integration with the global state and UI components. The Saturn API methods are the foundation; the UI wiring will be done in a future phase.

**Verify**: `cd web && npx tsc --noEmit --pretty 2>&1 | head -20`

---

## TASK-60: FINAL CHECKPOINT

**Run all builds**:
```bash
cd D:/job/orbit/pkg && go build ./... && go vet ./...
cd D:/job/orbit/services/bots && go build ./... && go vet ./... && go test ./... 2>&1 | tail -5
cd D:/job/orbit/services/integrations && go build ./... && go vet ./... && go test ./... 2>&1 | tail -5
cd D:/job/orbit/services/gateway && go build ./... && go vet ./...
```

Verify file counts:
```bash
find services/bots/internal -name "*.go" | wc -l        # should be ~15-20 files
find services/integrations/internal -name "*.go" | wc -l  # should be ~12-15 files
ls migrations/04[1-4]*.sql | wc -l                       # should be 4 files
```

**Commit**: `feat(phase8): TASK-56..59 — frontend Saturn types and API methods for bots/integrations`

Then final summary commit or tag:
```bash
git tag phase-8-foundation
```

**Update PROGRESS.md** with final summary.

---

## Summary of Created/Modified Files

### New migrations (4 files)
- `migrations/041_bot_accounts.sql`
- `migrations/042_bots.sql`
- `migrations/043_integrations.sql`
- `migrations/044_message_bot_extensions.sql`

### Modified shared packages (1 file)
- `pkg/permissions/system.go`

### New bots service files (~18 files)
- `services/bots/internal/model/models.go`
- `services/bots/internal/store/bot_store.go`
- `services/bots/internal/store/token_store.go`
- `services/bots/internal/store/command_store.go`
- `services/bots/internal/store/installation_store.go`
- `services/bots/internal/service/bot_service.go`
- `services/bots/internal/service/nats_subscriber.go`
- `services/bots/internal/service/webhook_worker.go`
- `services/bots/internal/service/update_queue.go`
- `services/bots/internal/handler/bot_handler.go`
- `services/bots/internal/handler/token_handler.go`
- `services/bots/internal/handler/command_handler.go`
- `services/bots/internal/handler/installation_handler.go`
- `services/bots/internal/handler/mock_stores_test.go`
- `services/bots/internal/handler/bot_handler_test.go`
- `services/bots/internal/client/messaging_client.go`
- `services/bots/internal/botapi/middleware.go`
- `services/bots/internal/botapi/models.go`
- `services/bots/internal/botapi/handler.go`
- `services/bots/internal/botapi/callback_handler.go`
- `services/bots/internal/botapi/webhook_handler.go`
- `services/bots/internal/botapi/updates_handler.go`

### Modified bots service files (2 files)
- `services/bots/go.mod`
- `services/bots/cmd/main.go`

### New integrations service files (~12 files)
- `services/integrations/internal/model/models.go`
- `services/integrations/internal/store/connector_store.go`
- `services/integrations/internal/store/route_store.go`
- `services/integrations/internal/store/delivery_store.go`
- `services/integrations/internal/service/integration_service.go`
- `services/integrations/internal/service/delivery_worker.go`
- `services/integrations/internal/handler/connector_handler.go`
- `services/integrations/internal/handler/webhook_handler.go`
- `services/integrations/internal/handler/delivery_handler.go`
- `services/integrations/internal/handler/mock_stores_test.go`
- `services/integrations/internal/handler/connector_handler_test.go`
- `services/integrations/internal/client/messaging_client.go`

### Modified integrations service files (2 files)
- `services/integrations/go.mod`
- `services/integrations/cmd/main.go`

### Modified gateway files (4 files)
- `services/gateway/internal/handler/proxy.go`
- `services/gateway/cmd/main.go`
- `services/gateway/internal/ws/events.go`
- `services/gateway/internal/ws/nats_subscriber.go`

### Modified infrastructure files (2 files)
- `docker-compose.yml`
- `.env.example`

### New frontend files (2 files)
- `web/src/api/saturn/methods/bots.ts`
- `web/src/api/saturn/methods/integrations.ts`

### Modified frontend files (2 files)
- `web/src/api/saturn/types.ts`
- `web/src/api/saturn/methods/index.ts`
