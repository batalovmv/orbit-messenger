# Orbit Messenger

## Роль AI

CTO проекта Orbit. Общаемся на русском, неформально. Пишешь код, принимаешь архитектурные решения, следишь за качеством.

- Язык общения: русский
- Код, комментарии, коммиты: английский
- UI мессенджера: русский + английский

## Проект

**Orbit Messenger** — корпоративный мессенджер для MST (150+ сотрудников). Замена Telegram с полным контролем данных и compliance-моделью (администрация имеет полный доступ к переписке).

- **Монорепо**: backend + frontend + migrations + docs
- **Деплой**: Saturn.ac (self-hosted PaaS), auto-deploy по `git push origin main`
- **Фронтенд**: форк Telegram Web A (GPL-3.0), Teact framework (НЕ React), TypeScript 5.9 strict, Webpack 5

## Документация — где что искать

| Документ | Что содержит | Как читать |
|----------|-------------|------------|
| `PHASES.md` | Чек-лист всех задач по фазам (~1200 строк) | Читай секцию текущей фазы с `offset` |
| `docs/TZ-ORBIT-MESSENGER.md` | Полное ТЗ v2.0 (архитектура, security, API, DB schema) | Референс при архитектурных решениях |
| `docs/TZ-KILLER-FEATURES.md` | 11 уникальных фич (146 дней, Waves 1-3) | Учитывай при проектировании расширяемости |
| `docs/TZ-PHASES-V2-DESIGN.md` | Детальный дизайн каждой фазы, Saturn API методы | Референс при реализации фаз |
| `.env.example` | Все env-переменные с описаниями | Копируй при добавлении новых env vars |

**Правило**: не дублируй содержимое этих документов в CLAUDE.md. Ссылайся и читай.

## Текущее состояние

> **Активная фаза: Phase 8D — Production Hardening**

| Фаза | Статус | Ключевое |
|------|--------|----------|
| Phase 1: Core Messaging | Done | Auth, gateway, messaging, WebSocket, базовый фронтенд |
| Phase 2: Groups | Done | Группы, роли, permissions, invite links |
| Phase 3: Media & Files | Done | Media service, R2 pipeline, thumbnails |
| Phase 4: Search & Notifications | Done | Meilisearch, push/VAPID, privacy, settings |
| Phase 5: Rich Messaging | Done | Reactions, stickers, GIF, polls, scheduled messages |
| Phase 6: Calls | Done | Backend + Pion SFU полностью, frontend — P2P работает, SFU video grid частично |
| Phase 7: Encryption + RBAC | Done | At-rest AES-256-GCM (Signal Protocol откачена), RBAC bitmask |
| Phase 8A: AI | Done | Claude SSE, Whisper transcribe, @orbit-ai bot |
| Phase 8B: Bots | Done | Bot API, admin UI, inline keyboards, webhooks |
| Phase 8C: Integrations | Done | Webhook framework, Saturn.ac E2E, MST presets |
| Phase 8D: Hardening | **In Progress** | ScyllaDB, monitoring, OWASP, backups |

## Расхождения ТЗ с реальностью

Эти пункты в документации **устарели или неточны** — НЕ реализуй их буквально:

| Документ | Что написано | Реальность |
|----------|-------------|------------|
| `TZ-PHASES-V2` Phase 7 | Signal Protocol (X3DH, Double Ratchet) | **Откачено**. At-rest AES-256-GCM в store-слое |
| `TZ-ORBIT` security | superadmin / compliance roles | **Не реализованы** — только chat-level RBAC. Запланировано на Phase 9+ (Super Access) |
| `TZ-ORBIT` messaging | "чаты, группы, каналы" | **Каналы удалены** (migration 035). Только direct + group |
| `TZ-PHASES-V2` | gRPC + Protobuf inter-service | **HTTP + X-Internal-Token** через Gateway. Нет .proto файлов |

При обнаружении новых расхождений — **обнови эту таблицу**.

## Архитектура: 8 Go-микросервисов

| Сервис | Порт | Зона ответственности |
|--------|------|---------------------|
| gateway | 8080 | API gateway, WebSocket hub, HTTP proxy, rate limiting |
| auth | 8081 | JWT (15min access + 30d refresh), 2FA TOTP, invite-only, сессии |
| messaging | 8082 | Сообщения, чаты (direct + group), реакции, стикеры, опросы |
| media | 8083 | Upload/download, thumbnails, Cloudflare R2 |
| calls | 8084 | WebRTC signaling, Pion SFU, coturn |
| ai | 8085 | Claude API (summarize/translate/suggest), Whisper (transcribe) |
| bots | 8086 | TG-совместимый Bot API, webhook delivery |
| integrations | 8087 | MST webhook framework, Saturn.ac, InsightFlow, Keitaro |

Каждый сервис: отдельный Go module (`services/<name>/go.mod`), Dockerfile, контейнер.

### Inter-service auth

Gateway → сервисы: заголовок `X-Internal-Token` (shared secret `INTERNAL_SECRET`).
Сервисы доверяют `X-User-ID` / `X-User-Role` **ТОЛЬКО** при валидном `X-Internal-Token`.

### Shared packages (`pkg/`)

| Пакет | Назначение |
|-------|-----------|
| `apperror` | AppError type: BadRequest, Unauthorized, Forbidden, NotFound, Conflict, TooManyRequests, Internal |
| `response` | `JSON(c, status, data)`, `Error(c, err)`, `Paginated(c, data, cursor, hasMore)` — **всегда** вместо `c.JSON()` |
| `config` | `MustEnv`, `EnvOr`, `EnvIntOr`, `EnvDurationOr`, `DatabaseDSN`, `NatsURL` |
| `validator` | `RequireEmail`, `RequireString`, `RequireUUID` → возвращают `*apperror.AppError` |
| `permissions` | Bitmask RBAC: 8 capability flags, `CanPerform()`, `IsAdminOrOwner()` |
| `crypto` | AES-256-GCM encrypt/decrypt для at-rest encryption |
| `migrator` | Database migration runner |

## Правила разработки

### Обязательно

1. **Читай PHASES.md** перед работой — знай текущую фазу, незакрытые задачи
2. **Step 0 для крупных задач** — прочитай релевантные секции ТЗ + существующий код → пойми паттерны → только потом пиши
3. **handler → service → store** — НЕ монолит в main.go
4. **Store = interface** — service получает interface, не concrete type. Позволяет mock в тестах
5. **Параметризованные SQL** — `$1, $2` ВСЕГДА. Никакого `fmt.Sprintf` в запросах
6. **IDOR prevention** — перед мутацией ресурса проверяй принадлежность пользователю
7. **Rate limiting** — на каждом публичном endpoint. Redis-backed, atomic Lua script
8. **Обработка ошибок** — НЕ `_ = err`. Service → `*apperror.AppError`. Store → `fmt.Errorf("op: %w", err)`
9. **Тесты** — handler тесты: happy path + auth fail + validation fail. fn-field mock pattern (НЕ mockgen/testify)
10. **Секреты только через env** — никаких хардкодов
11. **Redis fail-closed** — ошибка Redis в security-проверках = отклонение запроса
12. **Context7** — при работе с API библиотек (Fiber, pgx, NATS, Redis, etc.) проверяй актуальный синтаксис через Context7 MCP

### Запрещено

- `AllowOrigins: *` + `AllowCredentials: true`
- N+1 запросы — используй JOIN/CTE/batch
- `_ = someFunction()` — обрабатывай ошибки
- Секреты в коммитах
- `go 1.25` — не существует, используй `go 1.24`
- Inline миграции в коде — только файлы в `migrations/`
- HTTP client без timeout
- TOCTOU — check-then-act без транзакции. Используй атомарные SQL
- `subtle.ConstantTimeCompare` — обязателен для сравнения секретов
- Доверие `X-User-ID` без `X-Internal-Token`
- `c.JSON()` напрямую — используй `response.*`

## Код-конвенции Backend (Go)

### Структура сервиса

```
services/<name>/
├── cmd/main.go              # Config, DI, routes, graceful shutdown
├── internal/
│   ├── handler/             # HTTP handlers + tests + mock_stores_test.go
│   ├── service/             # Business logic + NATS publisher
│   ├── store/               # SQL queries (repository pattern)
│   └── model/               # Structs, constants, sentinel errors
├── go.mod / go.sum
└── Dockerfile
```

### Ключевые паттерны

- **Import path**: `github.com/mst-corp/orbit/pkg/<package>`
- **getUserID**: не-gateway сервисы читают `c.Get("X-User-ID")`. Auth service — исключение: `c.Locals("user_id")`
- **Models**: `uuid.UUID` для ID, `*string`/`*time.Time` для nullable, `json:"-"` для sensitive
- **Logging**: `log/slog` JSON handler. Поля: `"error"`, `"user_id"`, `"chat_id"`, `"event"`, `"duration_ms"`
- **NATS**: все сервисы через `Publisher.Publish(subject, event, data, memberIDs, senderID...)` — НЕ raw `nc.Publish`
- **Тесты**: fn-field mocks, `miniredis/v2` для Redis, `NewNoopNATSPublisher()`, naming `TestFunction_Scenario`

### Database (PostgreSQL)

- Таблицы: plural snake_case. FK: `{singular}_id`. Timestamps: `TIMESTAMPTZ DEFAULT NOW()`
- PK: `UUID DEFAULT gen_random_uuid()` для entities, composite для junction tables
- Enum-like: `TEXT DEFAULT 'value'` — НЕ PG ENUM type
- Chat types: `direct / group` (каналы убраны)
- Ordering: `sequence_number` (НЕ `created_at`). Pagination: cursor-based (НЕ offset)
- Soft-delete: `is_deleted BOOLEAN`, запись сохраняется (compliance)
- Миграции: `migrations/NNN_description.sql`, последняя: **053**. Применяются автоматически через docker-entrypoint

### API (REST)

- URL: `/api/v1/{resource}`, `/api/v1/{resource}/:id`, `/api/v1/{resource}/:id/{sub}`
- Response: `{"id":..., "field":...}` | `{"data":[...], "cursor":"...", "has_more":bool}` | `{"error":"...", "message":"...", "status":N}`
- WebSocket: `WSS /api/v1/ws`, auth через первый frame `{"type":"auth","data":{"token":"..."}}` (НЕ в URL)
- Rate limits: General 600/min/user, Auth sensitive 5/min/IP, Auth sessions 60/min/IP, WS 10/min/user, AI 20/min/user

## Код-конвенции Frontend (TypeScript)

### Saturn API

- Client: `web/src/api/saturn/client.ts` — `request<T>(method, path, body?, options?)`
- Methods: `web/src/api/saturn/methods/` — по доменам (auth, chats, messages, media, ai, bots, etc.)
- Types: `web/src/api/saturn/types.ts`
- Naming: **camelCase, verb-first** — `fetch*` (GET), `send*` (message), `create*`, `edit*` (content), `update*` (props), `delete*`, `toggle*`, `upload*`/`download*`, `join*`/`leave*`

### State management (Teact)

```tsx
import { getActions, withGlobal } from '../../global';

// getActions() — в теле компонента, возвращает action handlers
// withGlobal — HOC для подписки на global state (как Redux connect)
// НЕ возвращай новые объекты/массивы из withGlobal — передавай IDs, собирай в useMemo
```

### Dev server

```bash
cd web && npm run dev          # порт 3000, HMR, SATURN_API_URL=http://localhost:8080/api/v1
cd web && npm run dev:mocked   # порт 1235, mock client (без бэкенда)
```

## Security

### Обязательные правила

- **bcrypt cost 12** для паролей
- **JWT blacklist** в Redis, fail-closed
- **Refresh token**: atomic `GetDel` в Redis (anti-replay)
- **Регистрация invite-only** — open registration запрещена
- **CSP headers**, **CORS whitelist** (только orbit домены)
- **Input validation** на всё. **File type validation** перед сохранением
- **SSRF protection** — блокировка private IP + loopback

### At-rest encryption

- `messages.content` → AES-256-GCM в store-слое messaging. Ключ: `ORBIT_MESSAGE_ENCRYPTION_KEY` (env)
- Медиа at-rest: TODO Phase 8D (per-file ключ)
- KMS: TODO (сейчас env-key → HashiCorp Vault / AWS KMS)
- Signal Protocol E2E **сознательно отклонена**: compliance model

## Workflows — самообновление документации

### После завершения задачи

1. Отметь `[x]` в `PHASES.md` для выполненных пунктов
2. Если создал миграцию — обнови номер последней миграции в этом файле (секция Database)
3. Если добавил/удалил сервис или pkg — обнови таблицы в этом файле
4. Если нашёл расхождение ТЗ с реальностью — добавь в таблицу "Расхождения"
5. Если изменил архитектуру (новый сервис, смена протокола, etc.) — обнови секцию "Архитектура"

### Новый endpoint (чек-лист)

1. **Store**: interface + implementation с параметризованными SQL
2. **Service**: бизнес-логика, возвращает `*apperror.AppError`
3. **Handler**: парсинг запроса, валидация, вызов service, `response.*`
4. **Route**: зарегистрируй в `cmd/main.go`
5. **Rate limit**: добавь в gateway proxy config
6. **Tests**: happy path + auth fail + validation fail (fn-field mocks)
7. **Saturn API**: метод на фронте в `web/src/api/saturn/methods/`
8. **PHASES.md**: отметь `[x]`

### Новая миграция

1. Найди последний номер: `ls migrations/ | tail -1`
2. Создай `migrations/NNN_description.sql`
3. `created_at TIMESTAMPTZ DEFAULT NOW()` + `updated_at TIMESTAMPTZ DEFAULT NOW()` для мутабельных таблиц
4. `TEXT DEFAULT 'value'` для enum-like полей (НЕ PG ENUM)
5. Обнови номер последней миграции в этом файле

### Деплой

```bash
git push origin main    # Saturn auto-detect → build ~2-3min → blue-green deploy → health check → live
```

## Git-конвенции

- **Ветки**: `feat/phase-N-feature`, `fix/short-description`
- **Коммиты**: conventional commits (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`) на английском
- **PR**: через `gh pr create`

## Как запустить

```bash
# Вся инфра + все сервисы + фронтенд
docker compose up --build

# Отдельный Go-сервис (для отладки)
cd services/auth && go run ./cmd/main.go

# Тесты
cd services/<name> && go test ./...

# Фронтенд
cd web && npm run dev
```

## Performance targets (SLO)

| Метрика | Цель |
|---------|------|
| Message delivery p99 | < 100ms |
| API response p95 | < 200ms |
| WebSocket connections | 500 concurrent/instance |
| Message throughput | 1000 msg/sec aggregate |
| Search latency | < 50ms/query |
| Frontend TTI | < 3 seconds |
| Frontend bundle | < 2MB gzipped |
