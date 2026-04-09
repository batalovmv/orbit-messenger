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

## Phase 0: Костяк (done)

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
- [x] Изучить оригинальный TG Web A API layer — Saturn API layer реализован с UUID↔sequence_number маппингом
- [x] Спроектировать точный SQL миграций (CREATE TABLE с индексами, constraints, triggers)
- [x] Спроектировать маршрутизацию gateway: какие запросы проксирует, какие обрабатывает сам
- [x] Продумать формат WS-событий (JSON schema) — что отправляет сервер, что ожидает фронтенд
- [x] Продумать стратегию кэширования JWT-валидации в gateway (Redis TTL vs in-memory)
- [x] Проверить: как TG Web A ожидает получить список чатов — реализовано в buildApiChat + fetchChats
- [x] Проверить: optimistic UI — реализовано: sendMessage → localId → updateMessageSendSucceeded с race guard
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
- [x] `stop_typing` — явная остановка

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
- [x] entities JSONB (migration 012 — bold/italic/code/link formatting)

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
- [x] checkAuth, validateInviteCode, registerWithInvite, loginWithEmail, logout
- [x] restartAuth, provideAuthPhoneNumber (compatibility wrapper)

**Users:**
- [x] fetchCurrentUser, fetchUser, searchUsers, updateProfile
- [x] fetchGlobalUsers

**Chats:**
- [x] fetchChats, fetchFullChat, createDirectChat, createGroupChat
- [x] getChatInviteLink

**Compat / wiring:**
- [x] TG Web A ↔ Saturn parity for chat/profile photo flows and archive toggle (`editChatPhoto`, `updateProfilePhoto`/`uploadProfilePhoto`, `toggleChatArchived`), avatar hash media resolution, `settingsApi` debug logging, passcode timeout fix
- [x] Guard notification settings Saturn calls against missing `chat.id` in wrappers and chat actions

**Messages:**
- [x] fetchMessages, sendMessage, editMessage, deleteMessages
- [x] forwardMessages, fetchPinnedMessages, pinMessage, unpinAllMessages
- [x] markMessageListRead, sendMessageAction (typing)
- [x] fetchMessageLink (client-side hash link, серверный deep link → deferred to Phase 4)
- [~] reportMessages — deferred to Phase 4 (модерация, не нужно для invite-only 150 человек)

**Sync:**
- [x] fetchDifference — no-op stub; вместо diff protocol: re-fetch chats при WS reconnect
- [~] fetchUpdateManager — deferred (не нужен: WS + reconnect re-fetch покрывает Phase 1)

**WebSocket:**
- [x] connectWebSocket, disconnectWebSocket, initWebSocket, pingWebSocket
- [x] WS events: new_message, message_updated, message_deleted, messages_read, typing, user_status

**Localization:**
- [x] fetchLangPack, fetchLanguages
- [x] fetchLanguage — реализован в Saturn settings.ts (fallback strings)

**UI features (TG Web A):**
- [x] Optimistic UI (clock → ✓ → ✓✓) — inherited from TG Web A fork
- [x] Date separators (Today / Yesterday / Day) — inherited from TG Web A fork
- [x] Scroll-to-bottom кнопка — inherited from TG Web A fork
- [x] Rich text: **bold**, *italic*, `code`, ~~strike~~ — inherited from TG Web A fork
- [x] Link previews (OG tags — серверный парсер + Saturn endpoint реализованы)

### Известные отложения и расхождения с ТЗ

- **Scheduled messages** (TZ-PHASES-V2-DESIGN перечисляет в Phase 1) → deferred to Phase 5 (Rich Messaging). Логически относятся к Phase 5 вместе с reactions/stickers/polls.
- **Message search** (TZ §11.1, Should) → deferred to Phase 4 (Meilisearch). PostgreSQL ILIKE недостаточен для full-text search.
- **`chat_members.permissions` / `custom_title`** (TZ §6.1 — базовая схема) → Phase 2 ALTER. В Phase 1 используются роли (owner/admin/member), permission bitmask не нужен.
- **`reportMessages`** → Phase 4 (модерация). Для invite-only 150 человек не критично.
- **Migration 013**: `direct_chat_lookup.chat_id ON DELETE CASCADE` — добавлено для корректного удаления чатов.

### Критерий "готово"

Логин → список чатов → открыть DM → отправить сообщение → видеть typing → видеть ✓✓ → reply → edit → forward → pin → link preview. Всё real-time через WebSocket. Online/offline статус работает.

---

## Phase 2: Groups & Channels

**Цель:** Рабочие группы и каналы объявлений.
**Сервисы:** messaging (расширить), gateway (WS)

### Проработка (Шаг 0)

- [x] Прочитать `docs/TZ-PHASES-V2-DESIGN.md` секция Phase 2, `docs/TZ-ORBIT-MESSENGER.md` §11.2
- [x] Изучить код Phase 1 (messaging сервис) — понять паттерны, store layer, handler conventions
- [x] Спроектировать различие Groups vs Channels: кто может писать, анонимные посты, linked discussion
- [x] Спроектировать bitmask permissions — точные значения битов, как проверять в middleware
- [x] Продумать: как TG Web A отображает каналы vs группы? Какие поля API отличаются?
- [x] Продумать: invite links с модерацией (join requests) — flow UI + backend
- [x] SQL: ALTER chat_members + новые таблицы, проверить совместимость с Phase 1 миграциями
- [x] Оценить: Topics/Forums — делать сейчас или defer? → Deferred to Phase 5
- [x] Составить порядок реализации и предложить пользователю

### Backend: Endpoints (15 + 8 bonus)

- [x] POST /chats (type=group) — создание группы
- [x] POST /chats (type=channel) — создание канала
- [x] PUT /chats/:id — редактировать имя/описание/аватар
- [x] DELETE /chats/:id — удалить/архивировать
- [x] POST /chats/:id/members — добавить участника (batch)
- [x] DELETE /chats/:id/members/:userId — удалить/покинуть
- [x] PATCH /chats/:id/members/:userId — изменить роль/permissions/custom_title
- [x] GET /chats/:id/members — список (пагинация) + поиск (?q=)
- [x] GET /chats/:id/members/:userId — инфо об участнике
- [x] PUT /chats/:id/permissions — default permissions группы
- [x] PUT /chats/:id/members/:userId/permissions — per-user permissions
- [x] POST /chats/:id/invite-link — генерация invite link
- [x] POST /chats/join/:inviteHash — вступить по ссылке
- [x] GET /chats/:id/admins — список админов
- [x] POST /chats/:id/slow-mode — включить slow mode (N секунд)
- [x] GET /chats/:id/invite-links — список invite links (bonus)
- [x] PUT /invite-links/:id — редактировать invite link (bonus)
- [x] DELETE /invite-links/:id — отозвать invite link (bonus)
- [x] GET /chats/invite/:hash — публичная инфо (без JWT) (bonus)
- [x] GET /chats/:id/join-requests — список заявок (bonus)
- [x] POST /chats/:id/join-requests/:userId/approve — одобрить (bonus)
- [x] POST /chats/:id/join-requests/:userId/reject — отклонить (bonus)
- [x] Backend тесты: 104 handler теста (bonus)

### WebSocket события (5 новых + 2 bonus)

- [x] `chat_created` — новый чат/канал
- [x] `chat_updated` — изменение настроек
- [x] `chat_member_added` — участник добавлен
- [x] `chat_member_removed` — участник удалён/вышел
- [x] `chat_member_updated` — роль/права изменены
- [x] `chat_deleted` — чат удалён (bonus)
- [x] `mention` — @mention уведомление (bonus)

### Database

**ALTER chat_members:**
- [x] ADD permissions BIGINT DEFAULT 0
- [x] ADD custom_title TEXT

**Permissions bitmask (pkg/permissions):**
- [x] can_send_messages, can_send_media, can_add_members, can_pin_messages
- [x] can_change_info, can_delete_messages, can_ban_users, can_invite_via_link

**ALTER chats:**
- [x] ADD default_permissions BIGINT DEFAULT 255
- [x] ADD slow_mode_seconds INT DEFAULT 0
- [x] ADD is_signatures BOOLEAN DEFAULT false

**chat_invite_links:**
- [x] id UUID PK, chat_id, creator_id, hash TEXT UNIQUE
- [x] expire_at, usage_limit, usage_count, requires_approval BOOLEAN
- [x] created_at

**chat_join_requests:**
- [x] chat_id + user_id PK, message TEXT, status, reviewed_by, created_at

### @Mentions (Must)

- [x] Парсинг @username в тексте сообщения → entities массив (message_service.go)
- [x] Автокомплит @mention в ChatInput (TG Web A useMentionTooltip + groupChatMembers from withGlobal)
- [x] WS-уведомление на @mention (NATS orbit.user.*.mention → gateway → WS)
- [x] Push-уведомление на @mention — enriched mention NATS payload + gateway enqueueMentionPushDispatch (bypass mute)
- [x] Backend: хранение mention entities в message, notification при @mention

### Channels vs Groups

- [x] Groups: все пишут (по permissions), авторы видимы
- [x] Channels: только owner/admin пишет, автор = название канала (анонимно, если !is_signatures)
- [ ] Linked discussion group для канала → Nice to Have, deferred

### Frontend: Saturn API методы (~30)

- [x] createChannel, editChatTitle, editChatAbout (editChatDescription)
- [x] updateChatPhoto, deleteChatPhoto — реализованы в Saturn media.ts, экспортированы
- [x] addChatMembers, deleteChatMember, leaveChat
- [x] deleteChat (deleteChannel = same endpoint)
- [x] fetchMembers, searchMembers (member search for @mentions)
- [x] updateChatMemberBannedRights, updateChatAdmin
- [x] updateChatDefaultBannedRights
- [ ] toggleChatIsProtected → Phase 7 (E2E)
- [ ] toggleJoinToSend, toggleJoinRequest → covered by requires_approval on invite link
- [x] exportChatInviteLink, editExportedChatInvite, deleteExportedChatInvite, joinChat, fetchChatInviteInfo
- [x] toggleSlowMode
- [x] archiveChat, unarchiveChat, toggleChatPinned, setChatMuted (persisted via `PATCH /chats/:id/members/me`)
- [x] fetchChatInviteImporters, hideChatJoinRequest (join request management)
- [ ] fetchTopics, createTopic, editTopic, deleteTopic → Nice to Have, deferred to Phase 5
- [x] Frontend bug fix: isUserId для UUID — knownChatIds registry + cache restore
- [x] Frontend: MiddleHeader chat type check (group→GroupChatInfo)
- [x] Frontend: openChat fallback fetchFullChat для unknown UUID
- [x] Frontend: buildApiChat → chatTypeSuperGroup для групп
- [x] Frontend: bitmask decode для adminRights/bannedRights
- [x] Frontend: isCreator/adminRights на chat объекте из fetchFullChat

### Критерий "готово"

✅ Создать "MST Dev Team" → добавить 10 человек → назначить 2 admin → чат → pin → invite link → @mention → уведомление. Канал "MST Announcements" → owner пишет, 150 читают. Роли и права работают. Верифицировано Playwright E2E тестом с 3 пользователями.

---

## Phase 3: Media & Files

**Цель:** Фото, видео, файлы, голосовые, видео-заметки.
**Сервисы:** media (новый), messaging (расширить)

### Проработка (Шаг 0)

- [x] Прочитать `docs/TZ-PHASES-V2-DESIGN.md` секция Phase 3, `docs/TZ-ORBIT-MESSENGER.md` §11.3
- [x] Изучить Cloudflare R2 API — S3-совместимость, presigned URLs, multipart upload
- [x] Спроектировать media pipeline: upload → process (resize/thumbnail/waveform) → store → serve
- [x] Решить: обработка изображений на Go → disintegration/imaging (pure Go, без CGO)
- [x] Решить: chunked upload → свой протокол (init/chunk/complete), НЕ tus.io
- [x] Решить: media↔messages → JOIN table message_media (поддержка альбомов)
- [x] Продумать: TG Web A media flow → FormData multipart, XHR для progress
- [x] Решить: video streaming → presigned R2 URL (302 redirect), не проксируем
- [x] Решить: ClamAV → defer на Phase 7 (Security). 150 invite-only юзеров
- [x] Спроектировать cleanup policy → background goroutine каждые 6h, orphans >24h
- [x] Составить план реализации, консолидировать 10 вариантов, согласовать

### Backend: Media сервис (порт 8083) — Endpoints (8)

- [x] POST /media/upload — загрузка файла, возврат media_id + URLs
- [x] POST /media/upload/chunked/init — начать chunked upload (>10MB)
- [x] POST /media/upload/chunked/:uploadId — загрузить chunk
- [x] POST /media/upload/chunked/:uploadId/complete — завершить
- [x] GET /media/:id — presigned R2 redirect
- [x] GET /media/:id/thumbnail — thumbnail
- [x] DELETE /media/:id — удалить из R2
- [x] GET /media/:id/info — метаданные (size, type, dimensions, duration)

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

- [x] `media_upload_progress` — прогресс загрузки больших файлов
- [x] `media_ready` — thumbnail/resize готов

### Database

**media:**
- [x] id UUID PK, uploader_id, type, mime_type, original_filename
- [x] size_bytes BIGINT, r2_key TEXT, thumbnail_r2_key TEXT, medium_r2_key TEXT
- [x] width INT, height INT, duration_seconds FLOAT
- [x] waveform_data BYTEA, is_one_time BOOLEAN DEFAULT false, processing_status TEXT
- [x] created_at

**message_media:**
- [x] message_id + media_id PK, position INT (порядок в альбоме), is_spoiler BOOLEAN

### Frontend: Saturn API методы (~12 реализовано)

- [x] uploadMedia (XHR с progress), initChunkedUpload, uploadChunk, completeChunkedUpload
- [x] fetchMediaInfo, deleteMedia, fetchSharedMedia
- [x] sendMessage с media_ids (расширен)
- [x] updateChatPhoto, deleteChatPhoto
- [x] buildApiMessage → media_attachments маппинг (photo/video/voice/document/gif)
- [x] WS events: media_ready, media_upload_progress (handler stubs)
- [x] cancelMediaUpload — real XHR/fetch abort + chunked server abort
- [~] sendVoice, sendVideoNote, sendDocument, sendPhoto, sendVideo — convenience wrappers (deferred to UI wiring)

### Фичи

- [x] Drag & Drop в чат — TG Web A DropArea (2 зоны: quick/generic) + AttachmentModal + Saturn sendMediaMessage wiring работает
- [x] Clipboard paste (Ctrl+V для скриншотов) — TG Web A встроенный handler, Saturn wiring через тот же AttachmentModal
- [x] Preview перед отправкой + caption — AttachmentModal с превью, caption, send → работает
- [x] One-time media (self-destruct) — toggle в AttachmentModal, blur preview + one-time modal для photo/video, Saturn wiring и viewed placeholder готовы
- [x] Media spoiler (blur до клика) — MediaSpoiler компонент + is_spoiler в Saturn wire format + send/receive path работает
- [x] Albums (несколько фото в одном сообщении) — grouped_id маппится в groupedId/isInAlbum и рендерится через Album
- [x] Media gallery tab в чате — fetchSharedMedia Saturn wiring + backend GET /chats/:id/media + Profile tab UI работает
- [~] GIF: Tenor API прокси — defer to Phase 5 (Rich Messaging)
- [x] PDF preview — first-page canvas render in chat + page count + dedicated inline viewer

### Известные отложения и решения

- **Thumbnails в JPEG** (не WebP) — Go не имеет стабильного WebP encoder. JPEG для thumb/medium
- **ClamAV** → Phase 7 (Security). Invite-only, 150 юзеров
- **GIF Tenor API** → Phase 5 (Rich Messaging). GIF→MP4 конвертация работает
- **PDF preview** → Done — первая страница PDF рендерится в canvas прямо в сообщении, показываются filename/size/page count, полный просмотр через inline browser viewer с fallback в новую вкладку
- **Frontend UI wiring** — TG Web A компоненты (AttachMenu, MediaViewer, VoiceRecorder) существуют, нужна интеграция с Saturn API. Backend полностью готов
- **Chunked upload state** — Redis с TTL 24h (не DB), автоочистка

### Критерий "готово"

Drag фото → thumbnail → полное по клику → gallery swipe. Файл → прогресс → скачивание. Голосовое → waveform. Video note круглое. PDF preview. GIF search.

---

## Phase 4: Search, Notifications & Settings

**Цель:** Найти любое сообщение за секунды. Не пропустить ничего. Настроить под себя.
**Сервисы:** gateway (push), messaging (search), Meilisearch

### Проработка (Шаг 0)

- [x] Прочитать `docs/TZ-PHASES-V2-DESIGN.md` секция Phase 4, `docs/TZ-ORBIT-MESSENGER.md` §11.4
- [x] Изучить Meilisearch API — индексация, фильтры, ranking rules, typo tolerance
- [x] Спроектировать: какие данные индексировать? Как синхронизировать с PostgreSQL? (trigger vs CDC vs cron) — Event-driven через NATS
- [x] Спроектировать Web Push: VAPID key generation, subscription storage, payload format
- [x] Продумать: delivery logic (online → WS → push → mute check → DND check → @mention override)
- [x] Продумать: как TG Web A обрабатывает push? Service Worker? Notification API?
- [x] Продумать: Phase 7 E2E сломает серверный поиск — нужна ли архитектура, готовая к этому? — Да, client-side search as fallback
- [x] Оценить: FCM/APNs — нужны сейчас (для PWA) или только для native apps? — VAPID сейчас, FCM/APNs при native
- [x] Спроектировать settings sync — серверное хранение vs localStorage — серверное через REST API
- [x] Составить порядок реализации и предложить пользователю

### Backend: Search (через Meilisearch)

- [x] GET /search?q=&scope=messages — глобальный поиск сообщений
- [x] GET /search?q=&scope=users — поиск юзеров
- [x] GET /search?q=&scope=chats — поиск чатов
- [~] GET /search?q=&scope=media — поиск медиа (по caption + filename) — покрывается scope=messages с фильтром has_media=true
- [x] Фильтры: chat_id, from_user_id, date_from, date_to, type, has_media

### Backend: Notifications (4 endpoints)

- [x] POST /push/subscribe — регистрация push-подписки
- [x] DELETE /push/subscribe — отписка
- [x] GET/PUT /users/me/settings/notifications — глобальные настройки уведомлений per chat type (migration 031, backend + Saturn wiring)
- [x] PUT /chats/:id/notifications — per-chat (mute, sound)
- [x] GET /chats/:id/notifications — получить настройки
- [x] DELETE /chats/:id/notifications — сбросить (unmute)

**Delivery logic:** online → WS in-app → нет → Web Push (VAPID) / FCM / APNs → muted → skip (кроме @mention) → DND → skip
- [x] Push dispatcher в gateway — VAPID delivery, offline dispatch, mute check, stale subscription cleanup

### Backend: Settings (12 endpoints)

- [x] GET /users/me/settings/privacy — настройки приватности
- [x] PUT /users/me/settings/privacy — обновить
- [x] GET /users/me/settings/appearance — тема, язык, шрифт
- [x] PUT /users/me/settings/appearance — обновить
- [~] PUT /users/me/username — сменить @username — уже есть в PUT /users/me (Phase 1)
- [~] PUT /users/me/avatar — уже есть в PUT /users/me (Phase 1)
- [~] DELETE /users/me/avatar — deferred, через PUT /users/me с avatar_url=null
- [x] GET /users/me/blocked — список заблокированных
- [x] POST /users/me/blocked/:userId — заблокировать
- [x] DELETE /users/me/blocked/:userId — разблокировать

### Database (5 новых таблиц)

**push_subscriptions:**
- [x] id, user_id, endpoint TEXT, p256dh TEXT, auth TEXT, user_agent TEXT, created_at

**notification_settings:**
- [x] user_id + chat_id PK, muted_until, sound TEXT DEFAULT 'default', show_preview BOOLEAN DEFAULT true

**privacy_settings:**
- [x] user_id PK, last_seen DEFAULT 'everyone', avatar DEFAULT 'everyone'
- [x] phone DEFAULT 'contacts', calls DEFAULT 'everyone'
- [x] groups DEFAULT 'everyone', forwarded DEFAULT 'everyone'

**blocked_users:**
- [x] user_id + blocked_user_id PK, created_at, CHECK constraint (user_id != blocked_user_id)

**user_settings:**
- [x] user_id PK, theme DEFAULT 'auto', language DEFAULT 'ru'
- [x] font_size INT DEFAULT 16, send_by_enter BOOLEAN DEFAULT true
- [x] dnd_from TIME, dnd_until TIME, updated_at

### Meilisearch Integration

- [x] Go client integration (meilisearch-go v0.36)
- [x] Index configuration: messages (content), users (display_name, email), chats (name, description)
- [x] NATS-based indexer: listens to message.new/updated/deleted events
- [x] ACL enforcement: search results filtered by user's chat membership
- [x] Soft-delete handling: deleted messages removed from index

### Security Fixes

- [x] Docker ports bound to 127.0.0.1 (postgres, redis, nats, meilisearch, minio, internal services)
- [x] NATS authentication via --auth token
- [x] Block check in SendMessage for DM chats (prevents blocked user messaging)
- [x] Search ACL: user can only search messages in their own chats

### Дополнительные фичи

- [x] In-app notification banners (сообщение в другом чате → баннер сверху)
- [x] In-chat search (лупа → фильтры: from user, date, type) — frontend Saturn wiring for from user + date completed
- [x] Web Push UX on frontend — permission prompt on first entry / Settings, VAPID subscribe-unsubscribe wiring, notification click fallback to chat when `message_id` is missing

### Frontend: Saturn API методы (~25)

**Search:**
- [x] searchMessagesGlobal, searchUsersGlobal, searchChatsGlobal
- [x] Frontend: Meilisearch hits hydration (`message/user/chat` → `ApiMessage`/`ApiUser`/`ApiChat`) + global/middle search rendering
- [~] searchChatMessages — уже есть из Phase 1 (searchMessagesInChat)
- [x] searchHashtag — реализован через action layer (isHashtag flag → performMiddleSearch)
- [x] fetchSearchHistory — migration 032, GET/POST/DELETE /search/history, Saturn methods, реализовано
- [x] getMessageByDate (findFirstMessageIdAfterDate) — Saturn method → GET /chats/:id/history?date=&limit=1, calendar UI уже работает

**Notifications:**
- [x] subscribePush (registerDevice), unsubscribePush
- [x] getChatNotifySettings, updateChatNotifySettings, deleteChatNotifySettings
- [x] updateGlobalNotifySettings — migration 031, GET/PUT /users/me/settings/notifications, Saturn wiring заменил localStorage

**Settings:**
- [x] getPrivacySettings, setPrivacySettings, fetchBlockedUsers, blockUser, unblockUser (+ in-flight dedup для privacy requests)
- [x] getUserSettings, updateUserSettings
- [~] fetchActiveSessions, terminateSession — уже есть в auth service
- [~] updateUsername, checkUsername — через updateProfile
- [~] updateProfilePhoto, deleteProfilePhoto — через updateProfile + media upload

### Критерий "готово"

Поиск "отчёт" → найти сообщение за февраль → клик → прокрутка. Web Push в фоне. Mute группы. Dark theme. Скрыть last seen. Font size 18px.

---

## Phase 5: Rich Messaging (текущая)

**Цель:** Реакции, стикеры, GIF, опросы, scheduled messages. Всё бесплатно.
**Сервисы:** messaging (расширить), gateway (WS)

### Прогресс реализации

- [x] Шаг 1 (Reactions backend): `ReactionStore` + `ReactionService` реализованы, `reaction_added` / `reaction_removed` публикуются в NATS, таргетные reaction tests PASS
- [x] Шаг 3 (Stickers backend): `StickerStore` + `StickerService` реализованы, таргетные sticker handler/service tests PASS, runtime wiring в `services/messaging/cmd/main.go` подключён
- [x] Шаг 4 (GIF backend): `GIFStore` + `GIFService` + `internal/tenor/client.go` реализованы, таргетные GIF handler/service tests PASS, runtime wiring в `services/messaging/cmd/main.go` подключён
- [x] Шаг 5 (Sticker import + seed): admin CRUD для sticker packs, idempotent seed CLI `scripts/seed-stickers`, deploy migration с 3 SVG pack'ами и R2-backed import
- [x] Шаг 6 (Runtime wiring в messaging): DI в `services/messaging/cmd/main.go` подключён для reactions/stickers/GIF/polls/scheduled, Phase 5 HTTP routes зарегистрированы, `POST /chats/:id/messages` поддерживает `type=poll` и `?scheduled_at=`
- [x] Шаг 7 (Frontend wiring + poll hydration + smoke): Saturn API методы и TG Web A wiring закрыты для reactions/stickers/GIF/polls/scheduled, обычная history hydration для polls/reactions доведена, WS `reaction_added` / `reaction_removed` / `poll_vote` / `poll_closed` применяются без reload, live Playwright smoke PASS
- [x] Шаг 8 (Frontend polish follow-up): локальные animated reaction assets подключены в Saturn fallback, composer GIF tab получил trending + inline search, saved reaction tags получили localStorage fallback
- [x] Шаг 8.2 (Search polish): middle search получил фильтры по типу сообщения, дате и отправителю; `/search` принимает alias-параметры `from/after/before` и типы `links/files`; клик по `#hashtag` открывает чистый поиск по тегу
- [x] Шаг 8.1 (Reaction emoji parity): picker / reaction bubbles / selector используют Apple-style emoji-data-ios assets для static reaction render, Unicode fallback остаётся только для неподдержанных glyphs
- [x] Шаг 7.1 (Poll parity + quiz explanation): poll UI приведён к TG Web A 1-в-1 (radio/checkbox до голосования, оригинальные spacing/result lines/button), quiz explanation/solution прокинут end-to-end через Saturn + messaging backend
- [x] Шаг 8.3 (Manual QA hardening): убран неработающий `Checklist` из Orbit attach menu до появления backend/API поддержки, добавлен regression test на `canAttachToDoLists: false`
- [x] Шаг 8.4 (Manual QA poll regression): `sendPollVote` и WS `poll_vote` больше не дублируют локальный инкремент поверх серверного poll state, добавлен regression test на корректные `100%`/`50%`
- [x] Шаг 8.1 (Push stabilization): gateway Web Push теперь отправляет payload с `sequence_number` всем подписанным устройствам получателей кроме отправителя, Service Worker понимает gateway payload и корректно открывает чат/сообщение, in-app banners снова показываются на активной вкладке вне текущего чата
- [x] Шаг 7.2 (Sticker render parity): preview/full sticker asset chain больше не путает `tgs/webm` с image-preview, sticker message attachments сохраняют `thumbnail_url`, picker covers и fallback-рендер используют `StickerView` для animated/video/static наборов
- [x] Шаг 9 (Security audit): privacy settings enforcement в `GET /users/:id`, fix sender_id leak в anonymous channel NATS events, fix `media_ready` WS handler bug, ILIKE wildcard escaping, DB indexes (sender_id, reply_to_id), length constraints (polls.solution, sticker_packs.short_name), `updated_at` на chat_invite_links, WS reconnect → full sync, SettingsMain refactored to use callApi, NATS URL redacted from logs
- [x] Шаг 10 (Reaction/Sticker stabilization):
  - Fix: reaction emoji mapping — ReactionPicker всегда отправляет `{ type: 'emoji', emoticon }`, убран broken custom emoji path через customEmojis.byId
  - Fix: sticker emoji backfill — media_attachments path теперь подхватывает emoji из richContent JSON
  - Fix: MinIO URL leak prevention — `StickerPack.FillPreviewURLs()`, `toGatewayMediaURL` safety net, frontend `toAbsoluteUrl` блокирует MinIO/localhost URLs
  - Fix: UUID-as-emoji validation — бэкенд отклоняет UUID в AddReaction, фронтенд валидирует в ReactionPicker и ReactionStaticEmoji
  - Fix: pointer-events overlay chain (5 слоёв) — CustomEmojiEffect, ReactionAnimatedEmoji, .quick-reaction, ReactionSelectorReaction (.AnimatedSticker + .staticIcon), ReactionPicker (display:none when !isOpen)
  - Feat: 126 TGS реакционных анимаций из 6 Telegram стикер-сетов (center/around/appear/select/effect/activate)
  - Fix: quick-bar всегда static PNG (noAppearAnimation), overflow:hidden на .root
  - Fix: fetchMessageReactions refresh после sendReaction для предотвращения state desync

### Проработка (Шаг 0)

- [x] Прочитать `docs/TZ-PHASES-V2-DESIGN.md` секция Phase 5, `docs/TZ-ORBIT-MESSENGER.md` §11.5
- [x] Изучить TG Web A sticker rendering — TGS (Lottie), WebP, WebM. Какие библиотеки (rlottie)?
- [ ] Изучить Tenor API — rate limits, API key, response format, caching strategy
- [ ] Спроектировать sticker import из Telegram — как работает TG Bot API fetchStickerSet? Легальность?
- [x] Спроектировать scheduled messages — Go cron job (interval?), timezone handling, delivery guarantee
- [x] Продумать: isPremium removal — grep все проверки в TG Web A, составить список файлов для изменения
- [x] Продумать: reactions animation — CSS keyframes (pop-in, bounce, count-bump) + staggered appear в пикере, без серверных изменений
- [x] Продумать: polls real-time — WS broadcast при каждом голосе или батчинг? → per-vote WS (`poll_vote`) + отдельный `poll_closed`
- [x] Оценить: custom emoji — нужен свой рендер или TG Web A уже умеет? → TG Web A rendering reused, Saturn wiring + no-premium gating applied
- [x] Составить порядок реализации и предложить пользователю

### Backend: Endpoints

**Реакции:**
- [x] POST /messages/:id/reactions — добавить
- [x] DELETE /messages/:id/reactions — удалить
- [x] GET /messages/:id/reactions — список
- [x] GET /messages/:id/reactions/users — список реакторов с optional emoji filter + pagination
- [x] PUT /chats/:id/available-reactions — настроить доступные

**Стикеры:**
- [x] GET /stickers/featured — рекомендуемые паки
- [x] GET /stickers/search?q= — поиск
- [x] GET /stickers/sets/:id — получить пак
- [x] POST /stickers/sets/:id/install — установить
- [x] DELETE /stickers/sets/:id/install — удалить
- [x] GET /stickers/installed — мои паки
- [x] GET /stickers/recent — недавние

**GIF:**
- [x] GET /gifs/search?q= — поиск (Tenor прокси)
- [x] GET /gifs/trending — трендовые
- [x] GET /gifs/saved — сохранённые
- [x] POST /gifs/saved — сохранить
- [x] DELETE /gifs/saved/:id — удалить

**Опросы:**
- [x] Poll backend core (`PollStore` + `PollService`) — реализовано, service/handler tests PASS, route registration и `POST /chats/:id/messages (type=poll)` integration подключены в runtime
- [x] POST /chats/:id/messages (type=poll) — создать
- [x] POST /messages/:id/poll/vote — проголосовать
- [x] DELETE /messages/:id/poll/vote — отозвать голос
- [x] POST /messages/:id/poll/close — закрыть

**Scheduled:**
- [x] POST /chats/:id/messages?scheduled_at= — запланировать
- [x] GET /chats/:id/messages/scheduled — список запланированных
- [x] PATCH /messages/:id/scheduled — изменить время/текст
- [x] DELETE /messages/:id/scheduled — удалить
- [x] POST /messages/:id/scheduled/send-now — отправить сейчас

### WebSocket события (4 новых)

- [x] `reaction_added` — gateway WS delivery подключён, fanout работает через `member_ids` и fallback fetch member IDs, таргетные gateway WS tests PASS
- [x] `reaction_removed` — gateway WS delivery подключён, fanout работает через `member_ids` и fallback fetch member IDs, таргетные gateway WS tests PASS
- [x] `poll_vote` — gateway WS delivery подключён, fanout работает через `member_ids` и fallback fetch member IDs, таргетные gateway WS tests PASS
- [x] `poll_closed` — gateway WS delivery подключён, fanout работает через `member_ids` и fallback fetch member IDs, таргетные gateway WS tests PASS

### Database (7 таблиц)

- [x] message_reactions: message_id + user_id + emoji PK, created_at
- [x] chat_available_reactions: chat_id PK, mode DEFAULT 'all', allowed_emojis TEXT[]
- [x] sticker_packs: id, name, short_name, author_id, thumbnail_url, is_official, is_animated, sticker_count
- [x] stickers: id, pack_id, emoji, file_url, file_type, width, height, position
- [x] user_installed_stickers: user_id + pack_id PK, position, installed_at
- [x] recent_stickers: user_id + sticker_id PK, used_at
- [x] polls: id, message_id, question, is_anonymous, is_multiple, is_quiz, correct_option, close_at
- [x] poll_options: id, poll_id, text, position
- [x] poll_votes: poll_id + user_id + option_id PK, voted_at

### Frontend: Saturn API методы (~40)

**Реакции:**
- [x] sendReaction, fetchMessageReactionsList, fetchAvailableReactions, setDefaultReaction, setChatEnabledReactions

**Стикеры:**
- [x] fetchStickerSets, fetchRecentStickers, fetchFavoriteStickers, fetchFeaturedStickers
- [x] searchStickers, installStickerSet, uninstallStickerSet
- [x] addRecentSticker, removeRecentSticker, addFavoriteSticker, removeFavoriteSticker
- [x] fetchCustomEmoji, fetchCustomEmojiSets

**GIF:**
- [x] fetchGifs, searchGifs, fetchSavedGifs, saveGif, removeGif

**Polls:**
- [x] sendPoll, votePoll, closePoll, fetchPollVoters

**Scheduled:**
- [x] fetchScheduledHistory, sendScheduledMessages, editScheduledMessage, deleteScheduledMessages, rescheduleMessage — Saturn wiring закрыт для text/reply/media/poll flows; recurring repeat UI intentionally disabled until backend support

**Other:**
- [x] fetchCommonChats — GET /users/:id/common-chats, Saturn wiring + sendApiUpdate для чатов в Profile
- [x] fetchSavedMessages — GET /users/me/saved-chat (lazy creation), migration 033, Saturn fetchSavedChats
- [ ] toggleSavedDialogPinned — Saturn wiring не подключён (только GramJS path)

### No Premium — всё бесплатно

- [x] Phase 5 critical premium gates в TG Web A routed through `selectIsCurrentUserPremium` / `selectCurrentLimit`; unrelated premium payments/gifts flows intentionally left intact
- [x] Custom emoji в имени/статусе — бесплатно
- [x] Animated emoji — бесплатно
- [x] Все стикер-паки — бесплатно
- [x] Saturn custom emoji document lookup + frontend render guards for added emoji picker/composer
- [x] Extended upload limits — бесплатно
- [x] Emoji status — бесплатно

### Критерий "готово"

Long-press → реакция → анимация. Стикер-пикер → установить пак → отправить. GIF search → send. Опрос → голосование real-time. Schedule message на завтра 9:00. Custom emoji в статусе.

---

## Phase 6: Voice & Video Calls

**Цель:** 1-на-1 и групповые звонки с шарингом экрана.
**Сервисы:** calls (новый, порт 8084), gateway (WS signaling)
**Инфра:** Pion SFU на Saturn.ac, coturn на Hetzner VPS

### Проработка (Шаг 0)

- [x] Прочитать `docs/TZ-PHASES-V2-DESIGN.md` секция Phase 6, `docs/TZ-ORBIT-MESSENGER.md` §11.6
- [ ] Изучить Pion WebRTC Go library — SFU vs MCU, room management, codec support
- [x] Изучить coturn — конфигурация, REST API для credential rotation, HA (single point of failure!)
- [x] Изучить TG Web A calls UI — какие WebRTC events ожидает? Какой signaling protocol?
- [x] Спроектировать: WS signaling flow (offer → answer → ICE → connected) через gateway
- [x] Спроектировать: как gateway маршрутизирует signaling events к calls сервису?
- [x] Продумать: P2P vs SFU routing — автоматический выбор (2 участника → P2P, >2 → SFU)
- [ ] Продумать: screen sharing — getDisplayMedia API, как мультиплексировать с камерой?
- [ ] Продумать: push для звонков — высокоприоритетный push когда app закрыт
- [x] Оценить: coturn HA — нужен fallback сервер или достаточно одного для 150 юзеров?
- [x] Составить порядок реализации и предложить пользователю

### Architecture

```
P2P:  browser ←→ browser (1-на-1, direct WebRTC)
TURN: coturn на Hetzner (relay при корпоративном NAT)
SFU:  Pion (группа до 50 — каждый шлёт 1 поток, SFU раздаёт)
Signaling: WebSocket через gateway
```

### Backend: Endpoints (12)

- [x] POST /calls — инициировать звонок
- [x] PUT /calls/:id/accept — принять
- [x] PUT /calls/:id/decline — отклонить
- [x] PUT /calls/:id/end — завершить
- [x] GET /calls/:id — статус звонка
- [x] GET /calls/history — история звонков
- [x] POST /calls/:id/participants — добавить участника (group)
- [x] DELETE /calls/:id/participants/:userId — удалить участника
- [x] PUT /calls/:id/mute — mute/unmute
- [x] PUT /calls/:id/screen-share/start — начать шаринг
- [x] PUT /calls/:id/screen-share/stop — остановить шаринг
- [x] GET /calls/:id/ice-servers — получить TURN/STUN credentials

### WebSocket Signaling (11 событий)

**Server → Client:**
- [x] `call_incoming` — входящий звонок (ringtone)
- [x] `call_accepted` — собеседник принял
- [x] `call_declined` — собеседник отклонил
- [x] `call_ended` — звонок завершён
- [x] `call_participant_joined` — присоединился к групповому
- [x] `call_participant_left` — покинул групповой

**Bidirectional:**
- [x] `webrtc_offer` — SDP offer
- [x] `webrtc_answer` — SDP answer
- [x] `webrtc_ice_candidate` — ICE candidate
- [x] `call_muted` / `call_unmuted` — статус микрофона
- [x] `screen_share_started` / `screen_share_stopped`

### Database (2 таблицы)

**calls:**
- [x] id, type (voice/video), mode (p2p/group), chat_id, initiator_id
- [x] status (ringing/active/ended/missed/declined)
- [x] started_at, ended_at, duration_seconds, created_at

**call_participants:**
- [x] call_id + user_id PK, joined_at, left_at
- [x] is_muted, is_camera_off, is_screen_sharing

### Frontend: Saturn API методы (~20)

- [x] createCall, acceptCall, declineCall, hangUp
- [~] joinGroupCall, leaveGroupCall — stubs, full implementation requires Pion SFU
- [x] toggleCallMute, toggleCallCamera
- [x] startScreenShare, stopScreenShare
- [~] fetchCallParticipants, fetchCallHistory, rateCall — history done, rateCall stub
- [x] sendWebRtcOffer, sendWebRtcAnswer, sendIceCandidate, fetchIceServers
- [~] inviteToCall, setCallSpeaker — deferred to Pion SFU integration

### Frontend: WebRTC P2P Wiring

- [x] ICE server fetch + ApiPhoneCallConnection conversion (requestCall, acceptCall)
- [x] sendSignalingData → WS transport (InitialSetup→offer/answer, Candidates→ice_candidate, MediaState)
- [x] updateWebRTCSignaling handler → processSignalingMessage (incoming signaling from WS)
- [x] Saturn call path bypasses DH crypto (verifyPhoneCallProtocol, confirmPhoneCall)
- [x] wsHandler: handleCallIncoming/Accepted set activeCallId/PeerId correctly
- [x] ApiUpdateWebRTCSignaling type added to ApiUpdate union

### Дополнительные фичи

- [x] Ringtone + vibration на входящий — ringtone через TG Web A, vibration через navigator.vibrate (Stage 1)
- [ ] Push-уведомление на звонок когда app закрыт — high-priority push (Stage 4)
- [ ] Network quality indicator — Stage 5
- [ ] Call rating после завершения — Stage 5

### Критерий "готово"

Кнопка телефона → ringtone → принять → голос P2P. Видео → камера. Группа "Начать звонок" → 10 участников → video grid → screen share. Call history в профиле.

### Stage-by-stage completion tracker

Полный план доработки: `docs/calls-plan.md`. Разбит на 5 этапов для выполнения в отдельных чатах.

#### Stage 1: P2P Stabilization ✅ (commit <будет заполнен после commit>)

**Backend:**
- [x] `TURN_PUBLIC_URL` env var — публичный URL coturn для браузера (docker-compose + calls/main.go)
- [x] Warning log если TURN_PUBLIC_URL есть без credentials / пустой
- [x] Propagate participant insert errors с rollback call (`CreateCall`, `AcceptCall`)
- [x] `AddParticipant` / `CreateCall` / `AcceptCall` — проверка `chat_members` (IDOR fix)
- [x] Auto-expire ringing calls после 60s через background worker (`ExpireRingingCalls`)
- [x] `Delete`, `IsUserInChat`, `ExpireRinging` methods в CallStore interface

**Frontend:**
- [x] Статический импорт `setActiveCallId/setActiveCallPeerId` в wsHandler.ts (убран dynamic require)
- [x] `requestCall` — убран fallback `chatId = user.id`, логирует ошибку если chatId отсутствует
- [x] `p2p.ts`: ICE candidate timeout 15s если InitialSetup не пришёл
- [x] `p2p.ts`: `createOffer` обёрнут в try-catch, discard call на ошибку
- [x] `p2p.ts`: JSON.parse data channel messages обёрнут в try-catch
- [x] `p2p.ts`: cleanup `acquiredStream` в catch если getUserMedia/replaceTrack упал
- [x] `p2p.ts`: `stopPhoneCall` — защищённый `close()` + clear pending timer
- [x] `wsHandler.ts`: `navigator.vibrate([300,200,300,200,300])` при incoming call
- [x] `calls.async.ts`: `updatePhoneCallConnectionState` — не hangup на `disconnected` (даёт шанс ICE restart), только на `closed`/`failed`

#### Stage 2: Media state sync ⏳
#### Stage 3: Pion SFU (группы) ⏳
#### Stage 4: Push для закрытого app ⏳
#### Stage 5: Polish (quality indicator + rating) ⏳

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
