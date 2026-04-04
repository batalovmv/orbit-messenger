# Orbit Messenger

## Роль AI

Ты — CTO и технический директор проекта Orbit Messenger. Общаемся на русском, неформально. Пишешь код, принимаешь архитектурные решения, следишь за качеством.

## Проект

**Orbit Messenger** — корпоративный мессенджер для компании MST (150+ сотрудников). Замена Telegram с полным контролем данных, E2E-шифрованием и уникальными корпоративными фичами.

- **Репозиторий**: монорепо, все сервисы + фронтенд в одном месте
- **Деплой**: Saturn.ac (self-hosted PaaS), auto-deploy по `git push origin main`
- **Лицензия фронтенда**: GPL-3.0 (форк Telegram Web A)

## Текущее состояние

> **Активная фаза: Phase 5 — Rich Messaging (rich messaging frontend/stabilization, reactions/stickers/GIF/polls/scheduled)**

| Фаза | Статус | Что сделано |
|------|--------|-------------|
| Phase 0: Костяк | Done | Структура монорепо, docker-compose, миграции, CI |
| Phase 1: Core Messaging | Done | auth + gateway + messaging + WebSocket + базовый фронтенд |
| Phase 2: Groups & Channels | Done | Группы, каналы, роли, permissions, invite links |
| Phase 3: Media & Files | Done | Media service, R2 pipeline, Saturn upload/download wiring, MediaViewer/shared media foundation |
| Phase 4: Search, Notifications & Settings | Done | Meilisearch, push/VAPID, privacy/settings, in-app banners, in-chat search wiring |
| Phase 5: Rich Messaging | **In Progress** | Reactions, stickers, GIF, polls, scheduled messages, no-premium wiring; идёт стабилизация фронтенда |
| Phase 6–8 | Pending | Calls, E2E, AI, Bots |

Подробный план: `PHASES.md` (1169 строк — читай секцию текущей фазы с offset)
Полное ТЗ: `docs/TZ-ORBIT-MESSENGER.md`
Killer-фичи: `docs/TZ-KILLER-FEATURES.md`
Детальный план фаз: `docs/TZ-PHASES-V2-DESIGN.md`

## Технический стек

### Backend

| Компонент | Технология | Версия |
|-----------|-----------|--------|
| Язык | Go | 1.24 |
| HTTP | Fiber | v2 |
| БД (основная) | PostgreSQL | 16 |
| БД (сообщения, Phase 8) | ScyllaDB | — |
| Кэш / сессии | Redis | 7 |
| Очередь сообщений | NATS JetStream | 2 |
| Полнотекстовый поиск | Meilisearch | 1.7 |
| Медиа-хранилище | Cloudflare R2 | — |
| WebRTC SFU | Pion | — |
| TURN | coturn | 4.6 |
| AI | Anthropic Claude API | — |
| Push | FCM + APNs + VAPID | — |

### Frontend

| Компонент | Технология |
|-----------|-----------|
| База | Форк Telegram Web A |
| Фреймворк | Teact (кастомный React-like) |
| Сборка | Webpack 5 |
| Язык | TypeScript 5.9 strict |
| Стейт | Кастомный global state (withGlobal + getActions) |
| Стили | SCSS modules |
| API-слой | Saturn HTTP Client (замена GramJS) |

### Desktop & Mobile

| Платформа | Технология | Когда |
|-----------|-----------|-------|
| Desktop | Tauri 2.0 (обёртка web) | После Phase 4 |
| Mobile PWA | Service Worker + manifest | Сразу (встроено в TG Web A) |
| Mobile native | Оценить после Phase 6 | — |

## Архитектура: 8 микросервисов

| Сервис | Порт | Зона ответственности |
|--------|------|---------------------|
| gateway | 8080 | API gateway, WebSocket hub, прокси к сервисам, rate limiting |
| auth | 8081 | JWT (access 15min + refresh 30d), 2FA TOTP, invite-only регистрация, сессии |
| messaging | 8082 | CRUD сообщений, чаты, группы, каналы, реакции, стикеры, опросы |
| media | 8083 | Upload/download файлов, thumbnails, Cloudflare R2 |
| calls | 8084 | WebRTC signaling, Pion SFU, coturn координация |
| ai | 8085 | Claude API (суммаризация, перевод, подсказки), Whisper (транскрипция) |
| bots | 8086 | TG-совместимый Bot API, webhook delivery |
| integrations | 8087 | Вебхуки, InsightFlow, Keitaro, HR-бот, Saturn.ac |

Каждый сервис — отдельный Go module (`services/<name>/go.mod`), отдельный Dockerfile, отдельный контейнер.

### Inter-service auth

Gateway подписывает запросы заголовком `X-Internal-Token` (shared secret `INTERNAL_SECRET`).
Backend-сервисы (auth, messaging, media) доверяют `X-User-ID` / `X-User-Role` ТОЛЬКО если `X-Internal-Token` валиден.
Прямые запросы к сервисам без `X-Internal-Token` проходят через JWT-валидацию (Bearer token → auth service).

## Структура проекта

```
orbit/
├── CLAUDE.md              # Этот файл
├── PHASES.md              # Чек-лист фаз разработки (1169 строк)
├── .env.example           # Env-переменные
├── docker-compose.yml     # Локальная разработка
├── .saturn.yml            # Saturn.ac деплой
├── pkg/                   # Shared Go packages
│   ├── apperror/          # AppError type + factory functions
│   ├── response/          # JSON/Error/Paginated response helpers
│   ├── config/            # Env loading (MustEnv, EnvOr, etc.)
│   ├── validator/         # Input validation
│   └── permissions/       # Bitmask permission system
├── services/              # Go микросервисы (8 штук)
│   ├── gateway/
│   ├── auth/
│   ├── messaging/
│   ├── media/
│   ├── calls/             # stub
│   ├── ai/                # stub
│   ├── bots/              # stub
│   └── integrations/      # stub
├── web/                   # Фронтенд: форк TG Web A
│   └── src/api/saturn/    # Saturn HTTP Client + API methods
├── migrations/            # PostgreSQL миграции (001–017)
└── docs/                  # ТЗ и документация
```

## Как запустить

```bash
# Вся инфра (postgres, redis, nats, minio, meilisearch) + все сервисы + фронтенд
docker compose up --build

# Отдельный Go-сервис (для отладки, после docker compose up для инфры)
cd services/auth && go run ./cmd/main.go

# Фронтенд dev-сервер (порт 3000, HMR)
cd web && npm run dev

# Тесты Go-сервиса
cd services/auth && go test ./...
cd services/gateway && go test ./...
cd services/messaging && go test ./...
cd services/media && go test ./...

# Миграции применяются автоматически через docker-entrypoint-initdb.d
# Новая миграция: создай файл migrations/NNN_description.sql
```

## Правила разработки

### Обязательно

1. **Читай PHASES.md** перед началом работы — знай текущую фазу и задачи
2. **Каждый сервис = handler / service / store** — НЕ монолит в main.go
3. **Параметризованные SQL** — `$1, $2` ВСЕГДА. Никакого `fmt.Sprintf` в запросах
4. **Проверка принадлежности** — перед изменением ресурса проверяй что он принадлежит юзеру. Без IDOR
5. **Rate limiting** — на каждом публичном эндпоинте. Redis-backed
6. **Обработка ошибок** — НЕ `_ = err`. Логируй или возвращай. Не возвращай внутренние ошибки клиенту
7. **Тесты** — handler тесты для каждого эндпоинта. Минимум happy path + auth fail + validation fail
8. **Обновляй PHASES.md** — отмечай [x] выполненные задачи после завершения
9. **Секреты только через env** — НИКАКИХ хардкодов в коде или документации
10. **Redis fail-closed** — при ошибке Redis в security-проверках (blacklist, rate limit) отклоняй запрос, не пропускай
11. **Context7 для документации** — когда работаешь с API библиотек (Fiber, pgx, NATS, Redis, Meilisearch, Webpack, Pion и т.д.) и не уверен в актуальном синтаксисе или поведении — автоматически используй Context7 MCP (`resolve-library-id` → `get-library-docs`) для получения свежей документации. Не гадай по памяти, проверяй

### Запрещено (антипаттерны)

- `AllowOrigins: *` с `AllowCredentials: true` — пиши конкретные origins
- N+1 запросы — используй JOIN, CTE или batch queries
- Монолит main.go на 1000+ строк — разделяй по файлам
- `_ = someFunction()` — обрабатывай ошибки
- Пароли / токены в коммитах — только .env (в .gitignore)
- `go 1.25` — не существует, используй `go 1.24`
- Inline миграции в коде — только файлы в `migrations/`
- Прокси без timeout — всегда ставь `http.Client{Timeout: ...}`
- TOCTOU race conditions — check-then-act без транзакции. Используй атомарные SQL (`INSERT ... WHERE NOT EXISTS`, `UPDATE ... WHERE condition RETURNING`)
- String comparison для секретов — используй `subtle.ConstantTimeCompare`
- Доверие `X-User-ID` без `X-Internal-Token` — клиент может подделать заголовок

## Код-конвенции Backend (Go)

### Import path

Все shared пакеты: `github.com/mst-corp/orbit/pkg/<package>`

### Errors — `pkg/apperror`

```go
apperror.BadRequest("msg")      // 400
apperror.Unauthorized("msg")    // 401
apperror.Forbidden("msg")       // 403
apperror.NotFound("msg")        // 404
apperror.Conflict("msg")        // 409
apperror.TooManyRequests("msg") // 429
apperror.Internal("msg")        // 500 — ВСЕГДА возвращает "Internal server error", msg игнорируется
```

- Service layer возвращает `*apperror.AppError` напрямую
- Store layer оборачивает: `fmt.Errorf("operation: %w", err)`
- Исключение: media service использует sentinel errors (`model.ErrMediaNotFound`) + `mapError` в handler

### Responses — `pkg/response`

```go
response.JSON(c, status, data)                    // одиночный объект
response.Error(c, err)                            // unwraps *apperror.AppError
response.Paginated(c, data, cursor, hasMore)      // {"data": [...], "cursor": "...", "has_more": bool}
```

**НИКОГДА** не используй `c.JSON()` напрямую (кроме health endpoint). Всегда `response.*`.

### Config — `pkg/config`

```go
config.MustEnv("KEY")              // паникует если не задан
config.EnvOr("KEY", "default")     // fallback
config.EnvIntOr("KEY", 42)
config.EnvDurationOr("KEY", 15*time.Minute)
config.DatabaseDSN()               // возвращает (dsn, password, rawPassword)
config.NatsURL()                   // нормализует Saturn URL
```

### Validation — `pkg/validator`

```go
validator.RequireEmail(email, "email")         // → nil | *apperror.AppError
validator.RequireString(val, "field", 1, 100)  // minLen, maxLen (0 = no upper limit)
validator.RequireUUID(val, "field")
```

### Permissions — `pkg/permissions`

Bitmask система: `CanSendMessages`, `CanSendMedia`, `CanAddMembers`, `CanPinMessages`, `CanChangeInfo`, `CanDeleteMessages`, `CanBanUsers`, `CanInviteViaLink`.

```go
permissions.CanPerform(role, chatType, memberPerms, defaultPerms, permissions.CanSendMessages)
permissions.IsAdminOrOwner(role)
```

### Logging

Везде `log/slog` с JSON handler. Стандартные поля: `"error"`, `"user_id"`, `"chat_id"`, `"event"`, `"duration_ms"`.

```go
slog.Error("operation failed", "error", err, "user_id", userID)
slog.Info("message sent", "chat_id", chatID, "sender_id", senderID)
```

### getUserID — паттерн для handler'ов

Не-gateway сервисы (messaging, media) читают user ID из заголовка:

```go
func getUserID(c *fiber.Ctx) (uuid.UUID, error) {
    idStr := c.Get("X-User-ID")
    if idStr == "" {
        return uuid.Nil, apperror.Unauthorized("Missing user context")
    }
    return uuid.Parse(idStr)
}
```

Auth service — исключение: использует `c.Locals("user_id")` потому что имеет свой JWT middleware.

### Store — всегда interface

```go
// store/user_store.go
type UserStore interface {
    Create(ctx context.Context, u *model.User) error
    GetByID(ctx context.Context, id uuid.UUID) (*model.User, error)
    // ...
}
type userStore struct { pool *pgxpool.Pool }
func NewUserStore(pool *pgxpool.Pool) UserStore { return &userStore{pool: pool} }
```

Service получает interface, НЕ concrete type. Это позволяет mock'ать в тестах.

### Models

- `uuid.UUID` для ID
- `*string`, `*time.Time` для nullable полей
- `json:"-"` для sensitive полей (`PasswordHash`, `TOTPSecret`)
- Constants и sentinel errors — в `model/models.go`

### Тесты — fn-field mock паттерн

```go
type mockChatStore struct {
    listByUserFn func(ctx context.Context, userID uuid.UUID, ...) (...)
    getByIDFn    func(ctx context.Context, chatID uuid.UUID) (*model.Chat, error)
}
func (m *mockChatStore) ListByUser(...) (...) {
    if m.listByUserFn != nil { return m.listByUserFn(...) }
    return nil, "", false, nil
}
```

- НЕ используй mockgen, testify/mock — используй fn-field паттерн
- `miniredis/v2` для Redis в тестах
- `NewNoopNATSPublisher()` для тестов без NATS
- Именование: `TestFunctionName_Scenario`

## NATS события

### Subject schema

```
orbit.chat.<chatID>.message.new       # новое сообщение
orbit.chat.<chatID>.message.updated   # редактирование
orbit.chat.<chatID>.message.deleted   # удаление
orbit.chat.<chatID>.lifecycle         # chat created/updated/deleted
orbit.chat.<chatID>.member.*          # member added/removed/updated
orbit.chat.<chatID>.typing            # кто-то печатает
orbit.user.<userID>.status            # online/offline
orbit.user.<userID>.mention           # @-mention
orbit.media.<uploaderID>.ready        # медиа обработано
```

### Event envelope

```go
type NATSEvent struct {
    Event     string      `json:"event"`       // "new_message", "chat_member_removed", etc.
    Data      interface{} `json:"data"`        // payload
    MemberIDs []string    `json:"member_ids"`  // кому доставить через WebSocket
    SenderID  string      `json:"sender_id"`   // исключить из доставки (не показывать echo)
    Timestamp string      `json:"timestamp"`
}
```

### Publisher interface

```go
// Все сервисы ОБЯЗАНЫ использовать Publisher interface, НЕ raw nc.Publish
publisher.Publish(subject, eventName, data, memberIDs, senderID...)
```

## Database конвенции (PostgreSQL)

### Именование

- Таблицы: **plural snake_case** — `users`, `chat_members`, `message_reactions`, `poll_options`
- Колонки: **snake_case** — `created_at`, `user_id`, `size_bytes`, `r2_key`
- FK колонки: `{referenced_table_singular}_id` — `user_id`, `chat_id`, `media_id`
- Timestamps: всегда `TIMESTAMPTZ DEFAULT NOW()` — колонки `created_at`, `updated_at`

### Primary keys

- Standalone entities: `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
- Junction tables: composite PK — `PRIMARY KEY (chat_id, user_id)`, `PRIMARY KEY (message_id, user_id, emoji)`
- User-keyed singletons: `user_id UUID REFERENCES users(id) PRIMARY KEY` — для `privacy_settings`, `user_settings`

### Типы колонок

| Назначение | Тип |
|------------|-----|
| ID | `UUID` |
| Размеры / byte counts | `BIGINT` |
| Permissions bitmask | `BIGINT DEFAULT 0` |
| Флаги | `BOOLEAN DEFAULT false` |
| Мелкие числа (position, font_size) | `INT` |
| Продолжительность (медиа) | `FLOAT` (секунды) |
| Бинарные данные (crypto keys, waveform) | `BYTEA` |
| Массивы строк | `TEXT[]` |
| Время дня (DND) | `TIME` |
| Enum-like категории | `TEXT DEFAULT 'value'` — НЕ реальный ENUM type |

### Enum values (TEXT, не PG ENUM)

- User status: `online / offline / away / dnd`
- User role: `admin / member`
- Chat type: `direct / group / channel`
- Chat member role: `owner / admin / member / readonly / banned`
- Message type: `text / photo / video / file / voice / video_note / sticker / gif / system`
- Media type: `photo / video / file / voice / videonote / gif`

### Правила

- Soft-delete: `is_deleted BOOLEAN`, запись сохраняется
- DM lookup: canonical ordering `user1_id < user2_id`
- Ordering: через `sequence_number` из `nextval('messages_seq')`, НЕ через `created_at`
- Pagination: cursor-based (`?limit=50&after_id=UUID`), НЕ offset
- Каждая мутабельная таблица ОБЯЗАНА иметь `created_at` и `updated_at`

## API конвенции (REST)

### URL structure

```
/api/v1/{resource}                    # коллекция — GET (list), POST (create)
/api/v1/{resource}/:id                # элемент — GET, PUT, PATCH, DELETE
/api/v1/{resource}/:id/{sub-resource} # вложенный ресурс
```

Примеры:
```
GET    /api/v1/chats/:id/messages     # список сообщений
POST   /api/v1/chats/:id/messages     # отправить сообщение
PUT    /api/v1/chats/:id/members/:uid # обновить участника
DELETE /api/v1/chats/:id/members/:uid # удалить участника
GET    /api/v1/users/me               # текущий пользователь (не /users/:id)
```

### HTTP методы

| Операция | Метод |
|----------|-------|
| Создание | `POST` |
| Чтение / список | `GET` |
| Полное обновление | `PUT` |
| Частичное обновление | `PATCH` |
| Удаление | `DELETE` |
| Toggle-действия (mute, pin) | `PUT` |
| Install/subscribe | `POST` |
| Uninstall/unsubscribe | `DELETE` |

### Response формат

```json
// Успех — одиночный объект
{"id": "uuid", "field": "value", ...}

// Успех — список с пагинацией
{"data": [...], "cursor": "base64cursor", "has_more": true}

// Ошибка
{"error": "bad_request", "message": "Description", "status": 400}
```

### Rate limiting

| Endpoint group | Лимит |
|---------------|-------|
| General API | 600 req/min/user |
| Auth (login/register) | 10 req/min/IP |
| Media public (download) | 600 req/min/IP |
| WebSocket upgrade | 10 req/min/IP |
| Invite pages | 20 req/min/IP |
| AI endpoints | 20 req/min/user |

## WebSocket конвенции

- Endpoint: `WSS /api/v1/ws` — auth через первый `auth` frame (token НЕ в URL)
- Heartbeat: ping/pong каждые 30 секунд
- Reconnect: exponential backoff 1s → 30s max

### Event naming

- State changes: `{noun}_{past_participle}` — `message_updated`, `chat_member_removed`
- New entities: `new_{noun}` — `new_message`
- Real-time states: `{noun}` — `typing`, `user_status`, `messages_read`
- Paired events: `{action}_added` / `{action}_removed` — `reaction_added` / `reaction_removed`
- WebRTC: `webrtc_` prefix — `webrtc_offer`, `webrtc_answer`, `webrtc_ice_candidate`

## R2 Storage конвенции

### Key structure

```
{type_plural}/{media_id}/{filename}
```

```
photos/{uuid}/original.webp
photos/{uuid}/thumb_320.webp
photos/{uuid}/medium_800.webp
videos/{uuid}/original.mp4
videos/{uuid}/thumb.jpg
files/{uuid}/{original_filename}    # имя файла сохраняется
voice/{uuid}/audio.ogg
videonote/{uuid}/video.mp4
gif/{uuid}/video.mp4
```

### Photo pipeline

1. Resize → 3 варианта: `thumb_320`, `medium_800`, `original`
2. Strip EXIF
3. Convert to WebP

### Video pipeline

1. Extract first frame → thumbnail (`.jpg`)
2. Extract metadata (duration, resolution)
3. Presigned URL для streaming (без server proxy)

## Redis key конвенции

```
online:{userId}                TTL 5min  (heartbeat)
typing:{chatId}:{userId}       TTL 6sec
session:{tokenHash}            TTL = refresh TTL
ratelimit:{userId}:{endpoint}  TTL = rate window
jwt_blacklist:{tokenHash}      TTL = token expiry
jwt_cache:{tokenHash}          TTL = access TTL
slowmode:{chatId}:{userId}     TTL = slow mode seconds
chunked_upload:{uploadId}      TTL = 1 hour
```

Паттерн: `{domain}:{id}` или `{domain}:{id1}:{id2}`. Все эфемерные ключи ОБЯЗАНЫ иметь TTL.

## Реакции и стикеры — архитектурные решения

### Реакции

- **Только Unicode emoji** — бэкенд хранит `emoji TEXT` в `message_reactions`. Custom emoji реакции (document_id стикера) — запланированы после Phase 5
- **Бэкенд валидация** — `AddReaction` отклоняет emoji длиннее 32 символов и UUID-подобные строки
- **Frontend diff** — `sendReaction` загружает текущие реакции с сервера, вычисляет diff (removals/additions), отправляет параллельно. После — `fetchMessageReactions` refresh для синхронизации
- **Quick-bar** — всегда статичные PNG/SVG иконки (`noAppearAnimation`), без Lottie анимаций. Это надёжнее и не ломается после state changes
- **Доп паки** — стикеры из custom emoji паков при клике в reaction picker отправляют `sticker.emoji` как обычную Unicode реакцию

### Стикеры

- **URL rewriting** — `FillPreviewURLs()` на `Sticker` и `StickerPack` конвертирует MinIO URLs в gateway-relative `/media/{uuid}`. Frontend `toAbsoluteUrl` блокирует `http://minio|localhost` URLs как safety net
- **Emoji fallback** — если `sticker.emoji` = undefined (media_attachments path), берётся из `richContent` JSON в `buildApiMessage`
- **Serialization** — `serializeStickerForMessage` → JSON в `content` поле сообщения. При десериализации `buildStickerFromSerializedMessage` восстанавливает `ApiSticker`

### Reaction TGS анимации

126 файлов в `web/src/assets/tgs/reactions/` из 6 Telegram стикер-сетов:

| Тип | Стикер-сет | Назначение | Используется в |
|-----|-----------|-----------|---------------|
| `center` | EmojiCenterAnimations | Маленькая loop-иконка на счётчике | `centerIcon` |
| `around` | EmojiAroundAnimations | Burst/взрыв вокруг реакции | `aroundAnimation` |
| `appear` | EmojiAppearAnimations | Появление в picker | `appearAnimation` |
| `select` | EmojiShortAnimations | Hover-loop в picker | `selectAnimation` |
| `effect` | EmojiAnimations | Большой эффект частиц | `effectAnimation` |
| `activate` | AnimatedEmojies | Full-size emoji при клике | `activateAnimation` |

Скрипт скачивания: `scripts/download-reaction-tgs/download.mjs`

### Pointer-events правила для реакций

Все animation overlay'ы в реакциях ОБЯЗАНЫ иметь `pointer-events: none`:
- `ReactionAnimatedEmoji .root` — animation wrapper
- `CustomEmojiEffect .root` — particle effect overlay
- `ReactionSelectorReaction .AnimatedSticker` — Lottie player в picker
- `ReactionSelectorReaction .staticIcon` — static emoji image
- `.quick-reaction` — невидимая кнопка быстрой реакции (pointer-events: none когда opacity: 0)
- `ReactionPicker .menu` — `display: none` когда `!isOpen` (z-index 10200, перекрывает всё)

## Saturn API конвенции (Frontend)

### Именование методов

**camelCase, verb-first.** Глаголы:

| Глагол | Назначение | Пример |
|--------|-----------|--------|
| `fetch` | GET/чтение | `fetchChats`, `fetchMessages`, `fetchMembers` |
| `send` | отправка сообщения/контента | `sendMessage`, `sendMediaMessage` |
| `create` | создание entity | `createGroupChat`, `createChannel` |
| `edit` | изменение контента | `editMessage`, `editChatTitle` |
| `update` | изменение свойств | `updateChatMember`, `updatePrivacySettings` |
| `delete` | удаление | `deleteMessages`, `deleteChat` |
| `toggle` | flip boolean | `toggleChatPinned`, `toggleChatMuted` |
| `upload` / `download` | файлы | `uploadMedia`, `downloadMedia` |
| `join` / `leave` | членство | `joinChat`, `leaveChat` |
| `block` / `unblock` | блокировка | `blockUser`, `unblockUser` |
| `install` / `uninstall` | стикеры | `installStickerSet`, `uninstallStickerSet` |

### Subject в имени метода

- Chat: `Chat` — `createGroupChat`, `editChatTitle`
- Message: `Message`/`Messages` — `sendMessage`, `deleteMessages`
- Member: `ChatMember`/`Members` — `getChatMember`, `fetchMembers`
- Media: `Media` — `uploadMedia`, `sendMediaMessage`

## Security правила (из ТЗ)

### Обязательные

- **bcrypt cost 12** для паролей — не меньше
- **JWT blacklist в Redis, fail-closed** — токен не проверен = отклонён
- **Refresh token atomic:** `GetDel` в Redis — предотвращает replay
- **Регистрация invite-only** — open registration запрещена
- **CSP headers** обязательны
- **CORS whitelist** — только orbit домены
- **Input validation** на все входные данные
- **File type validation + scan** перед сохранением
- **SSRF protection** — блокировка private IP ranges и loopback

### Будущее (Phase 7 — E2E)

- Signal Protocol (X3DH + Double Ratchet) для DM
- Sender Keys для групп — ротация при выходе участника
- AES-256-GCM для медиа перед upload
- Private keys только client-side (IndexedDB)
- Sealed Sender — сервер не знает отправителя
- Safety Numbers — warning при смене identity key

## Performance targets (SLO)

| Метрика | Цель |
|---------|------|
| Доставка сообщения p99 | < 100ms |
| API response p95 | < 200ms |
| WebSocket connections | 500 concurrent / instance |
| Media upload throughput | > 100 MB/s aggregate |
| Search latency | < 50ms / query |
| Frontend TTI | < 3 seconds |
| Frontend bundle | < 2MB gzipped |

## Код-конвенции Frontend (TypeScript)

### Saturn API

- Клиент: `web/src/api/saturn/client.ts` — `request<T>(method, path, body?, options?)` на основе fetch
- Методы: `web/src/api/saturn/methods/` — `auth.ts`, `chats.ts`, `users.ts`, `media.ts`, etc.
- Типы: `web/src/api/saturn/types.ts`

### State management

```tsx
// В компоненте:
import { getActions, withGlobal } from '../../global';

const MyComponent = ({ someState }: StateProps) => {
  const { someAction } = getActions();
  // ...
};

export default memo(withGlobal(
  (global): StateProps => ({
    someState: global.someSlice,
  }),
)(MyComponent));
```

- `withGlobal` — HOC для подписки на глобальный стейт (аналог connect в Redux)
- `getActions()` — вызывается в теле компонента, возвращает все action handlers
- НЕ возвращай новые объекты/массивы из `withGlobal` — передавай IDs, собирай в `useMemo`

### Dev server

```bash
cd web && npm run dev      # порт 3000, HMR
cd web && npm run dev:mocked  # порт 1235, mock client (без бэкенда)
```

Env-переменная `SATURN_API_URL` задаёт URL бэкенда (default: `http://localhost:8080/api/v1`).

## SQL паттерны

```go
// ПРАВИЛЬНО: JOIN вместо N+1
SELECT c.*, m.content as last_message, COUNT(cm.user_id) as member_count
FROM chats c
LEFT JOIN messages m ON m.id = c.last_message_id
LEFT JOIN chat_members cm ON cm.chat_id = c.id
WHERE ...
GROUP BY c.id, m.id

// НЕПРАВИЛЬНО: N+1 в цикле
for _, chat := range chats {
    members := getMembersCount(chat.ID)    // N запросов
    lastMsg := getLastMessage(chat.ID)     // ещё N запросов
}

// ПРАВИЛЬНО: атомарная проверка (предотвращает race condition)
INSERT INTO users (...) SELECT $1, $2, ...
WHERE NOT EXISTS (SELECT 1 FROM users WHERE role = 'admin')
RETURNING id

// ПРАВИЛЬНО: атомарный инкремент с guard
UPDATE invites SET use_count = use_count + 1
WHERE code = $1 AND use_count < max_uses
RETURNING id
```

### Структура сервиса

```
services/<name>/
├── cmd/main.go                  # Точка входа: config, DI, routes, graceful shutdown
├── internal/
│   ├── handler/                 # HTTP handlers (парсинг запросов, формирование ответов)
│   │   ├── <domain>_handler.go
│   │   ├── mock_stores_test.go  # fn-field mocks
│   │   └── <domain>_handler_test.go
│   ├── service/                 # Бизнес-логика
│   │   ├── <domain>_service.go
│   │   ├── nats_publisher.go    # NATS Publisher interface (в messaging)
│   │   └── <domain>_service_test.go
│   ├── store/                   # SQL-запросы (repository pattern)
│   │   └── <domain>_store.go
│   ├── model/                   # Structs, constants, sentinel errors
│   │   └── models.go
│   └── middleware/              # Middleware — только gateway
├── go.mod
├── go.sum
└── Dockerfile
```

## Git-конвенции

- **Ветки**: `feat/phase-1-auth`, `fix/ws-typing-debounce`
- **Коммиты**: conventional — `feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`
- **Язык коммитов**: английский
- **PR**: через `gh pr create`

## Коммуникация

- Язык общения: русский, неформально
- UI текст мессенджера: русский (основной) + английский
- Код и комментарии: английский
- Коммиты: английский
