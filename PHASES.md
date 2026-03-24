# Orbit Messenger — План разработки

Каждая фаза = рабочий релиз. Код пишется пофазно в отдельных чатах.

Полное ТЗ: `docs/TZ-ORBIT-MESSENGER.md`
Детали фаз: `docs/TZ-PHASES-V2-DESIGN.md`
Killer-фичи: `docs/TZ-KILLER-FEATURES.md`

---

## Порядок работы в каждой фазе

Каждая фаза начинается в **новом чате**. Ассистент ОБЯЗАН пройти шаги в указанном порядке:

### Шаг 0: Анализ и проработка (ПЕРЕД написанием кода)

1. **Прочитать ТЗ** — прочитать `docs/TZ-PHASES-V2-DESIGN.md` (секцию текущей фазы), `docs/TZ-ORBIT-MESSENGER.md` (соответствующие разделы), `CLAUDE.md`
2. **Прочитать текущий код** — изучить все уже написанные сервисы, миграции, фронтенд. Понять что уже есть, какие паттерны используются, какие зависимости
3. **Найти противоречия** — сверить этот план (PHASES.md) с ТЗ-документами. Если есть расхождения — сообщить пользователю ПЕРЕД началом работы
4. **Продумать архитектуру** — предложить:
   - Структуру файлов для новых/изменяемых сервисов
   - Схему взаимодействия между сервисами (HTTP/WS/NATS)
   - SQL-миграции (точный SQL, не псевдокод)
   - Порядок реализации (что первое, что зависит от чего)
5. **Найти потенциальные проблемы** — подумать о:
   - Edge cases (что если 0 чатов? что если 10000 сообщений? что если параллельные запросы?)
   - Безопасность (IDOR, injection, race conditions, rate limits)
   - Перформанс (N+1, отсутствие индексов, большие payloads)
   - Совместимость с TG Web A фронтендом (формат данных, UUID→BigInt конвертация, ожидаемые поля)
6. **Составить план работы** — разбить фазу на конкретные шаги, предложить пользователю на согласование
7. **Получить одобрение** — НЕ НАЧИНАТЬ код пока пользователь не подтвердит план

### Шаг 1: Реализация

- Писать код согласно утверждённому плану
- Отмечать [x] задачи в PHASES.md по мере выполнения
- Коммитить логическими блоками (не один гигантский коммит)

### Шаг 2: Самопроверка

- Перечитать весь написанный код
- Проверить: SQL injection? IDOR? N+1? Race conditions? Error handling?
- Запустить тесты (если написаны)
- Проверить что фронтенд получает данные в ожидаемом формате

### Шаг 3: Отчёт

- Обновить PHASES.md — отметить выполненные задачи
- Записать известные ограничения / tech debt / что не вошло
- Предложить что проверить вручную

---

## Phase 0: Костяк (текущая)

**Цель:** Структура проекта, конфиги, документация.

- [x] Структура папок
- [x] CLAUDE.md
- [x] PHASES.md
- [x] docker-compose.yml + .env.example
- [x] .saturn.yml + .gitignore
- [x] Заглушки всех 8 сервисов
- [x] Копии ТЗ в docs/

---

## Phase 1: Core Messaging

**Цель:** Люди могут переписываться в личке. Надёжно. С reply/forward/edit/pin.
**Сервисы:** gateway, auth, messaging
**Фронтенд:** Форк TG Web A + Saturn API layer

### Проработка (Шаг 0 — до написания кода)

- [x] Прочитать `docs/TZ-PHASES-V2-DESIGN.md` секция Phase 1, `docs/TZ-ORBIT-MESSENGER.md` §5, §6, §8, §11.1
- [ ] Изучить оригинальный TG Web A API layer (`refs/telegram-web-z/src/api/`) — понять формат данных, типы, конвертацию UUID↔BigInt
- [x] Спроектировать точный SQL миграций (CREATE TABLE с индексами, constraints, triggers)
- [x] Спроектировать маршрутизацию gateway: какие запросы проксирует, какие обрабатывает сам
- [x] Продумать формат WS-событий (JSON schema) — что отправляет сервер, что ожидает фронтенд
- [x] Продумать стратегию кэширования JWT-валидации в gateway (Redis TTL vs in-memory)
- [ ] Проверить: как TG Web A ожидает получить список чатов? Какие поля обязательны?
- [ ] Проверить: как TG Web A обрабатывает optimistic UI? Какой формат sendMessage response?
- [x] Оценить: нужен ли OG-tag parser для link preview на бэкенде или defer to Phase 4?
- [x] Составить порядок реализации и предложить пользователю

### Backend: Auth сервис (порт 8081)

- [x] POST /auth/bootstrap — первый admin-аккаунт (самоотключается)
- [x] POST /auth/register — регистрация по инвайту
- [x] POST /auth/login — email + password + optional 2FA TOTP
- [x] POST /auth/logout — выход + Redis blacklist токена
- [x] POST /auth/refresh — ротация refresh-токена (get-then-delete)
- [x] GET /auth/me — валидация сессии, возврат user data
- [x] POST /auth/reset-admin — сброс пароля через ORBIT_ADMIN_RESET_KEY
- [x] GET /auth/sessions — список активных сессий юзера
- [x] DELETE /auth/sessions/:id — отзыв сессии (ПРОВЕРКА принадлежности user_id!)
- [x] POST /auth/2fa/setup — генерация TOTP secret + provisioning URI
- [x] POST /auth/2fa/verify — подтверждение кода, включение 2FA
- [x] POST /auth/2fa/disable — отключение 2FA (с подтверждением пароля)
- [x] POST /auth/invite/validate — проверка инвайт-кода
- [x] POST /auth/invites — создание инвайта (admin only)
- [x] GET /auth/invites — список инвайтов (admin only, все инвайты)
- [x] DELETE /auth/invites/:id — отзыв инвайта (атомарный UPDATE с WHERE created_by)

### Backend: Gateway (порт 8080)

- [x] GET /health — health check
- [x] GET /api/v1/ws — WebSocket endpoint (JWT из query param или header)
- [x] Прокси /api/v1/auth/* → auth сервис (с timeout!)
- [x] Прокси /api/v1/* → messaging сервис (с timeout!)
- [x] JWT middleware — валидация через auth /me с Redis-кэшем (TTL 30s)
- [x] CORS middleware — конкретные origins из FRONTEND_URL, НЕ wildcard
- [x] Rate limiting middleware — Redis-backed (100 req/min/user, 5/min auth, 20/min AI)
- [x] Request logging middleware — structured JSON (slog)
- [x] Ping/pong heartbeat — каждые 30 сек, авто-reconnect с exponential backoff (1s→30s)

### Backend: Messaging (порт 8082)

**Чаты:**
- [x] GET /chats — список чатов юзера (JOIN для last_message, member_count, unread — БЕЗ N+1!)
- [x] POST /chats/direct — создать/получить DM (дедупликация через direct_chat_lookup)
- [x] POST /chats — создать группу (type=group)
- [x] GET /chats/:id — информация о чате + members
- [x] GET /chats/:id/members — список участников (пагинация)

**Сообщения:**
- [x] GET /chats/:id/messages — история (пагинация cursor-based, limit max 100)
- [x] GET /chats/:id/history?date= — jump to message по дате
- [x] POST /chats/:id/messages — отправка (text, replyToId, entities для форматирования)
- [x] PATCH /messages/:id — редактирование (только автор, обновить is_edited + edited_at)
- [x] DELETE /messages/:id — soft-delete (автор или chat admin, is_deleted=true, content=null)
- [x] POST /messages/forward — пересылка batch (is_forwarded=true, forwarded_from=original_sender)
- [x] POST /chats/:id/pin/:messageId — закрепить сообщение
- [x] DELETE /chats/:id/pin/:messageId — открепить
- [x] DELETE /chats/:id/pin — открепить все
- [x] GET /chats/:id/pinned — список закреплённых

**Прочтение:**
- [x] PATCH /chats/:id/read — обновить last_read_message_id

**Пользователи:**
- [x] GET /users/me — текущий юзер
- [x] PUT /users/me — обновить профиль (display_name, bio, custom_status)
- [x] GET /users/:id — профиль юзера
- [x] GET /users?q= — поиск по имени/email (LIMIT 20)

### WebSocket события (через Gateway)

**Server → Client:**
- [x] `new_message` — broadcast всем членам чата (кроме отправителя)
- [x] `message_updated` — при редактировании
- [x] `message_deleted` — при soft-delete
- [x] `messages_read` — при прочтении (inbox/outbox read pointer)
- [x] `user_status` — online (broadcast контактам при connect) + offline (при disconnect с TTL 5min)
- [x] `typing` — broadcast членам чата (auto-expire 6 сек)
- [ ] `stop_typing` — явная остановка

**Client → Server:**
- [x] `typing` — { chat_id } с server-side debounce
- [x] `ping` → server отвечает `pong`

### Database: Миграции

**users:**
- [x] id UUID PK DEFAULT gen_random_uuid()
- [x] email TEXT UNIQUE NOT NULL
- [x] password_hash TEXT NOT NULL
- [x] phone TEXT UNIQUE
- [x] display_name TEXT NOT NULL
- [x] avatar_url TEXT
- [x] bio TEXT
- [x] status TEXT DEFAULT 'offline' (online/offline/recently)
- [x] custom_status TEXT
- [x] custom_status_emoji TEXT
- [x] role TEXT DEFAULT 'member' (admin/member)
- [x] totp_secret TEXT
- [x] totp_enabled BOOLEAN DEFAULT false
- [x] invited_by UUID REFERENCES users(id)
- [x] invite_code TEXT
- [x] last_seen_at TIMESTAMPTZ
- [x] created_at TIMESTAMPTZ DEFAULT now()
- [x] updated_at TIMESTAMPTZ DEFAULT now()

**sessions:**
- [x] id UUID PK
- [x] user_id UUID REFERENCES users(id) ON DELETE CASCADE
- [x] device_id UUID REFERENCES devices(id)
- [x] token_hash TEXT NOT NULL
- [x] ip_address INET
- [x] user_agent TEXT
- [x] expires_at TIMESTAMPTZ NOT NULL
- [x] created_at TIMESTAMPTZ DEFAULT now()

**invites:**
- [x] id UUID PK
- [x] code TEXT UNIQUE NOT NULL (8 hex chars, crypto/rand)
- [x] created_by UUID REFERENCES users(id)
- [x] email TEXT (email-lock)
- [x] role TEXT DEFAULT 'member'
- [x] max_uses INT DEFAULT 1
- [x] use_count INT DEFAULT 0
- [x] used_by UUID REFERENCES users(id)
- [x] used_at TIMESTAMPTZ
- [x] expires_at TIMESTAMPTZ
- [x] is_active BOOLEAN DEFAULT true
- [x] created_at TIMESTAMPTZ DEFAULT now()

**chats:**
- [x] id UUID PK
- [x] type TEXT NOT NULL (direct/group/channel)
- [x] name TEXT
- [x] description TEXT
- [x] avatar_url TEXT
- [x] created_by UUID REFERENCES users(id)
- [x] is_encrypted BOOLEAN DEFAULT false
- [x] max_members INT DEFAULT 200000
- [x] created_at TIMESTAMPTZ DEFAULT now()
- [x] updated_at TIMESTAMPTZ DEFAULT now()

**chat_members:**
- [x] chat_id UUID REFERENCES chats(id) ON DELETE CASCADE
- [x] user_id UUID REFERENCES users(id) ON DELETE CASCADE
- [x] role TEXT DEFAULT 'member' (owner/admin/member/readonly/banned)
- [x] last_read_message_id UUID
- [x] joined_at TIMESTAMPTZ DEFAULT now()
- [x] muted_until TIMESTAMPTZ
- [x] notification_level TEXT DEFAULT 'all' (all/mentions/none)
- [x] PRIMARY KEY (chat_id, user_id)

**direct_chat_lookup:**
- [x] user1_id UUID NOT NULL (CONSTRAINT user1_id < user2_id)
- [x] user2_id UUID NOT NULL
- [x] chat_id UUID REFERENCES chats(id)
- [x] PRIMARY KEY (user1_id, user2_id)

**messages:**
- [x] id UUID PK DEFAULT gen_random_uuid()
- [x] chat_id UUID REFERENCES chats(id) ON DELETE CASCADE
- [x] sender_id UUID REFERENCES users(id)
- [x] type TEXT DEFAULT 'text' (text/photo/video/file/voice/videonote/sticker/poll/system)
- [x] content TEXT
- [x] encrypted_content BYTEA (Phase 7)
- [x] reply_to_id UUID REFERENCES messages(id)
- [x] is_edited BOOLEAN DEFAULT false
- [x] is_deleted BOOLEAN DEFAULT false
- [x] is_pinned BOOLEAN DEFAULT false
- [x] is_forwarded BOOLEAN DEFAULT false
- [x] forwarded_from UUID REFERENCES users(id)
- [x] thread_id UUID
- [x] expires_at TIMESTAMPTZ (disappearing messages, Phase 7)
- [x] sequence_number BIGINT DEFAULT nextval('messages_seq')
- [x] created_at TIMESTAMPTZ DEFAULT now()
- [x] edited_at TIMESTAMPTZ

**devices** (для push + Phase 7 E2E):
- [x] id UUID PK
- [x] user_id UUID REFERENCES users(id) ON DELETE CASCADE
- [x] device_name TEXT
- [x] device_type TEXT (web/desktop/ios/android)
- [x] identity_key BYTEA (Phase 7: Signal Protocol public key)
- [x] push_token TEXT
- [x] push_type TEXT (vapid/fcm/apns)
- [x] last_active_at TIMESTAMPTZ
- [x] created_at TIMESTAMPTZ DEFAULT now()

**Индексы:**
- [x] idx_messages_chat_seq (chat_id, sequence_number DESC)
- [x] idx_messages_chat_created (chat_id, created_at DESC)
- [x] idx_chat_members_user (user_id)
- [x] idx_users_email (email)
- [x] idx_sessions_user (user_id)
- [x] idx_sessions_token (token_hash)

**Триггеры:**
- [x] update_updated_at() → users, chats

### Frontend: Saturn API методы (~35)

**Auth:**
- [ ] checkAuth, validateInviteCode, registerWithInvite, loginWithEmail, logout
- [ ] restartAuth, provideAuthPhoneNumber (compatibility wrapper)

**Users:**
- [ ] fetchCurrentUser, fetchUser, fetchGlobalUsers, searchUsers, updateProfile

**Chats:**
- [ ] fetchChats, fetchFullChat, createDirectChat, createGroupChat
- [ ] getChatInviteLink

**Messages:**
- [ ] fetchMessages, sendMessage, editMessage, deleteMessages
- [ ] forwardMessages, fetchMessageLink, reportMessages
- [ ] fetchPinnedMessages, pinMessage, unpinAllMessages, toggleMessagePinned
- [ ] markMessageListRead, readHistory

**Sync:**
- [ ] fetchUpdateManager, fetchDifference

**WebSocket:**
- [ ] connectWebSocket, disconnectWebSocket, sendTypingAction, pingWebSocket, initWebSocket

**Localization:**
- [ ] fetchLangPack, fetchLanguage, fetchLanguages

**UI features (TG Web A):**
- [ ] Optimistic UI (clock → ✓ → ✓✓)
- [ ] Date separators (Today / Yesterday / Day)
- [ ] Scroll-to-bottom кнопка
- [ ] Rich text: **bold**, *italic*, `code`, ~~strike~~
- [ ] Link previews (OG tags — нужен серверный парсер)

### Критерий "готово"

Логин → список чатов → открыть DM → отправить сообщение → видеть typing → видеть ✓✓ → reply → edit → forward → pin → link preview. Всё real-time через WebSocket. Online/offline статус работает.

---

## Phase 2: Groups & Channels

**Цель:** Рабочие группы и каналы объявлений.
**Сервисы:** messaging (расширить), gateway (WS)

### Проработка (Шаг 0)

- [ ] Прочитать `docs/TZ-PHASES-V2-DESIGN.md` секция Phase 2, `docs/TZ-ORBIT-MESSENGER.md` §11.2
- [ ] Изучить код Phase 1 (messaging сервис) — понять паттерны, store layer, handler conventions
- [ ] Спроектировать различие Groups vs Channels: кто может писать, анонимные посты, linked discussion
- [ ] Спроектировать bitmask permissions — точные значения битов, как проверять в middleware
- [ ] Продумать: как TG Web A отображает каналы vs группы? Какие поля API отличаются?
- [ ] Продумать: invite links с модерацией (join requests) — flow UI + backend
- [ ] SQL: ALTER chat_members + новые таблицы, проверить совместимость с Phase 1 миграциями
- [ ] Оценить: Topics/Forums — делать сейчас или defer?
- [ ] Составить порядок реализации и предложить пользователю

### Backend: Endpoints (15)

- [ ] POST /chats (type=group) — создание группы
- [ ] POST /chats (type=channel) — создание канала
- [ ] PUT /chats/:id — редактировать имя/описание/аватар
- [ ] DELETE /chats/:id — удалить/архивировать
- [ ] POST /chats/:id/members — добавить участника
- [ ] DELETE /chats/:id/members/:userId — удалить/покинуть
- [ ] PATCH /chats/:id/members/:userId — изменить роль
- [ ] GET /chats/:id/members — список (пагинация)
- [ ] GET /chats/:id/members/:userId — инфо об участнике
- [ ] PUT /chats/:id/permissions — default permissions группы
- [ ] PUT /chats/:id/members/:userId/permissions — per-user permissions
- [ ] POST /chats/:id/invite-link — генерация invite link
- [ ] POST /chats/join/:inviteHash — вступить по ссылке
- [ ] GET /chats/:id/admins — список админов
- [ ] POST /chats/:id/slow-mode — включить slow mode (N секунд)

### WebSocket события (5 новых)

- [ ] `chat_created` — новый чат/канал
- [ ] `chat_updated` — изменение настроек
- [ ] `chat_member_added` — участник добавлен
- [ ] `chat_member_removed` — участник удалён/вышел
- [ ] `chat_member_updated` — роль/права изменены

### Database

**ALTER chat_members:**
- [ ] ADD permissions BIGINT DEFAULT 0
- [ ] ADD custom_title TEXT

**Permissions bitmask:**
- [ ] can_send_messages, can_send_media, can_add_members, can_pin_messages
- [ ] can_change_info, can_delete_messages, can_ban_users, can_invite_via_link

**chat_invite_links:**
- [ ] id UUID PK, chat_id, creator_id, hash TEXT UNIQUE
- [ ] expire_at, usage_limit, usage_count, requires_approval BOOLEAN
- [ ] created_at

**chat_join_requests:**
- [ ] chat_id + user_id PK, message TEXT, created_at

### @Mentions (Must)

- [ ] Парсинг @username в тексте сообщения → entities массив
- [ ] Автокомплит @mention в ChatInput (поиск участников чата)
- [ ] Push-уведомление на @mention (даже в замьюченном чате)
- [ ] Backend: хранение mention entities в message, notification при @mention

### Channels vs Groups

- [ ] Groups: все пишут (по permissions), авторы видимы
- [ ] Channels: только owner/admin пишет, автор = название канала (анонимно)
- [ ] Linked discussion group для канала (Nice to Have)

### Frontend: Saturn API методы (~30)

- [ ] createChannel, editChatTitle, editChatDescription
- [ ] updateChatPhoto, deleteChatPhoto
- [ ] addChatMembers, deleteChatMember, leaveChat
- [ ] deleteChat, deleteChannel
- [ ] getChatMember, fetchMembers, updateChatMember
- [ ] updateChatMemberBannedRights, updateChatAdmin, updateChannelAdmin
- [ ] updateChatDefaultBannedRights
- [ ] toggleChatIsProtected, toggleJoinToSend, toggleJoinRequest
- [ ] exportChatInviteLink, editChatInviteLink, joinChat, fetchChatInviteInfo
- [ ] toggleSlowMode
- [ ] archiveChat, unarchiveChat, toggleChatPinned, setChatMuted
- [ ] fetchTopics, createTopic, editTopic, deleteTopic (Nice to Have)

### Критерий "готово"

Создать "MST Dev Team" → добавить 10 человек → назначить 2 admin → чат → pin → invite link → @mention → уведомление. Канал "MST Announcements" → owner пишет, 150 читают. Роли и права работают.

---

## Phase 3: Media & Files

**Цель:** Фото, видео, файлы, голосовые, видео-заметки.
**Сервисы:** media (новый), messaging (расширить)

### Проработка (Шаг 0)

- [ ] Прочитать `docs/TZ-PHASES-V2-DESIGN.md` секция Phase 3, `docs/TZ-ORBIT-MESSENGER.md` §11.3
- [ ] Изучить Cloudflare R2 API — S3-совместимость, presigned URLs, multipart upload
- [ ] Спроектировать media pipeline: upload → process (resize/thumbnail/waveform) → store → serve
- [ ] Решить: обработка изображений на Go (imaging lib) или external service (sharp)?
- [ ] Решить: chunked upload — свой протокол или tus.io?
- [ ] Продумать: как связать media с messages? JOIN table vs inline columns (ТЗ-документы расходятся!)
- [ ] Продумать: как TG Web A отправляет медиа? FormData? Base64? Какой UI flow?
- [ ] Продумать: video streaming — прямой R2 presigned URL или прокси через media сервис?
- [ ] Оценить: ClamAV virus scanning — нужен сейчас или defer?
- [ ] Спроектировать cleanup policy для one-time media и orphaned uploads
- [ ] Составить порядок реализации и предложить пользователю

### Backend: Media сервис (порт 8083) — Endpoints (8)

- [ ] POST /media/upload — загрузка файла, возврат media_id + URLs
- [ ] POST /media/upload/chunked/init — начать chunked upload (>10MB)
- [ ] POST /media/upload/chunked/:uploadId — загрузить chunk
- [ ] POST /media/upload/chunked/:uploadId/complete — завершить
- [ ] GET /media/:id — presigned R2 redirect
- [ ] GET /media/:id/thumbnail — thumbnail
- [ ] DELETE /media/:id — удалить из R2
- [ ] GET /media/:id/info — метаданные (size, type, dimensions, duration)

### Server-side обработка

| Тип | Обработка | Лимит |
|-----|----------|-------|
| Photo | 320px thumb + 800px medium + original; strip EXIF; WebP | ≤10MB |
| Video | Thumbnail из 1-го кадра; metadata (duration, resolution) | ≤2GB, streaming |
| File | Без обработки, иконка по MIME | ≤2GB |
| Voice | Waveform peaks для визуализации; duration | ≤200MB, OGG |
| Video Note | Circular 384px; duration ≤60s | ≤50MB, MP4 |
| GIF | Конвертация в MP4; thumbnail | ≤20MB |

### R2 Storage Layout

```
r2://orbit-media/
├── photos/{media_id}/original.webp, thumb_320.webp, medium_800.webp
├── videos/{media_id}/original.mp4, thumb.jpg
├── files/{media_id}/{original_filename}
├── voice/{media_id}/audio.ogg
└── videonote/{media_id}/video.mp4
```

### WebSocket события (2 новых)

- [ ] `media_upload_progress` — прогресс загрузки больших файлов
- [ ] `media_ready` — thumbnail/resize готов

### Database

**media:**
- [ ] id UUID PK, uploader_id, type, mime_type, original_filename
- [ ] size_bytes BIGINT, r2_key TEXT, thumbnail_r2_key TEXT
- [ ] width INT, height INT, duration_seconds INT
- [ ] waveform_data BYTEA, is_one_time BOOLEAN DEFAULT false
- [ ] created_at

**message_media:**
- [ ] message_id + media_id PK, position INT (порядок в альбоме)

### Frontend: Saturn API методы (~25)

- [ ] uploadMedia, sendMediaMessage, downloadMedia, fetchMessageMedia
- [ ] cancelMediaDownload, cancelMediaUpload
- [ ] sendVoice, sendVideoNote, sendDocument, sendPhoto, sendVideo
- [ ] sendAlbum, fetchSharedMedia, fetchCommonMedia, resendMedia
- [ ] fetchMediaViewers, sendOneTimeMedia, openOneTimeMedia
- [ ] fetchDocumentPreview, setMediaSpoiler, removeMediaSpoiler

### Фичи

- [ ] Drag & Drop в чат
- [ ] Clipboard paste (Ctrl+V для скриншотов)
- [ ] Preview перед отправкой + caption
- [ ] One-time media (self-destruct)
- [ ] Media spoiler (blur до клика)
- [ ] Albums (несколько фото в одном сообщении)
- [ ] Media gallery tab в чате
- [ ] GIF: Tenor API прокси (search + trending + saved)
- [ ] PDF preview

### Критерий "готово"

Drag фото → thumbnail → полное по клику → gallery swipe. Файл → прогресс → скачивание. Голосовое → waveform. Video note круглое. PDF preview. GIF search.

---

## Phase 4: Search, Notifications & Settings

**Цель:** Найти любое сообщение за секунды. Не пропустить ничего. Настроить под себя.
**Сервисы:** gateway (push), messaging (search), Meilisearch

### Проработка (Шаг 0)

- [ ] Прочитать `docs/TZ-PHASES-V2-DESIGN.md` секция Phase 4, `docs/TZ-ORBIT-MESSENGER.md` §11.4
- [ ] Изучить Meilisearch API — индексация, фильтры, ranking rules, typo tolerance
- [ ] Спроектировать: какие данные индексировать? Как синхронизировать с PostgreSQL? (trigger vs CDC vs cron)
- [ ] Спроектировать Web Push: VAPID key generation, subscription storage, payload format
- [ ] Продумать: delivery logic (online → WS → push → mute check → DND check → @mention override)
- [ ] Продумать: как TG Web A обрабатывает push? Service Worker? Notification API?
- [ ] Продумать: Phase 7 E2E сломает серверный поиск — нужна ли архитектура, готовая к этому?
- [ ] Оценить: FCM/APNs — нужны сейчас (для PWA) или только для native apps?
- [ ] Спроектировать settings sync — серверное хранение vs localStorage
- [ ] Составить порядок реализации и предложить пользователю

### Backend: Search (через Meilisearch)

- [ ] GET /search?q=&scope=messages — глобальный поиск сообщений
- [ ] GET /search?q=&scope=users — поиск юзеров
- [ ] GET /search?q=&scope=chats — поиск чатов
- [ ] GET /search?q=&scope=media — поиск медиа (по caption + filename)
- [ ] Фильтры: chat_id, from_user_id, date_from, date_to, type, has_media

### Backend: Notifications (4 endpoints)

- [ ] POST /push/subscribe — регистрация push-подписки
- [ ] DELETE /push/subscribe — отписка
- [ ] PUT /users/me/notifications — глобальные настройки уведомлений
- [ ] PUT /chats/:id/notifications — per-chat (mute, sound)

**Delivery logic:** online → WS in-app → нет → Web Push (VAPID) / FCM / APNs → muted → skip (кроме @mention) → DND → skip

### Backend: Settings (8 endpoints)

- [ ] GET /users/me/settings/privacy — настройки приватности
- [ ] PUT /users/me/settings/privacy — обновить
- [ ] GET /users/me/settings/notifications — глобальные настройки
- [ ] PUT /users/me/settings/notifications — обновить
- [ ] GET /users/me/settings/appearance — тема, язык, шрифт
- [ ] PUT /users/me/settings/appearance — обновить
- [ ] PUT /users/me/username — сменить @username
- [ ] PUT /users/me/avatar — загрузить аватар
- [ ] DELETE /users/me/avatar — удалить аватар
- [ ] GET /users/me/blocked — список заблокированных
- [ ] POST /users/me/blocked/:userId — заблокировать
- [ ] DELETE /users/me/blocked/:userId — разблокировать

### Database (5 новых таблиц)

**push_subscriptions:**
- [ ] id, user_id, endpoint TEXT, p256dh TEXT, auth TEXT, user_agent TEXT, push_type, created_at

**notification_settings:**
- [ ] user_id + chat_id PK, muted_until, sound TEXT DEFAULT 'default', show_preview BOOLEAN DEFAULT true

**privacy_settings:**
- [ ] user_id PK, last_seen DEFAULT 'everyone', avatar DEFAULT 'everyone'
- [ ] phone DEFAULT 'contacts', calls DEFAULT 'everyone'
- [ ] groups DEFAULT 'everyone', forwarded DEFAULT 'everyone'

**blocked_users:**
- [ ] user_id + blocked_user_id PK, created_at

**user_settings:**
- [ ] user_id PK, theme DEFAULT 'auto', language DEFAULT 'ru'
- [ ] font_size INT DEFAULT 16, send_by_enter BOOLEAN DEFAULT true
- [ ] dnd_from TIME, dnd_until TIME, updated_at

### Дополнительные фичи

- [ ] In-app notification banners (сообщение в другом чате → баннер сверху)
- [ ] In-chat search (лупа → фильтры: from user, date, type)

### Frontend: Saturn API методы (~25)

**Search:**
- [ ] searchMessages, searchChatMessages, fetchSearchHistory, searchHashtag, getMessageByDate

**Notifications:**
- [ ] registerDevice, unregisterDevice, updateNotifySettings, getNotifySettings
- [ ] updateGlobalNotifySettings, resetNotifySettings, muteChat, unmuteChat

**Settings:**
- [ ] getPrivacySettings, setPrivacySettings, fetchBlockedUsers, blockUser, unblockUser
- [ ] fetchActiveSessions, terminateSession, terminateAllSessions
- [ ] updateUsername, checkUsername, fetchLanguageStrings
- [ ] updateProfilePhoto, deleteProfilePhoto

### Критерий "готово"

Поиск "отчёт" → найти сообщение за февраль → клик → прокрутка. Web Push в фоне. Mute группы. Dark theme. Скрыть last seen. Font size 18px.

---

## Phase 5: Rich Messaging

**Цель:** Реакции, стикеры, GIF, опросы, scheduled messages. Всё бесплатно.
**Сервисы:** messaging (расширить), gateway (WS)

### Проработка (Шаг 0)

- [ ] Прочитать `docs/TZ-PHASES-V2-DESIGN.md` секция Phase 5, `docs/TZ-ORBIT-MESSENGER.md` §11.5
- [ ] Изучить TG Web A sticker rendering — TGS (Lottie), WebP, WebM. Какие библиотеки (rlottie)?
- [ ] Изучить Tenor API — rate limits, API key, response format, caching strategy
- [ ] Спроектировать sticker import из Telegram — как работает TG Bot API fetchStickerSet? Легальность?
- [ ] Спроектировать scheduled messages — Go cron job (interval?), timezone handling, delivery guarantee
- [ ] Продумать: isPremium removal — grep все проверки в TG Web A, составить список файлов для изменения
- [ ] Продумать: reactions animation — что отправляет сервер? Какой формат ожидает фронтенд?
- [ ] Продумать: polls real-time — WS broadcast при каждом голосе или батчинг?
- [ ] Оценить: custom emoji — нужен свой рендер или TG Web A уже умеет?
- [ ] Составить порядок реализации и предложить пользователю

### Backend: Endpoints

**Реакции:**
- [ ] POST /messages/:id/reactions — добавить
- [ ] DELETE /messages/:id/reactions — удалить
- [ ] GET /messages/:id/reactions — список
- [ ] PUT /chats/:id/available-reactions — настроить доступные

**Стикеры:**
- [ ] GET /stickers/featured — рекомендуемые паки
- [ ] GET /stickers/search?q= — поиск
- [ ] GET /stickers/sets/:id — получить пак
- [ ] POST /stickers/sets/:id/install — установить
- [ ] DELETE /stickers/sets/:id/install — удалить
- [ ] GET /stickers/installed — мои паки
- [ ] GET /stickers/recent — недавние

**GIF:**
- [ ] GET /gifs/search?q= — поиск (Tenor прокси)
- [ ] GET /gifs/trending — трендовые
- [ ] GET /gifs/saved — сохранённые
- [ ] POST /gifs/saved — сохранить
- [ ] DELETE /gifs/saved/:id — удалить

**Опросы:**
- [ ] POST /chats/:id/messages (type=poll) — создать
- [ ] POST /messages/:id/poll/vote — проголосовать
- [ ] DELETE /messages/:id/poll/vote — отозвать голос
- [ ] POST /messages/:id/poll/close — закрыть

**Scheduled:**
- [ ] POST /chats/:id/messages?scheduled_at= — запланировать
- [ ] GET /chats/:id/messages/scheduled — список запланированных
- [ ] PATCH /messages/:id/scheduled — изменить время/текст
- [ ] DELETE /messages/:id/scheduled — удалить
- [ ] POST /messages/:id/scheduled/send-now — отправить сейчас

### WebSocket события (4 новых)

- [ ] `reaction_added` — реакция добавлена
- [ ] `reaction_removed` — реакция удалена
- [ ] `poll_vote` — обновление результатов опроса
- [ ] `poll_closed` — опрос завершён

### Database (7 таблиц)

- [ ] message_reactions: message_id + user_id + emoji PK, created_at
- [ ] chat_available_reactions: chat_id PK, mode DEFAULT 'all', allowed_emojis TEXT[]
- [ ] sticker_packs: id, name, short_name, author_id, thumbnail_url, is_official, is_animated, sticker_count
- [ ] stickers: id, pack_id, emoji, file_url, file_type, width, height, position
- [ ] user_installed_stickers: user_id + pack_id PK, position, installed_at
- [ ] recent_stickers: user_id + sticker_id PK, used_at
- [ ] polls: id, message_id, question, is_anonymous, is_multiple, is_quiz, correct_option, close_at
- [ ] poll_options: id, poll_id, text, position
- [ ] poll_votes: poll_id + user_id + option_id PK, voted_at

### Frontend: Saturn API методы (~40)

**Реакции:**
- [ ] sendReaction, fetchMessageReactionsList, fetchAvailableReactions, setDefaultReaction, setChatEnabledReactions

**Стикеры:**
- [ ] fetchStickerSets, fetchRecentStickers, fetchFavoriteStickers, fetchFeaturedStickers
- [ ] searchStickers, installStickerSet, uninstallStickerSet
- [ ] addRecentSticker, removeRecentSticker, addFavoriteSticker, removeFavoriteSticker
- [ ] fetchCustomEmoji, fetchCustomEmojiSets

**GIF:**
- [ ] fetchGifs, searchGifs, fetchSavedGifs, saveGif, removeGif

**Polls:**
- [ ] sendPoll, votePoll, closePoll, fetchPollVoters

**Scheduled:**
- [ ] fetchScheduledHistory, sendScheduledMessages, editScheduledMessage, deleteScheduledMessages, rescheduleMessage

**Other:**
- [ ] fetchSavedMessages, toggleSavedDialogPinned, fetchCommonChats

### No Premium — всё бесплатно

- [ ] Убрать все isPremium проверки в TG Web A (return true)
- [ ] Custom emoji в имени/статусе — бесплатно
- [ ] Animated emoji — бесплатно
- [ ] Все стикер-паки — бесплатно
- [ ] Extended upload limits — бесплатно
- [ ] Emoji status — бесплатно

### Критерий "готово"

Long-press → реакция → анимация. Стикер-пикер → установить пак → отправить. GIF search → send. Опрос → голосование real-time. Schedule message на завтра 9:00. Custom emoji в статусе.

---

## Phase 6: Voice & Video Calls

**Цель:** 1-на-1 и групповые звонки с шарингом экрана.
**Сервисы:** calls (новый, порт 8084), gateway (WS signaling)
**Инфра:** Pion SFU на Saturn.ac, coturn на Hetzner VPS

### Проработка (Шаг 0)

- [ ] Прочитать `docs/TZ-PHASES-V2-DESIGN.md` секция Phase 6, `docs/TZ-ORBIT-MESSENGER.md` §11.6
- [ ] Изучить Pion WebRTC Go library — SFU vs MCU, room management, codec support
- [ ] Изучить coturn — конфигурация, REST API для credential rotation, HA (single point of failure!)
- [ ] Изучить TG Web A calls UI — какие WebRTC events ожидает? Какой signaling protocol?
- [ ] Спроектировать: WS signaling flow (offer → answer → ICE → connected) через gateway
- [ ] Спроектировать: как gateway маршрутизирует signaling events к calls сервису?
- [ ] Продумать: P2P vs SFU routing — автоматический выбор (2 участника → P2P, >2 → SFU)
- [ ] Продумать: screen sharing — getDisplayMedia API, как мультиплексировать с камерой?
- [ ] Продумать: push для звонков — высокоприоритетный push когда app закрыт
- [ ] Оценить: coturn HA — нужен fallback сервер или достаточно одного для 150 юзеров?
- [ ] Составить порядок реализации и предложить пользователю

### Architecture

```
P2P:  browser ←→ browser (1-на-1, direct WebRTC)
TURN: coturn на Hetzner (relay при корпоративном NAT)
SFU:  Pion (группа до 50 — каждый шлёт 1 поток, SFU раздаёт)
Signaling: WebSocket через gateway
```

### Backend: Endpoints (12)

- [ ] POST /calls — инициировать звонок
- [ ] PUT /calls/:id/accept — принять
- [ ] PUT /calls/:id/decline — отклонить
- [ ] PUT /calls/:id/end — завершить
- [ ] GET /calls/:id — статус звонка
- [ ] GET /calls/history — история звонков
- [ ] POST /calls/:id/participants — добавить участника (group)
- [ ] DELETE /calls/:id/participants/:userId — удалить участника
- [ ] PUT /calls/:id/mute — mute/unmute
- [ ] PUT /calls/:id/screen-share/start — начать шаринг
- [ ] PUT /calls/:id/screen-share/stop — остановить шаринг
- [ ] GET /calls/:id/ice-servers — получить TURN/STUN credentials

### WebSocket Signaling (11 событий)

**Server → Client:**
- [ ] `call_incoming` — входящий звонок (ringtone)
- [ ] `call_accepted` — собеседник принял
- [ ] `call_declined` — собеседник отклонил
- [ ] `call_ended` — звонок завершён
- [ ] `call_participant_joined` — присоединился к групповому
- [ ] `call_participant_left` — покинул групповой

**Bidirectional:**
- [ ] `webrtc_offer` — SDP offer
- [ ] `webrtc_answer` — SDP answer
- [ ] `webrtc_ice_candidate` — ICE candidate
- [ ] `call_muted` / `call_unmuted` — статус микрофона
- [ ] `screen_share_started` / `screen_share_stopped`

### Database (2 таблицы)

**calls:**
- [ ] id, type (voice/video), mode (p2p/group), chat_id, initiator_id
- [ ] status (ringing/active/ended/missed/declined)
- [ ] started_at, ended_at, duration_seconds, created_at

**call_participants:**
- [ ] call_id + user_id PK, joined_at, left_at
- [ ] is_muted, is_camera_off, is_screen_sharing

### Frontend: Saturn API методы (~20)

- [ ] createCall, acceptCall, declineCall, hangUp
- [ ] joinGroupCall, leaveGroupCall
- [ ] toggleCallMute, toggleCallCamera
- [ ] startScreenShare, stopScreenShare
- [ ] fetchCallParticipants, fetchCallHistory, rateCall
- [ ] sendWebRtcOffer, sendWebRtcAnswer, sendIceCandidate, fetchIceServers
- [ ] inviteToCall, setCallSpeaker

### Дополнительные фичи

- [ ] Ringtone + vibration на входящий
- [ ] Push-уведомление на звонок когда app закрыт
- [ ] Network quality indicator
- [ ] Call rating после завершения (Nice to Have)

### Критерий "готово"

Кнопка телефона → ringtone → принять → голос P2P. Видео → камера. Группа "Начать звонок" → 10 участников → video grid → screen share. Call history в профиле.

---

## Phase 7: E2E Encryption

**Цель:** Zero-Knowledge — сервер не может прочитать DM. Криптографическая гарантия.
**Сервисы:** auth (key server), messaging (encrypt/decrypt), shared/crypto (новый)

### Проработка (Шаг 0)

- [ ] Прочитать `docs/TZ-PHASES-V2-DESIGN.md` секция Phase 7, `docs/TZ-ORBIT-MESSENGER.md` §9, §10, §11.7, `docs/SIGNAL_PROTOCOL.md` (если есть)
- [ ] Изучить libsignal-protocol-js — API, key generation, session management, message encrypt/decrypt
- [ ] Изучить Signal Protocol Go implementations — какая библиотека для key server?
- [ ] **Критический вопрос:** как совместить E2E с Super Access (Killer Feature #1)? Escrow Key design
- [ ] Спроектировать: key management lifecycle (registration → key upload → session creation → ratchet → key rotation)
- [ ] Спроектировать: multi-device — как шифровать для N устройств получателя?
- [ ] Спроектировать: Sender Keys для групп — key distribution, rotation при leave/join
- [ ] Продумать: impact на Meilisearch (Phase 4) — client-side search architecture
- [ ] Продумать: impact на push notifications — "Новое сообщение" без plaintext
- [ ] Продумать: impact на media — AES-256-GCM перед R2 upload, как передать ключ получателю?
- [ ] Продумать: миграция — существующие plaintext сообщения в БД, что с ними?
- [ ] Оценить: Sealed Sender — реалистично ли для нашей архитектуры?
- [ ] Составить rollout план (opt-in → default) и предложить пользователю

### Signal Protocol Flow

1. Alice запрашивает public keys Bob с сервера
2. Сервер возвращает Identity Key + Signed PreKey + One-Time PreKey
3. Alice: X3DH → вычислить shared secret
4. Alice: шифрование AES-256-GCM → отправить ciphertext
5. Bob: X3DH с ключами Alice → расшифровка
6. Далее: Double Ratchet — новый ключ после КАЖДОГО сообщения

### Backend: Key Server Endpoints (7, расширение auth)

- [ ] POST /keys/identity — загрузить Identity Key (раз при регистрации)
- [ ] POST /keys/signed-prekey — загрузить Signed PreKey (ротация еженедельно)
- [ ] POST /keys/one-time-prekeys — загрузить batch 100 One-Time PreKeys
- [ ] GET /keys/:userId/bundle — получить key bundle для начала сессии
- [ ] GET /keys/:userId/identity — получить Identity Key (для Safety Numbers)
- [ ] GET /keys/count — сколько One-Time PreKeys осталось
- [ ] GET /keys/transparency-log — публичный лог изменений ключей

### Encrypted Message Format

```json
{
  "text": null,
  "encrypted": true,
  "ciphertext": "base64-blob",
  "sender_identity_key": "base64",
  "session_version": 3
}
```

### Database (3 таблицы)

**user_keys:**
- [ ] user_id + device_id PK
- [ ] identity_key BYTEA, signed_prekey BYTEA, signed_prekey_signature BYTEA
- [ ] signed_prekey_id INT, uploaded_at TIMESTAMPTZ

**one_time_prekeys:**
- [ ] id SERIAL, user_id, device_id, key_id INT
- [ ] public_key BYTEA, used BOOLEAN DEFAULT false

**key_transparency_log:**
- [ ] id SERIAL, user_id, event_type TEXT, public_key_hash TEXT, created_at

### Frontend: Saturn API методы (~15)

- [ ] uploadIdentityKey, uploadSignedPreKey, uploadOneTimePreKeys
- [ ] fetchKeyBundle, fetchIdentityKey, fetchPreKeyCount
- [ ] sendEncryptedMessage, fetchKeyTransparencyLog
- [ ] verifyIdentity, setDisappearingTimer, fetchDisappearingTimer

### Фичи

- [ ] Sender Keys для группового E2E
- [ ] Safety Numbers: QR + числовое сравнение
- [ ] Disappearing messages: 24h / 7d / 30d / Off
- [ ] Шифрование медиа (AES-256-GCM) перед загрузкой в R2
- [ ] Multi-device: шифрование отдельно для КАЖДОГО устройства получателя
- [ ] Client-side search (Meilisearch не видит plaintext)
- [ ] Push показывает "Новое сообщение" без текста

### Rollout план

1. Opt-in для DM
2. Default для новых DM
3. Группы opt-in
4. Default для всех

### Критерий "готово"

Открыть DM → замок "E2E encrypted". Отправить → сервер хранит ciphertext. Safety Numbers → QR → "Verified". Disappearing 24h. Admin в БД → blob.

---

## Phase 8: AI, Bots, Integrations & Production

**Цель:** Claude AI встроен, боты работают, MST-тулы подключены, мониторинг.

### Проработка (Шаг 0)

- [ ] Прочитать `docs/TZ-PHASES-V2-DESIGN.md` секция Phase 8, `docs/TZ-ORBIT-MESSENGER.md` §11.8, §11.9
- [ ] **8A AI:** Изучить Anthropic Claude API — streaming (SSE), rate limits, pricing, context window
- [ ] **8A AI:** Изучить Whisper API (OpenAI) — audio formats, language detection, speaker diarization options
- [ ] **8A AI:** Спроектировать embedding pipeline для semantic search (какой model? pgvector vs Qdrant?)
- [ ] **8B Bots:** Изучить Telegram Bot API spec — какие методы критичны для совместимости?
- [ ] **8B Bots:** Спроектировать webhook delivery — retry strategy, dead letter queue, timeout handling
- [ ] **8C Integrations:** Изучить API InsightFlow, Keitaro, Saturn.ac — форматы webhook payload
- [ ] **8C Integrations:** Спроектировать generic webhook framework — verification (HMAC), retry, logging
- [ ] **8D ScyllaDB:** Спроектировать migration strategy — dual-write? Shadow read? Cut-over?
- [ ] **8D ScyllaDB:** Спроектировать CQL schema — partition key (chat_id + day bucket), clustering (sequence_number DESC)
- [ ] **8D NATS:** Спроектировать stream topology — MESSAGES, EVENTS, PUSH, WEBHOOKS
- [ ] **8D Monitoring:** Выбрать: Prometheus+Grafana managed (Saturn?) или self-hosted?
- [ ] **8D Security:** Составить OWASP checklist для аудита всех сервисов
- [ ] Продумать: backup strategy — pg_dump schedule, WAL archiving, R2 cross-region
- [ ] Это самая большая фаза — разбить на подфазы (8A→8B→8C→8D) с отдельными PR
- [ ] Составить порядок реализации и предложить пользователю

### 8A: AI сервис (порт 8085)

**Endpoints (6):**
- [ ] POST /ai/summarize — суммаризация чата (Claude API, SSE streaming)
- [ ] POST /ai/translate — перевод N сообщений (SSE streaming)
- [ ] POST /ai/reply-suggest — 3 варианта ответа
- [ ] POST /ai/transcribe — транскрипция голосовых (Whisper API)
- [ ] POST /ai/search — семантический поиск (embeddings)
- [ ] GET /ai/usage — статистика использования AI

**Rate limit:** 20 AI-запросов/мин/юзер. Бесплатно для всех.

**@orbit-ai бот** (Nice to Have):
- [ ] Диалог с AI в любом чате через @orbit-ai mention

**Saturn методы (~10):**
- [ ] summarizeChat, translateMessages, suggestReply
- [ ] transcribeVoice, semanticSearch, explainMessage, fetchAiUsage

### 8B: Bots сервис (порт 8086)

**Admin endpoints:**
- [ ] POST /bots — создать бота (возврат token)
- [ ] GET /bots — список моих ботов
- [ ] PUT /bots/:id — редактировать
- [ ] DELETE /bots/:id — удалить
- [ ] POST /bots/:id/commands — установить /commands
- [ ] POST /bots/:id/webhook — установить webhook URL
- [ ] GET /bots/:id/webhook/logs — логи вызовов

**TG-совместимый Bot API:**
- [ ] getMe, sendMessage, editMessageText, deleteMessage
- [ ] answerCallbackQuery, setWebhook, deleteWebhook, getUpdates
- [ ] sendPhoto, sendDocument, sendVoice

**Saturn методы (~10):**
- [ ] fetchBotInfo, sendBotCommand, answerCallbackQuery
- [ ] fetchInlineResults, sendInlineResult
- [ ] requestBotWebView, closeBotWebView, loadAttachBot, toggleAttachBot

**Фичи:**
- [ ] Webhook delivery с retry (exponential backoff)
- [ ] /commands автокомплит в ChatInput
- [ ] Inline keyboards под сообщениями ботов
- [ ] Inline mode (@botname query)
- [ ] Bot management UI в настройках

### 8C: Integrations сервис (порт 8087)

**Endpoints:**
- [ ] POST /webhooks — создать webhook
- [ ] GET /webhooks — список
- [ ] PUT /webhooks/:id — редактировать
- [ ] DELETE /webhooks/:id — удалить
- [ ] GET /webhooks/:id/logs — логи
- [ ] POST /webhooks/:id/test — тестовый вызов

**MST интеграции:**
- [ ] InsightFlow → #alerts канал
- [ ] Keitaro postbacks → уведомления
- [ ] Saturn.ac deploy status → #dev
- [ ] HR-бот миграция из Telegram
- [ ] ASA Analytics → campaign alerts

**Saturn методы (~8):**
- [ ] fetchWebhooks, createWebhook, editWebhook, deleteWebhook
- [ ] fetchWebhookLogs, testWebhook, fetchIntegrations, toggleIntegration

**Database Phase 8B — bots:**
- [ ] id UUID PK, owner_id, username TEXT UNIQUE, display_name, description
- [ ] avatar_url, webhook_url, api_token TEXT UNIQUE, is_inline BOOLEAN
- [ ] commands JSONB, is_active BOOLEAN DEFAULT true, created_at

**Database Phase 8C — webhooks:**
- [ ] id UUID PK, chat_id, name, webhook_url, incoming_webhook_token TEXT UNIQUE
- [ ] events JSONB, config JSONB, is_active BOOLEAN, created_by, created_at

### 8D: Production Hardening

**ScyllaDB миграция:**
- [ ] Messages table → ScyllaDB (partitioned by chat_id + day bucket (unix/86400), clustered by sequence_number DESC)
- [ ] Dual-write стратегия миграции (write PostgreSQL + ScyllaDB, read from ScyllaDB, fallback PostgreSQL)
- [ ] Target: 1000 msg/sec

**NATS JetStream:**
- [ ] Stream MESSAGES — гарантированная доставка
- [ ] Stream EVENTS — WS events (typing, status, reactions)
- [ ] Stream PUSH — push notification queue
- [ ] Stream WEBHOOKS — webhook delivery queue

**Redis:**
- [ ] `online:{userId}` → TTL 5min (heartbeat)
- [ ] `typing:{chatId}:{userId}` → TTL 6sec
- [ ] `session:{tokenHash}` → user data cache
- [ ] `ratelimit:{userId}:{endpoint}` → counter
- [ ] `jwt_blacklist:{tokenHash}` → TTL = remaining token validity

**Мониторинг:**
- [ ] Prometheus — RPS, latency p50/p95/p99, error rate, WS connections
- [ ] Grafana — real-time dashboards
- [ ] Structured JSON logging → Loki
- [ ] OpenTelemetry distributed tracing
- [ ] External healthcheck каждые 30 сек
- [ ] Alerts → Orbit канал "MST Monitoring" (dogfooding)

**Security audit:**
- [ ] OWASP Top 10
- [ ] Dependency scan
- [ ] Rate limiting ALL endpoints
- [ ] Input validation (XSS, SQL injection)
- [ ] CORS whitelist
- [ ] Secrets rotation
- [ ] Penetration test
- [ ] GPL-3.0 compliance: license headers, source availability

**Backup:**
- [ ] PostgreSQL: pg_dump + WAL archiving (daily + PITR)
- [ ] ScyllaDB: snapshot + incremental (daily)
- [ ] Redis: RDB snapshot каждые 15 мин
- [ ] R2: cross-region replication (real-time)
- [ ] E2E keys: client-side encrypted backup (user-initiated)

**Scaling roadmap (150 → 500 → 1000+):**
- [ ] 150 users: single PostgreSQL, single Redis, single gateway instance
- [ ] 500 users: PostgreSQL read replicas, Redis Sentinel, ScyllaDB, 2-3 gateway instances + LB
- [ ] 1000+ users: sharded PostgreSQL, Redis Cluster, ScyllaDB multi-DC, auto-scaling gateway

**Performance targets:**
- [ ] Message delivery: p99 <100ms
- [ ] API response: p95 <200ms
- [ ] WS connections: 500 concurrent/instance
- [ ] Media upload: >100 MB/s aggregate
- [ ] Search: <50ms per query
- [ ] Frontend TTI: <3 seconds
- [ ] Frontend bundle: <2MB gzipped
- [ ] 150+ concurrent users без деградации

**Inter-service communication:**
- [ ] HTTP (текущий) — достаточно для 150 юзеров
- [ ] gRPC миграция — оценить при масштабировании до 500+ (опционально)

### Критерий "готово"

AI sparkle → "Суммаризируй за час" → streaming. Голосовое → транскрипция. HR-бот в Orbit. InsightFlow → "#alerts: Новая конверсия!". Grafana зелёная. 150 юзеров онлайн, <100ms.

---

## Параллельные треки

### Desktop (после Phase 4)

- [ ] Tauri 2.0 обёртка вокруг web/
- [ ] .dmg / .exe / .AppImage / .deb (~10-15MB)
- [ ] Deep links: `orbit://chat/{id}`
- [ ] Auto-update (Tauri updater plugin)
- [ ] System tray: иконка + badge (unread) + context menu
- [ ] Native notifications (OS-native, не browser)
- [ ] Auto-launch при старте системы (опционально)
- [ ] Global shortcut: Ctrl+Shift+O

### Mobile

- [ ] PWA (уже встроено в TG Web A — Service Worker + manifest)
- [ ] Нативные: оценить после Phase 6 (форк TG-iOS Swift / TG-Android Kotlin)

---

## Killer Features (после Phase 8)

Подробности: `docs/TZ-KILLER-FEATURES.md`

| # | Фича | Дни | Фаза | Волна |
|---|------|-----|------|-------|
| 1 | Super Access (C-Level AI аналитика) | 27 | 9+ | 3 |
| 2 | AI Meeting Notes | 17 | 8 | 2 |
| 3 | Smart Notifications | 10 | 8 | 2 |
| 4 | Workflow Automations | 15 | 8 | 2 |
| 5 | Knowledge Base | 12 | 9+ | 3 |
| 6 | Live Translate | 8 | 8 | 2 |
| 7 | Video Notes Pro | 10 | 3 | 1 |
| 8 | Anonymous Feedback (ring signatures) | 12 | 7 | 3 |
| 9 | Status Automations | 8 | 4 | 1 |
| 10 | Team Pulse (HR dashboard) | 15 | 9+ | 3 |
| 11 | Orbit Spaces (voice rooms) | 12 | 6 | 1 |

**Волна 1** (параллельно Phases 3-6): Video Notes Pro, Status Automations, Orbit Spaces
**Волна 2** (Phase 8): Live Translate, Smart Notifications, AI Meeting Notes, Workflow Automations
**Волна 3** (Phase 9+): Anonymous Feedback, Knowledge Base, Team Pulse, Super Access
