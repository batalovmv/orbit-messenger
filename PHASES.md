# Orbit Messenger — План разработки

Каждая фаза = рабочий релиз. Код пишется пофазно в отдельных чатах.

Полное ТЗ: `docs/TZ-ORBIT-MESSENGER.md`
Детали фаз: `docs/TZ-PHASES-V2-DESIGN.md`
Killer-фичи: `docs/TZ-KILLER-FEATURES.md`

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

**Цель:** Люди могут переписываться в личке. Надёжно. С reply/forward/edit.
**Сервисы:** gateway, auth, messaging
**Фронтенд:** Форк TG Web A + Saturn API layer

### Backend: Auth сервис (порт 8081)

- [ ] POST /auth/register — регистрация по инвайту
- [ ] POST /auth/login — логин email + password + optional 2FA
- [ ] POST /auth/logout — выход + blacklist токена
- [ ] POST /auth/refresh — ротация refresh-токена
- [ ] GET /auth/me — валидация сессии
- [ ] GET /auth/sessions — список сессий
- [ ] DELETE /auth/sessions/:id — отзыв сессии (проверка принадлежности!)
- [ ] POST /auth/2fa/setup — настройка TOTP
- [ ] POST /auth/2fa/verify — подтверждение 2FA
- [ ] POST /auth/invite/validate — проверка инвайт-кода
- [ ] POST /auth/invites — создание инвайтов (admin)
- [ ] GET /auth/invites — список инвайтов (admin)
- [ ] DELETE /auth/invites/:id — отзыв инвайта
- [ ] POST /auth/bootstrap — первый admin-аккаунт

### Backend: Gateway (порт 8080)

- [ ] Прокси /auth/* → auth сервис
- [ ] JWT middleware (валидация через auth сервис с кэшированием)
- [ ] CORS middleware (конкретные origins, НЕ wildcard)
- [ ] Rate limiting middleware (Redis-backed)
- [ ] Health check GET /health

### Backend: Messaging (порт 8082) — через Gateway

- [ ] GET /chats — список чатов (JOIN, без N+1!)
- [ ] POST /chats/direct — создание/получение DM
- [ ] GET /chats/:id — информация о чате
- [ ] GET /chats/:id/messages — история сообщений (пагинация)
- [ ] POST /chats/:id/messages — отправка (text, replyTo, forwardFrom)
- [ ] PATCH /messages/:id — редактирование (только автор)
- [ ] DELETE /messages/:id — soft-delete (автор или admin)
- [ ] POST /messages/:id/forward — пересылка
- [ ] POST /chats/:id/pin/:messageId — закрепить
- [ ] DELETE /chats/:id/pin/:messageId — открепить
- [ ] PATCH /chats/:id/read — отметить прочитанным
- [ ] GET /users/me — текущий юзер
- [ ] PUT /users/me — обновить профиль
- [ ] GET /users/:id — профиль юзера
- [ ] GET /users?q= — поиск юзеров

### WebSocket (через Gateway)

- [ ] new_message — broadcast всем членам чата
- [ ] message_updated — при редактировании
- [ ] message_deleted — при удалении
- [ ] typing — с debounce и stop-событием
- [ ] user_status — online (broadcast контактам) + offline (при disconnect)
- [ ] messages_read — при прочтении

### Database (миграции)

- [ ] users — id, email, password_hash, display_name, avatar_url, role, status, 2FA, last_seen
- [ ] sessions — id, user_id, token_hash, ip, user_agent, expires_at
- [ ] invites — id, code, created_by, email, role, max_uses, use_count, expires_at
- [ ] chats — id, type, name, description, avatar_url, created_by, is_encrypted
- [ ] chat_members — chat_id, user_id, role, last_read_message_id
- [ ] direct_chat_lookup — user1_id, user2_id, chat_id (user1 < user2)
- [ ] messages — id, chat_id, sender_id, type, content, reply_to_id, is_edited, is_deleted, is_pinned, sequence_number

### Frontend: Saturn API методы (~35)

- [ ] checkAuth, validateInviteCode, registerWithInvite, loginWithEmail, logout
- [ ] fetchCurrentUser, fetchUser, searchUsers, updateProfile
- [ ] fetchChats, fetchMessages, sendMessage, editMessage, deleteMessages
- [ ] forwardMessages, pinMessage, unpinMessage
- [ ] markMessageListRead, createDirectChat, fetchFullChat
- [ ] WebSocket: connectWebSocket, sendTypingAction, pingWebSocket

### Критерий "готово"

Логин → список чатов → открыть DM → отправить сообщение → видеть typing → видеть ✓✓ → reply → edit → forward → pin. Всё real-time через WebSocket.

---

## Phase 2: Groups & Channels

**Цель:** Рабочие группы и каналы объявлений.
**Сервисы:** messaging (расширить), gateway (WS)

- [ ] Создание группы (имя + аватар + участники)
- [ ] Создание канала (broadcast-only)
- [ ] Роли: Owner > Admin > Member > Banned
- [ ] Permissions bitmask (send, media, pin, delete, ban, invite)
- [ ] @mention с автокомплитом и уведомлением
- [ ] Invite links (публичные, приватные, с лимитом)
- [ ] Добавление / удаление / бан участников
- [ ] Slow mode (1 сообщение в N секунд)
- [ ] WS: chat_created, chat_updated, member_added/removed
- [ ] ~30 Saturn API методов
- [ ] DB: chat_invite_links, chat_join_requests + ALTER chat_members

### Критерий "готово"

Создать "MST Dev Team" → добавить 10 человек → назначить 2 admin → чат → pin → invite link. Канал "MST Announcements" → owner пишет, 150 читают.

---

## Phase 3: Media & Files

**Цель:** Фото, видео, файлы, голосовые, видео-заметки.
**Сервисы:** media (новый), messaging (расширить)

- [ ] Media сервис: upload/download через Cloudflare R2
- [ ] Chunked upload для файлов >10MB (до 2GB)
- [ ] Фото: resize (320px thumb + 800px medium + original), WebP, strip EXIF
- [ ] Видео: thumbnail из первого кадра, streaming через presigned URL
- [ ] Голосовые: waveform peaks, OGG
- [ ] Video notes: circular 384px, 60 сек max
- [ ] GIF: Tenor API прокси
- [ ] Drag & Drop, Clipboard paste (Ctrl+V)
- [ ] One-time media (self-destruct)
- [ ] Media spoiler (blur)
- [ ] Albums (несколько фото в одном сообщении)
- [ ] ~25 Saturn API методов
- [ ] DB: media, message_media

### Критерий "готово"

Drag фото → thumbnail → полное изображение по клику → gallery swipe. Файл → прогресс → скачивание. Голосовое → waveform. Video note круглое.

---

## Phase 4: Search, Notifications & Settings

**Цель:** Найти любое сообщение за секунды. Не пропустить ничего. Настроить под себя.
**Сервисы:** gateway (push), messaging (search), Meilisearch

- [ ] Meilisearch интеграция: messages, users, chats, media (by caption)
- [ ] Фильтры: chat_id, from_user, date_from/to, type, has_media
- [ ] Web Push (VAPID): browser закрыт → уведомление
- [ ] In-app уведомления через WebSocket
- [ ] Per-chat mute (1h / 8h / forever)
- [ ] DND расписание (тихие часы)
- [ ] @mention уведомление даже при mute
- [ ] Privacy: last seen, avatar, phone (everyone/contacts/nobody)
- [ ] Active sessions + remote terminate
- [ ] Тема: dark / light / auto
- [ ] Язык: RU / EN
- [ ] ~25 Saturn API методов
- [ ] DB: push_subscriptions, notification_settings, privacy_settings, blocked_users, user_settings

### Критерий "готово"

Поиск "отчёт" → найти сообщение за февраль → клик → прокрутка к нему. Закрыть вкладку → новое сообщение → Web Push → клик → открывается чат. Настройки: скрыть last seen, замьютить группу, тёмная тема.

---

## Phase 5: Rich Messaging

**Цель:** Реакции, стикеры, GIF, опросы, scheduled messages. Всё бесплатно (без Premium).
**Сервисы:** messaging (расширить), gateway (WS)

- [ ] Реакции (long-press → emoji → анимация)
- [ ] Стикер-паки: featured, search, install/uninstall
- [ ] Форматы: WebP (static), TGS/Lottie (animated), WebM (video)
- [ ] Импорт стикеров из Telegram (через TG Bot API)
- [ ] GIF: Tenor API прокси (search + trending + saved)
- [ ] Опросы: create, vote, retract, close, quiz mode
- [ ] Scheduled messages (отложенная отправка)
- [ ] Saved Messages (личный архив)
- [ ] Убрать все isPremium проверки в TG Web A → всё бесплатно
- [ ] ~40 Saturn API методов
- [ ] DB: message_reactions, chat_available_reactions, user_installed_stickers, polls, poll_options, poll_votes

### Критерий "готово"

Long-press → реакция → анимация. Стикер-пикер → установить пак → отправить. GIF поиск. Опрос "Куда на корпоратив?" → 4 варианта → голосование в реальном времени. Запланировать поздравление на 9:00 завтра.

---

## Phase 6: Voice & Video Calls

**Цель:** 1-на-1 и групповые звонки с шарингом экрана.
**Сервисы:** calls (новый), gateway (signaling через WS)

- [ ] Calls сервис: Pion SFU + coturn интеграция
- [ ] 1-на-1 voice (P2P через WebRTC)
- [ ] 1-на-1 video
- [ ] Group voice (до 50 участников, SFU)
- [ ] Group video (video grid + active speaker)
- [ ] Screen sharing
- [ ] Call history
- [ ] WS signaling: offer/answer/ICE/mute/screen_share
- [ ] ~20 Saturn API методов
- [ ] DB: calls, call_participants

### Критерий "готово"

Кнопка телефона → звонок → принять → голос P2P. Видео → камера. Группа "Начать звонок" → 10 участников → video grid → screen share.

---

## Phase 7: E2E Encryption

**Цель:** Zero-Knowledge — сервер не может прочитать DM. Криптографическая гарантия.
**Сервисы:** auth (key server), messaging (encrypt/decrypt)

- [ ] Signal Protocol: X3DH key exchange
- [ ] Double Ratchet: новый ключ после КАЖДОГО сообщения
- [ ] AES-256-GCM шифрование контента
- [ ] Sender Keys для групп
- [ ] Safety Numbers: QR + числовое сравнение
- [ ] Disappearing messages: 24h / 7d / 30d
- [ ] Key Transparency (публичный лог ключей)
- [ ] Шифрование медиа перед загрузкой в R2
- [ ] Key server endpoints (upload/fetch keys)
- [ ] ~15 Saturn API методов
- [ ] DB: user_keys, one_time_prekeys, key_transparency_log

### Критерий "готово"

Открыть DM → иконка замка "E2E encrypted". Отправить → сервер хранит только ciphertext. Safety Numbers → QR → "Verified". Admin смотрит в БД → видит blob.

---

## Phase 8: AI, Bots, Integrations & Production

**Цель:** Claude AI встроен, боты работают, MST-тулы подключены, мониторинг для 150+ юзеров.

### 8A: AI (ai сервис)

- [ ] Суммаризация чата (Claude API, SSE streaming)
- [ ] Перевод сообщений
- [ ] Подсказки ответов (3 варианта)
- [ ] Транскрипция голосовых (Whisper)
- [ ] Семантический поиск (embeddings)
- [ ] Rate limit: 20 AI-запросов/мин/юзер

### 8B: Bots (bots сервис)

- [ ] TG-совместимый Bot API (sendMessage, editMessage, etc.)
- [ ] Webhook delivery с retry
- [ ] /commands автокомплит
- [ ] Inline keyboards

### 8C: Integrations (integrations сервис)

- [ ] InsightFlow → #alerts канал
- [ ] Keitaro postbacks → уведомления
- [ ] Saturn.ac deploy status → #dev
- [ ] HR-бот миграция из Telegram
- [ ] Generic webhook система

### 8D: Production Hardening

- [ ] ScyllaDB для сообщений (миграция с PostgreSQL)
- [ ] NATS JetStream: гарантированная доставка WS-событий
- [ ] Prometheus + Grafana мониторинг
- [ ] OpenTelemetry distributed tracing
- [ ] Structured JSON logging (slog)
- [ ] OWASP security audit
- [ ] Перформанс: p99 <100ms доставка, p95 <200ms API, 500 WS/instance

### Критерий "готово"

AI sparkle → "Суммаризируй за час" → streaming ответ. Голосовое → транскрипция. HR-бот в Orbit. InsightFlow → "#alerts: Новая конверсия!". Grafana зелёная. 150 юзеров онлайн, сообщения <100ms.

---

## Параллельные треки

### Desktop (после Phase 4)

- [ ] Tauri 2.0 обёртка вокруг web/
- [ ] .dmg / .exe / .AppImage (~10-15MB)
- [ ] Deep links: `orbit://chat/{id}`
- [ ] Auto-update (Tauri updater)

### Mobile

- [ ] PWA (уже встроено в TG Web A — Service Worker + manifest)
- [ ] Нативные приложения — оценить необходимость после Phase 6

---

## Killer Features (после Phase 8)

Подробности: `docs/TZ-KILLER-FEATURES.md`

| # | Фича | Дни | Волна |
|---|------|-----|-------|
| 1 | Super Access (C-Level AI аналитика) | 27 | 3 |
| 2 | AI Meeting Notes | 17 | 2 |
| 3 | Smart Notifications | 10 | 2 |
| 4 | Workflow Automations | 15 | 2 |
| 5 | Knowledge Base | 12 | 3 |
| 6 | Live Translate | 8 | 2 |
| 7 | Video Notes Pro | 10 | 1 |
| 8 | Anonymous Feedback | 12 | 3 |
| 9 | Status Automations | 8 | 1 |
| 10 | Team Pulse | 15 | 3 |
| 11 | Orbit Spaces | 12 | 1 |

**Волна 1** (параллельно Phases 3-6): Video Notes Pro, Status Automations, Orbit Spaces
**Волна 2** (Phase 8): Live Translate, Smart Notifications, AI Meeting Notes, Workflow Automations
**Волна 3** (Phase 9+): Anonymous Feedback, Knowledge Base, Team Pulse, Super Access
