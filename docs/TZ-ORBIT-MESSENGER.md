# ТЕХНИЧЕСКОЕ ЗАДАНИЕ
# Orbit Messenger — Корпоративный мессенджер

**Версия:** 2.0
**Дата:** 2026-03-24 (создание) / 2026-04-16 (актуализация)
**Заказчик:** MST (150+ сотрудников)
**Разработка:** Внутренняя команда + AI-assisted development

---

## СОДЕРЖАНИЕ

1. [Введение и цели проекта](#1-введение-и-цели-проекта)
2. [Текущее состояние проекта](#2-текущее-состояние-проекта)
3. [Архитектура системы](#3-архитектура-системы)
4. [Технологический стек](#4-технологический-стек)
5. [Микросервисы](#5-микросервисы)
6. [База данных](#6-база-данных)
7. [API спецификация](#7-api-спецификация)
8. [WebSocket протокол](#8-websocket-протокол)
9. [Безопасность и шифрование](#9-безопасность-и-шифрование)
10. [At-rest Encryption](#10-at-rest-encryption)
11. [Полный функционал по модулям](#11-полный-функционал-по-модулям)
12. [Фазы разработки](#12-фазы-разработки)
13. [Инфраструктура и деплой](#13-инфраструктура-и-деплой)
14. [Платформы](#14-платформы)
15. [UI/UX требования](#15-uiux-требования)
16. [Производительность и масштабирование](#16-производительность-и-масштабирование)
17. [Мониторинг и observability](#17-мониторинг-и-observability)
18. [Конкурентный анализ](#18-конкурентный-анализ)
19. [Отличия от Telegram](#19-отличия-от-telegram)
20. [Приложения](#20-приложения)

---

## 1. Введение и цели проекта

### 1.1 Назначение

Orbit Messenger — корпоративный мессенджер для компании MST (150+ сотрудников), предназначенный для замены Telegram как основного инструмента рабочей коммуникации.

### 1.2 Ключевые цели

| # | Цель | Приоритет |
|---|------|-----------|
| 1 | Полный контроль данных — сервер принадлежит MST | Критично |
| 2 | At-rest шифрование + compliance доступ — защита данных при сохранении административного контроля | Критично |
| 3 | Telegram-like UX — знакомый интерфейс, минимальный порог входа | Критично |
| 4 | Интеграция с MST инструментами (InsightFlow, Keitaro, HR-бот) | Важно |
| 5 | AI-ассистент (Claude) встроен в мессенджер | Killer feature |
| 6 | Все фичи бесплатны для всех (no Premium paywall) | Принцип |
| 7 | Desktop + Mobile + Web на одной кодовой базе | Важно |

### 1.3 Целевая аудитория

- **Primary:** 150+ сотрудников MST (менеджеры, разработчики, маркетологи)
- **Secondary:** Масштабирование до 1000+ пользователей
- **Use cases:** Рабочие переписки, групповые обсуждения, звонки, обмен файлами, AI-помощь

### 1.4 Критерии успеха

- Стабильная работа 24/7 без потери данных
- Доставка сообщений p99 < 100ms
- 500+ одновременных WebSocket соединений
- Удобство на уровне Telegram (UX benchmark)
- 100% данных на собственном сервере

---

## 2. Текущее состояние проекта

### 2.1 Что реализовано

| Компонент | Статус | Детали |
|-----------|--------|--------|
| **Концепция и архитектура** | ✅ 100% | Актуализировано: compliance-first модель |
| **Auth сервис (Go)** | ✅ 100% | JWT + httpOnly cookies + 2FA TOTP + invite codes + sessions + RBAC (superadmin/compliance/admin/member) |
| **Gateway сервис (Go)** | ✅ 100% | HTTP proxy + WebSocket hub + rate limiting + NATS fan-out |
| **Frontend (TG Web A fork)** | ✅ 90% | Auth, chats, messages, media, reactions, stickers, GIF, polls, AI, bots, search, notifications, settings |
| **Saturn API layer** | ✅ ~200 | Реальные методы для всех реализованных фич |
| **WebSocket** | ✅ 100% | Real-time: messages, typing, status, reactions, members, read receipts |
| **Messaging сервис** | ✅ 100% | CRUD, groups, permissions, reactions, stickers, polls, scheduled messages, at-rest encryption |
| **Media сервис** | ✅ 100% | Upload/download, thumbnails, R2 pipeline, chunked upload |
| **AI сервис** | ✅ 100% | Claude SSE (summarize/translate/reply-suggest), Whisper transcribe, @orbit-ai bot |
| **Bots сервис** | ✅ 100% | Bot API, admin UI, inline keyboards, webhooks, BotFather |
| **Integrations сервис** | ✅ 100% | MST webhook framework + presets, Saturn.ac E2E |
| **Calls сервис** | 🔴 0% | Pending (Phase 6) |
| **Desktop (Tauri)** | 🔴 0% | Pending |
| **Mobile** | 🔴 0% | Не начато |
| **At-rest шифрование** | ✅ 100% | AES-256-GCM обёртка messages.content (Signal Protocol E2E — сознательно отклонена) |
| **Деплой (Saturn.ac)** | ✅ | Все 7 сервисов + frontend задеплоены |

### 2.2 Репозиторий

- **Хостинг:** Saturn.ac (private)
- **Prod URL:** см. `.env` / Saturn dashboard
- **Backend API:** см. `.env` / Saturn dashboard
- **Коммитов:** 20+ (6 сессий разработки)
- **Размер проекта:** ~229MB (без node_modules)

### 2.3 Команда

| Роль | Кто |
|------|-----|
| Архитектор / Product Owner | Alex (Александр) |
| CTO / Tech Lead | Claude AI (AI-assisted development) |
| Frontend | TG Web A fork (Teact framework) |
| Backend | Go 1.24 (Fiber v2) |

---

## 3. Архитектура системы

### 3.1 Общая архитектура

```
┌─────────────────────────────────────────────────────────┐
│                     КЛИЕНТЫ                              │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌───────────┐  │
│  │   Web   │  │ Desktop │  │  iOS    │  │  Android  │  │
│  │ (React) │  │ (Tauri) │  │  (RN)   │  │   (RN)    │  │
│  └────┬────┘  └────┬────┘  └────┬────┘  └─────┬─────┘  │
│       └────────────┼───────────┼──────────────┘         │
└────────────────────┼───────────┼────────────────────────┘
                     │           │
              HTTPS/WSS + JWT
                     │           │
┌────────────────────┼───────────┼────────────────────────┐
│              ┌─────▼───────────▼─────┐                   │
│              │      GATEWAY          │                   │
│              │   (API + WebSocket)   │                   │
│              └──┬──┬──┬──┬──┬──┬────┘                   │
│                 │  │  │  │  │  │                         │
│    ┌────────────┘  │  │  │  │  └─────────┐              │
│    ▼               ▼  ▼  ▼  ▼            ▼              │
│ ┌──────┐ ┌────────┐┌───┐┌───┐┌────┐ ┌──────────┐       │
│ │ AUTH │ │MESSAG. ││MED││CAL││ AI │ │  BOTS +  │       │
│ │      │ │        ││IA ││LS ││    │ │INTEGRAT. │       │
│ └──┬───┘ └───┬────┘└─┬─┘└─┬─┘└─┬──┘ └────┬─────┘       │
│    │         │       │    │    │          │              │
│    └─────────┼───────┼────┼────┼──────────┘              │
│              ▼       ▼    ▼    ▼                         │
│    ┌─────────────────────────────────┐                   │
│    │         DATA LAYER              │                   │
│    │  PostgreSQL │ ScyllaDB │ Redis  │                   │
│    │  Meilisearch│ R2 (S3)  │ NATS   │                   │
│    └─────────────────────────────────┘                   │
│                    SATURN.AC                              │
└─────────────────────────────────────────────────────────┘
```

### 3.2 Принципы архитектуры

1. **Микросервисы** — 8 независимых Go сервисов, каждый со своим Dockerfile
2. **At-rest Encryption** — данные зашифрованы в хранилище (AES-256-GCM), compliance читает через API
3. **API-first** — REST HTTP + WebSocket, никакого MTProto
4. **Event-driven** — NATS JetStream для асинхронных операций
5. **Horizontal scaling** — каждый сервис масштабируется независимо
6. **No vendor lock-in** — self-hosted на Saturn.ac (fork Coolify)

### 3.3 Потоки данных

**Отправка сообщения (DM):**
```
User A → Gateway (HTTP POST) → Messaging Service → PostgreSQL (store)
                                    ↓
                               NATS (event)
                                    ↓
                            Gateway (WS Hub)
                                    ↓
                           User B (WebSocket push)
```

**Групповое сообщение:**
```
User A → Gateway → Messaging → PostgreSQL
                       ↓
                  NATS (fan-out to N members)
                       ↓
              Gateway WS Hub → User B, C, D, ... (parallel push)
```

**Звонок (WebRTC):**
```
User A → Gateway (WS: call_init) → Calls Service → DB (log call)
                                        ↓
                                   NATS (notify)
                                        ↓
                              Gateway → User B (WS: call_incoming)
User B → Gateway (WS: call_accept) → SDP exchange via WS
                                        ↓
                               P2P / TURN (coturn) / SFU (Pion)
```

---

## 4. Технологический стек

### 4.1 Backend

| Компонент | Технология | Версия | Назначение |
|-----------|-----------|--------|-----------|
| Язык | Go | 1.24 | Все микросервисы |
| HTTP Framework | Fiber | v2 | REST API, middleware |
| Inter-service | HTTP + X-Internal-Token | — | HTTP-прокси через Gateway с shared secret |
| БД (основная) | PostgreSQL | 16 | Пользователи, чаты, метаданные |
| БД (сообщения) | ScyllaDB | latest | Высоконагруженное хранение сообщений |
| Кэш | Redis | 7 | Сессии, online status, typing, rate limiting |
| Очередь | NATS JetStream | 2 | Гарантированная доставка, fan-out |
| Поиск | Meilisearch | 1.7 | Полнотекстовый поиск с typo tolerance |
| Медиа хранилище | Cloudflare R2 | — | S3-compatible, фото/видео/файлы |
| WebRTC SFU | Pion | latest | Групповые видеозвонки |
| TURN сервер | coturn | 4.6 | NAT traversal для звонков |
| AI | Anthropic Claude API | — | Саммари, перевод, транскрипция |
| Push | FCM + APNs + VAPID | — | Push уведомления |

### 4.2 Frontend

| Компонент | Технология | Версия | Назначение |
|-----------|-----------|--------|-----------|
| Base | Telegram Web A fork | — | Proven UI/UX, 931 компонент |
| Framework | Teact | custom | Lightweight React-like library |
| Язык | TypeScript | 5.9 | Strict mode |
| Сборка | Webpack | 5 | Code splitting, HMR |
| Стили | SCSS Modules | — | Scoped styles, BEM |
| State | Custom Global State | — | Redux-like, Teactn |
| API layer | Saturn HTTP Client | custom | REST + WebSocket → Worker |
| Worker | Web Worker | — | API вызовы в отдельном потоке |
| Animations | fasterdom + RAF | custom | 60fps DOM batching |
| Stickers | rlottie | — | TGS animated stickers |
| IndexedDB | — | — | Local message cache |

### 4.3 Desktop

| Компонент | Технология | Назначение |
|-----------|-----------|-----------|
| Обёртка | Tauri 2.0 (Rust) | Native window, tray, notifications |
| Результат | .dmg / .exe / .AppImage | 10-15MB installer |
| Auto-update | Tauri updater plugin | OTA updates |
| Deep links | `orbit://chat/id` | URL scheme handler |

### 4.4 Mobile

| Компонент | Технология | Назначение |
|-----------|-----------|-----------|
| PWA | Service Worker + manifest | Уже работает из TG Web A |
| Native (future) | React Native | iOS + Android |
| Code sharing | ~70% с web | Shared logic layer |

---

## 5. Микросервисы

### 5.1 Gateway Service

**Роль:** API Gateway + WebSocket Hub + HTTP proxy

**Endpoints (реализовано):**

| Endpoint | Method | Auth | Назначение |
|----------|--------|------|-----------|
| `/health` | GET | No | Health check |
| `/api/v1/auth/*` | ALL | No | Proxy к Auth service |
| `/api/v1/users/me` | GET | JWT | Текущий пользователь |
| `/api/v1/users/me` | PUT | JWT | Обновить профиль |
| `/api/v1/users/:id` | GET | JWT | Профиль пользователя |
| `/api/v1/users` | GET | JWT | Поиск пользователей (?q=) |
| `/api/v1/chats` | GET | JWT | Список чатов |
| `/api/v1/chats` | POST | JWT | Создать группу |
| `/api/v1/chats/:id` | GET | JWT | Детали чата |
| `/api/v1/chats/direct` | POST | JWT | Создать/получить DM |
| `/api/v1/chats/:id/messages` | GET | JWT | Сообщения (pagination) |
| `/api/v1/chats/:id/messages` | POST | JWT | Отправить сообщение |
| `/api/v1/messages/:id` | PATCH | JWT | Редактировать |
| `/api/v1/messages/:id` | DELETE | JWT | Удалить (soft) |
| `/api/v1/chats/:id/members` | GET | JWT | Участники чата |
| `/api/v1/chats/:id/members` | POST | JWT | Добавить участника |
| `/api/v1/chats/:id/read` | PATCH | JWT | Отметить прочитанным |
| `/api/v1/ws` | WS | JWT (query) | WebSocket подключение |

**WebSocket Hub:**
- In-memory map: userId → WebSocket connection
- Broadcast events: new_message, typing, user_status, messages_read
- Heartbeat: ping/pong каждые 30 секунд
- Auto-reconnect: exponential backoff (1s → 30s)

### 5.2 Auth Service

**Роль:** Аутентификация, сессии, 2FA, invite codes

**Endpoints:**

| Endpoint | Method | Auth | Назначение |
|----------|--------|------|-----------|
| `/api/v1/auth/bootstrap` | POST | No | Создать первого админа |
| `/api/v1/auth/register` | POST | No | Регистрация по invite code |
| `/api/v1/auth/login` | POST | No | Вход (email + password + optional TOTP) |
| `/api/v1/auth/refresh` | POST | No | Обновить access token |
| `/api/v1/auth/invite/validate` | POST | No | Проверить invite code |
| `/api/v1/auth/me` | GET | JWT | Текущий пользователь + validate token |
| `/api/v1/auth/logout` | POST | JWT | Завершить сессию |
| `/api/v1/auth/sessions` | GET | JWT | Активные сессии |
| `/api/v1/auth/sessions/:id` | DELETE | JWT | Завершить чужую сессию |
| `/api/v1/auth/2fa/setup` | POST | JWT | Получить TOTP secret + QR |
| `/api/v1/auth/2fa/verify` | POST | JWT | Подтвердить 2FA |
| `/api/v1/auth/invites` | POST | Admin | Создать invite code |
| `/api/v1/auth/invites` | GET | Admin | Список invite codes |
| `/api/v1/auth/invites/:id` | DELETE | Admin | Отозвать invite |
| `/api/v1/auth/reset-admin` | POST | Secret key | Сброс пароля админа |

**Токены:**
- Access token: JWT HS256, TTL 15 минут
- Refresh token: httpOnly cookie, TTL 30 дней
- Atomic rotation: GetDel в Redis при refresh

### 5.3 Messaging Service (реализован)

**Роль:** Бизнес-логика сообщений, at-rest шифрование, sync

**Реализовано:**
- CRUD сообщений, чаты, группы, роли, permissions
- At-rest encryption (AES-256-GCM) в store-слое
- Реакции (Unicode emoji), стикеры, GIF, опросы
- Scheduled messages
- NATS pub/sub для real-time доставки
- Invite links, slow mode, pinned messages

### 5.4 Media Service (реализован)

**Роль:** Upload/download медиа, сжатие, стриминг

**Реализовано:**
- `POST /media/upload` → R2
- `POST /media/upload/chunked/*` → chunked upload для файлов >10MB
- `GET /media/:id` → presigned R2 URL / gateway proxy
- `GET /media/:id/thumbnail` → 320px thumbnail
- Resize фото (thumbnail + medium + original), WebP conversion
- Video: first frame extraction, metadata
- Voice: waveform peak values
- Video note: circular 384px, ≤60s
- File type validation

### 5.5 Calls Service (планируется)

**Роль:** WebRTC signaling, TURN, SFU

**Архитектура:**
- P2P для 1-on-1 (browser-to-browser)
- TURN (coturn) для NAT traversal
- SFU (Pion) для групповых звонков до 50 человек
- Signaling через Gateway WebSocket

### 5.6 AI Service (реализован)

**Роль:** Claude API интеграция

**Реализовано:**
- `POST /ai/summarize` — саммари чата (SSE streaming)
- `POST /ai/translate` — перевод сообщений
- `POST /ai/reply-suggest` — 3 варианта ответа
- `POST /ai/transcribe` — voice → text (Whisper)
- `@orbit-ai` — чат-бот в любом чате через @-mention
- `/ai/search` — отложен на 8A.2 (pgvector)

### 5.7 Bots Service (реализован)

**Роль:** Bot API, совместимый с Telegram Bot API

**Реализовано:**
- Создание ботов (token generation), admin UI
- Webhook delivery + retry
- /commands, inline keyboards
- BotFather system bot
- Bot management UI в настройках

### 5.8 Integrations Service (реализован)

**Роль:** Webhook система для внешних инструментов

**Реализовано:**
- MST webhook framework + presets
- Saturn.ac E2E integration (deploy status)
- InsightFlow, Keitaro, ASA — presets готовы (ждут реальных credentials)
- HR-бот — прямая интеграция (pending)

---

## 6. База данных

### 6.1 PostgreSQL — основная БД

```sql
-- Пользователи
CREATE TABLE users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL,
  display_name TEXT NOT NULL,
  avatar_url TEXT,
  bio TEXT,
  phone TEXT UNIQUE,
  status TEXT DEFAULT 'offline', -- online/offline/away/dnd
  custom_status TEXT,
  custom_status_emoji TEXT,
  role TEXT DEFAULT 'member', -- superadmin/compliance/admin/member
  totp_secret TEXT,
  totp_enabled BOOLEAN DEFAULT FALSE,
  last_seen_at TIMESTAMPTZ,
  invited_by UUID REFERENCES users(id),
  invite_code TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Устройства (multi-device)
CREATE TABLE devices (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES users(id),
  device_name TEXT,
  platform TEXT, -- web/desktop/ios/android
  push_token TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Сессии
CREATE TABLE sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES users(id),
  device_id UUID REFERENCES devices(id),
  token_hash TEXT NOT NULL,
  ip_address INET,
  user_agent TEXT,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Invite codes
CREATE TABLE invites (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  code TEXT UNIQUE NOT NULL,
  created_by UUID REFERENCES users(id),
  used_by UUID REFERENCES users(id),
  email TEXT,
  role TEXT DEFAULT 'member',
  max_uses INT DEFAULT 1,
  use_count INT DEFAULT 0,
  expires_at TIMESTAMPTZ,
  is_active BOOLEAN DEFAULT TRUE,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  used_at TIMESTAMPTZ
);

-- Чаты
CREATE TABLE chats (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  type TEXT NOT NULL, -- direct/group
  name TEXT,
  description TEXT,
  avatar_url TEXT,
  created_by UUID REFERENCES users(id),
  max_members INT DEFAULT 200000,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Участники чатов
CREATE TABLE chat_members (
  chat_id UUID REFERENCES chats(id),
  user_id UUID REFERENCES users(id),
  role TEXT DEFAULT 'member', -- owner/admin/member/readonly/banned
  permissions BIGINT DEFAULT 0, -- битовая маска
  custom_title TEXT,
  joined_at TIMESTAMPTZ DEFAULT NOW(),
  muted_until TIMESTAMPTZ,
  last_read_message_id UUID,
  notification_level TEXT DEFAULT 'all', -- all/mentions/none
  PRIMARY KEY (chat_id, user_id)
);

-- Быстрый поиск DM
CREATE TABLE direct_chat_lookup (
  user1_id UUID NOT NULL, -- canonical: user1_id < user2_id
  user2_id UUID NOT NULL,
  chat_id UUID REFERENCES chats(id),
  PRIMARY KEY (user1_id, user2_id)
);

-- Сообщения (PostgreSQL — для Phase 1-7, ScyllaDB позже)
CREATE TABLE messages (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  chat_id UUID REFERENCES chats(id),
  sender_id UUID REFERENCES users(id),
  reply_to_id UUID REFERENCES messages(id),
  type TEXT DEFAULT 'text', -- text/photo/video/file/voice/video_note/sticker/gif/system
  content TEXT, -- AES-256-GCM encrypted at-rest via store layer
  media_url TEXT,
  media_type TEXT,
  media_size BIGINT,
  media_duration_seconds INT,
  is_edited BOOLEAN DEFAULT FALSE,
  is_forwarded BOOLEAN DEFAULT FALSE,
  forwarded_from UUID,
  is_pinned BOOLEAN DEFAULT FALSE,
  thread_id UUID,
  is_deleted BOOLEAN DEFAULT FALSE,
  sequence_number BIGINT DEFAULT nextval('messages_seq'),
  created_at TIMESTAMPTZ DEFAULT NOW(),
  edited_at TIMESTAMPTZ
);

-- Реакции
CREATE TABLE message_reactions (
  message_id UUID REFERENCES messages(id),
  user_id UUID REFERENCES users(id),
  emoji TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY (message_id, user_id, emoji)
);

-- Стикер-паки
CREATE TABLE sticker_packs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title TEXT NOT NULL,
  short_name TEXT UNIQUE NOT NULL,
  author_id UUID REFERENCES users(id),
  thumbnail_url TEXT,
  is_official BOOLEAN DEFAULT FALSE,
  is_animated BOOLEAN DEFAULT FALSE,
  sticker_count INT DEFAULT 0,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Стикеры
CREATE TABLE stickers (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  pack_id UUID REFERENCES sticker_packs(id),
  emoji TEXT,
  file_url TEXT NOT NULL,
  file_type TEXT DEFAULT 'webp', -- webp/tgs/webm
  width INT,
  height INT,
  position INT DEFAULT 0
);

-- Установленные паки пользователя
CREATE TABLE user_sticker_packs (
  user_id UUID REFERENCES users(id),
  pack_id UUID REFERENCES sticker_packs(id),
  position INT DEFAULT 0,
  installed_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY (user_id, pack_id)
);

-- Звонки
CREATE TABLE calls (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  type TEXT NOT NULL, -- voice/video
  mode TEXT NOT NULL, -- p2p/group
  chat_id UUID REFERENCES chats(id),
  initiator_id UUID REFERENCES users(id),
  status TEXT DEFAULT 'ringing', -- ringing/active/ended/missed/declined
  started_at TIMESTAMPTZ,
  ended_at TIMESTAMPTZ,
  duration_seconds INT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Участники звонка
CREATE TABLE call_participants (
  call_id UUID REFERENCES calls(id),
  user_id UUID REFERENCES users(id),
  joined_at TIMESTAMPTZ,
  left_at TIMESTAMPTZ,
  is_muted BOOLEAN DEFAULT FALSE,
  is_camera_off BOOLEAN DEFAULT FALSE,
  is_screen_sharing BOOLEAN DEFAULT FALSE,
  PRIMARY KEY (call_id, user_id)
);

-- Push подписки
CREATE TABLE push_subscriptions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES users(id),
  endpoint TEXT NOT NULL,
  p256dh TEXT NOT NULL,
  auth TEXT NOT NULL,
  user_agent TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Интеграции / Webhooks
CREATE TABLE integrations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  chat_id UUID REFERENCES chats(id),
  webhook_url TEXT,
  webhook_secret TEXT,
  config JSONB,
  is_active BOOLEAN DEFAULT TRUE,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Настройки пользователя
CREATE TABLE user_settings (
  user_id UUID REFERENCES users(id) PRIMARY KEY,
  theme TEXT DEFAULT 'auto',
  language TEXT DEFAULT 'ru',
  font_size INT DEFAULT 16,
  send_by_enter BOOLEAN DEFAULT TRUE,
  dnd_from TIME,
  dnd_until TIME
);

-- Privacy настройки
CREATE TABLE privacy_settings (
  user_id UUID REFERENCES users(id) PRIMARY KEY,
  last_seen TEXT DEFAULT 'everyone',
  avatar TEXT DEFAULT 'everyone',
  phone TEXT DEFAULT 'contacts',
  calls TEXT DEFAULT 'everyone',
  groups TEXT DEFAULT 'everyone',
  forwarded TEXT DEFAULT 'everyone'
);

-- Заблокированные пользователи
CREATE TABLE blocked_users (
  user_id UUID REFERENCES users(id),
  blocked_user_id UUID REFERENCES users(id),
  created_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY (user_id, blocked_user_id)
);

-- Notification settings per-chat
CREATE TABLE notification_settings (
  user_id UUID REFERENCES users(id),
  chat_id UUID REFERENCES chats(id),
  muted_until TIMESTAMPTZ,
  sound TEXT DEFAULT 'default',
  show_preview BOOLEAN DEFAULT TRUE,
  PRIMARY KEY (user_id, chat_id)
);

-- Боты
CREATE TABLE bots (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id UUID REFERENCES users(id),
  display_name TEXT NOT NULL,
  username TEXT UNIQUE NOT NULL,
  avatar_url TEXT,
  description TEXT,
  api_token TEXT UNIQUE NOT NULL,
  webhook_url TEXT,
  is_active BOOLEAN DEFAULT TRUE,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Медиа файлы
CREATE TABLE media (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  uploader_id UUID REFERENCES users(id),
  type TEXT NOT NULL, -- photo/video/file/voice/videonote/gif
  mime_type TEXT NOT NULL,
  original_filename TEXT,
  size_bytes BIGINT NOT NULL,
  r2_key TEXT NOT NULL,
  thumbnail_r2_key TEXT,
  width INT,
  height INT,
  duration_seconds FLOAT,
  waveform_data BYTEA,
  is_one_time BOOLEAN DEFAULT FALSE,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Invite links для чатов
CREATE TABLE chat_invite_links (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  chat_id UUID REFERENCES chats(id),
  creator_id UUID REFERENCES users(id),
  hash TEXT UNIQUE NOT NULL,
  expire_at TIMESTAMPTZ,
  usage_limit INT,
  usage_count INT DEFAULT 0,
  requires_approval BOOLEAN DEFAULT FALSE,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Audit log (RBAC)
CREATE TABLE audit_log (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  actor_id UUID REFERENCES users(id),
  action TEXT NOT NULL, -- user_created, user_deactivated, role_changed, etc.
  target_type TEXT, -- user, chat, message
  target_id UUID,
  details JSONB,
  ip_address INET,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Polls
CREATE TABLE polls (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  message_id UUID REFERENCES messages(id),
  question TEXT NOT NULL,
  is_anonymous BOOLEAN DEFAULT TRUE,
  is_multiple BOOLEAN DEFAULT FALSE,
  is_quiz BOOLEAN DEFAULT FALSE,
  correct_option INT,
  close_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE poll_options (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  poll_id UUID REFERENCES polls(id),
  text TEXT NOT NULL,
  position INT NOT NULL
);

CREATE TABLE poll_votes (
  poll_id UUID REFERENCES polls(id),
  option_id UUID REFERENCES poll_options(id),
  user_id UUID REFERENCES users(id),
  voted_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY (poll_id, user_id, option_id)
);
```

### 6.2 ScyllaDB — сообщения (Phase 8)

```cql
CREATE TABLE messages (
  chat_id UUID,
  bucket INT,              -- unix_time / 86400 (day partitions)
  sequence_number BIGINT,
  sender_id UUID,
  content TEXT, -- at-rest encryption applied at store layer
  media_ids LIST<UUID>,
  reply_to BIGINT,
  edited_at TIMESTAMP,
  deleted BOOLEAN,
  created_at TIMESTAMP,
  PRIMARY KEY ((chat_id, bucket), sequence_number)
) WITH CLUSTERING ORDER BY (sequence_number DESC);
```

### 6.3 Redis — кэш и real-time

```
online:{userId}                → TTL 5min (heartbeat)
typing:{chatId}:{userId}       → TTL 6sec
session:{tokenHash}            → user data, TTL = refresh TTL
ratelimit:{userId}:{endpoint}  → counter, TTL = window
jwt_blacklist:{tokenHash}      → exists until exp
```

---

## 7. API спецификация

### 7.1 Общие правила

- **Base URL:** настраивается через env `API_BASE_URL`
- **Auth:** `Authorization: Bearer <JWT>` header + httpOnly cookie fallback
- **Content-Type:** `application/json`
- **Pagination:** Cursor-based (`?limit=50&after_id=UUID`)
- **Rate limiting:** 600 req/min/user (auth sensitive: 5/min, auth sessions: 60/min, AI: 20/min)

### 7.2 Error format

```json
{
  "error": "not_found",
  "message": "Chat not found",
  "status": 404
}
```

### 7.3 HTTP Status Codes

| Code | Значение |
|------|---------|
| 200 | OK |
| 201 | Created |
| 400 | Bad Request (validation) |
| 401 | Unauthorized (token expired/invalid) |
| 403 | Forbidden (no permission) |
| 404 | Not Found |
| 429 | Rate Limited |
| 500 | Internal Server Error |

---

## 8. WebSocket протокол

### 8.1 Подключение

```
WSS /api/v1/ws
```

Auth через первый `auth` frame (JSON `{"type": "auth", "token": "<JWT>"}`). Токен НЕ передаётся в URL query string — это безопаснее, т.к. URL попадает в серверные логи и referrer headers.

### 8.2 Client → Server events

| Event | Payload | Назначение |
|-------|---------|-----------|
| `typing` | `{chat_id: "uuid"}` | Индикатор набора |
| `ping` | `{}` | Keep-alive |

### 8.3 Server → Client events

| Event | Payload | Назначение |
|-------|---------|-----------|
| `new_message` | `{id, chat_id, sender_id, content, ...}` | Новое сообщение |
| `message_updated` | `{id, chat_id, content, ...}` | Редактирование |
| `message_deleted` | `{chat_id, message_ids[]}` | Удаление |
| `typing` | `{chat_id, user_id}` | Кто-то печатает |
| `user_status` | `{user_id, status, last_seen}` | Онлайн/оффлайн |
| `messages_read` | `{chat_id, max_message_id, is_outgoing, unread_count}` | Прочитано |
| `call_incoming` | `{call_id, caller_id, type}` | Входящий звонок |
| `call_accepted` / `call_declined` / `call_ended` | `{call_id}` | Статус звонка |
| `webrtc_offer` / `webrtc_answer` / `webrtc_ice_candidate` | SDP/ICE data | WebRTC signaling |
| `chat_created` / `chat_updated` | `{chat_id, ...}` | Изменения чата |
| `chat_member_added` / `chat_member_removed` | `{chat_id, user_id}` | Участники |

---

## 9. Безопасность и шифрование

### 9.1 Принцип Compliance-first Security

Orbit — **корпоративный мессенджер**. Администрация (superadmin, compliance) имеет полный доступ к переписке для целей compliance, HR-расследований и аудита. Данные защищены **at-rest шифрованием** от кражи дампа БД, но доступны через API авторизованным ролям.

> **Историческая справка:** Изначально проектировался Zero-Knowledge E2E мессенджер (Signal Protocol). Решение было отклонено — корпоративная модель MST требует полного доступа администрации к переписке.

### 9.2 Уровни шифрования

| Уровень | Технология | Что защищает |
|---------|-----------|-------------|
| Transport | TLS 1.3 (HTTPS/WSS) | Данные в пути |
| Auth | JWT + bcrypt + httpOnly cookies | Доступ к API |
| Message at-rest | AES-256-GCM (store layer) | Содержимое сообщений при краже дампа БД |
| Media | AES-256-GCM (TODO: per-file keys) | Файлы в R2 |

### 9.3 Матрица доступа (RBAC)

| Данные | Member | Admin | Compliance | Superadmin | Сервер (дамп БД) | Хакер |
|--------|--------|-------|------------|------------|-------------------|-------|
| Свои DM | ✅ | ✅ | ✅ | ✅ | ❌ (ciphertext) | ❌ |
| Чужие DM | ❌ | ❌ | ✅ | ✅ | ❌ (ciphertext) | ❌ |
| Группы (участник) | ✅ | ✅ | ✅ | ✅ | ❌ (ciphertext) | ❌ |
| Метаданные (кто, когда) | ❌ | ✅ | ✅ | ✅ | ✅ (plaintext) | ❌ |
| Email/телефон | ✅ (свои) | ✅ | ✅ | ✅ | ✅ (plaintext) | ❌ |
| Логи сервера | ❌ | ✅ | ✅ | ✅ | ✅ | ❌ |
| Управление ролями | ❌ | ❌ | ❌ | ✅ | ❌ | ❌ |
| Audit log | ❌ | ✅ | ✅ | ✅ | ✅ | ❌ |

### 9.5 Защита инфраструктуры

- HTTPS only + HSTS
- Rate limiting на все endpoints
- JWT blacklist при logout (Redis, fail-closed)
- Parameterized SQL queries (no injection)
- CSP headers (no XSS)
- CORS whitelist (только orbit домены)
- bcrypt для паролей (cost 12)
- Input validation + sanitization
- File upload: type validation + ClamAV scan
- SSRF protection: блок приватных IP/loopback

---

## 10. At-rest Encryption

### 10.1 Обзор

Orbit использует **server-side at-rest encryption** для защиты содержимого сообщений. При краже дампа БД атакующий получает только ciphertext. Авторизованные пользователи (и compliance роли) читают сообщения прозрачно через API.

### 10.2 Архитектура

```
1. Запись сообщения:
   - Messaging service → store layer
   - Store: AES-256-GCM encrypt(content, master_key) → encrypted content
   - Encrypted content → PostgreSQL (messages.content)

2. Чтение сообщения:
   - PostgreSQL → encrypted content
   - Store: AES-256-GCM decrypt(content, master_key) → plaintext
   - Plaintext → API response → клиент

3. Master key:
   - Env variable: ORBIT_MESSAGE_ENCRYPTION_KEY
   - Один ключ для всех сообщений (symmetric)
   - TODO: KMS (HashiCorp Vault / AWS KMS) для key rotation
```

### 10.3 Что защищает

| Сценарий | Защита |
|----------|--------|
| Кража дампа PostgreSQL | ✅ Ciphertext, бесполезен без master key |
| Кража бэкапа БД | ✅ Аналогично |
| SQL injection (read-only) | ✅ Ciphertext |
| Компрометация сервера (full) | ❌ Master key в памяти процесса |
| Insider threat (admin) | ❌ Compliance model — доступ через API |

### 10.4 Roadmap

- **Phase 8D:** Per-file encryption для медиа в R2 (отдельный ключ на файл, master key оборачивает)
- **Future:** KMS интеграция (HashiCorp Vault) для key rotation и аудита доступа к ключам

---

## 11. Полный функционал по модулям

### 11.1 Messaging (Фаза 1)

| Функция | Описание | Приоритет |
|---------|---------|-----------|
| Отправка текстовых сообщений | Enter для отправки, Shift+Enter для новой строки | Must |
| Optimistic UI | Сообщение появляется мгновенно с clock icon | Must |
| Delivery status | ✓ отправлено / ✓✓ прочитано | Must |
| Reply (цитирование) | Свайп или правый клик → ответить | Must |
| Edit (редактирование) | Карандашик → изменить текст → "изменено" | Must |
| Delete | Удалить для себя / для всех | Must |
| Forward (пересылка) | Переслать в другой чат с attribution | Must |
| Pin message | Закрепить в хедере чата | Must |
| Typing indicator | "Alice печатает..." | Must |
| Online status | Зелёная точка / "был(а) недавно" | Must |
| Date separators | "Today", "Yesterday", "Friday" | Must |
| Scroll to bottom | Кнопка при новых сообщениях | Must |
| Rich text | **bold**, *italic*, `code`, ~~strike~~ | Should |
| Link preview | OG-теги: заголовок, описание, картинка | Should |
| Message search | Поиск внутри чата | Should |

### 11.2 Groups (Фаза 2)

| Функция | Описание | Приоритет |
|---------|---------|-----------|
| Создание группы | Название + аватар + выбор участников | Must |
| Управление участниками | Добавить / удалить / забанить | Must |
| Роли | Owner → Admin → Member → Banned | Must |
| Permissions | Битовая маска: send, media, pin, delete, etc. | Must |
| @mentions | Автокомплит @username → уведомление | Must |
| Invite links | Публичные + приватные + с лимитом | Must |
| Slow mode | 1 сообщение каждые N секунд | Should |
| Topics/Forums | Треды внутри канала | Nice |
| Join requests | Модерация вступления | Should |

### 11.3 Media & Files (Фаза 3)

| Функция | Описание | Приоритет |
|---------|---------|-----------|
| Фото | Upload + resize + thumbnail + gallery | Must |
| Видео | Upload + streaming + thumbnail | Must |
| Файлы | Любой тип, до 2GB, chunked upload | Must |
| Голосовое | Запись → waveform → отправка | Must |
| Видео-кружок | 60 сек, circular 384px | Should |
| GIF | Поиск через Tenor API | Should |
| Drag & Drop | Перетащить файл в чат | Must |
| Clipboard paste | Ctrl+V → вставить скриншот | Must |
| Preview перед отправкой | Просмотр + caption | Must |
| One-time media | Самоуничтожающееся фото/видео | Should |
| Media spoiler | Блюр до клика | Should |
| Альбомы | Несколько фото в одном сообщении | Should |
| Media gallery | Все медиа чата во вкладке | Must |

### 11.4 Search & Notifications (Фаза 4)

| Функция | Описание | Приоритет |
|---------|---------|-----------|
| Глобальный поиск | По сообщениям, юзерам, чатам | Must |
| Поиск в чате | Лупа → фильтры (from, date, type) | Must |
| Jump to message | Клик на результат → скролл к сообщению | Must |
| Web Push | VAPID уведомления при закрытом браузере | Must |
| In-app notification | Banner при сообщении в фоновом чате | Must |
| Per-chat mute | Замьютить на час / 8 часов / навсегда | Must |
| DND (не беспокоить) | Расписание тишины | Should |
| @mention notification | Пушить даже при mute | Must |
| Privacy: Last seen | Все / контакты / никто | Must |
| Privacy: Avatar | Кто видит аватар | Must |
| Active sessions | Список устройств + завершить | Must |
| Theme (dark/light/auto) | Переключение темы | Must |
| Language | RU / EN | Must |
| Font size | 12-20px | Should |

### 11.5 Rich Messaging (Фаза 5)

| Функция | Описание | Приоритет |
|---------|---------|-----------|
| Реакции | Long-press → emoji → анимация | Must |
| Стикеры | Паки + поиск + animated (.tgs) | Must |
| Custom emoji | В нике и статусе (бесплатно!) | Must |
| GIF picker | Поиск + trending + сохранённые | Must |
| Опросы | Создание + голосование + результаты real-time | Should |
| Quiz mode | Опрос с правильным ответом | Nice |
| Scheduled messages | Отправить завтра в 9:00 | Should |
| Saved Messages | Личный архив | Should |
| Emoji status | Эмодзи рядом с именем | Should |
| TG Sticker Import | Импорт стикер-паков из Telegram | Should |
| **No Premium** | ВСЕ фичи бесплатные для всех | Принцип |

### 11.6 Voice & Video Calls (Фаза 6)

| Функция | Описание | Приоритет |
|---------|---------|-----------|
| 1-on-1 voice | Кнопка 📞 в хедере → P2P call | Must |
| 1-on-1 video | Кнопка 📹 → видеозвонок | Must |
| Group voice | До 50 участников | Must |
| Group video | Video grid + active speaker | Must |
| Screen sharing | Кнопка в call UI | Must |
| Call history | Список звонков в профиле | Should |
| Rate call | Оценка качества после звонка | Nice |
| Ringtone | Звук + вибрация при входящем | Must |
| Network indicator | Качество связи во время звонка | Should |
| Push for calls | Уведомление при закрытом приложении | Must |

### 11.7 At-rest Encryption & RBAC (Фаза 7 — реализовано)

> Signal Protocol E2E был отклонён — несовместим с compliance моделью. Заменён на at-rest encryption.

| Функция | Описание | Статус |
|---------|---------|--------|
| At-rest encryption | AES-256-GCM обёртка messages.content в store-слое | ✅ Done |
| RBAC роли | superadmin / compliance / admin / member | ✅ Done |
| Audit log | Логирование административных действий | ✅ Done |
| User deactivation | Деактивация пользователей (superadmin) | ✅ Done |
| Media encryption | Per-file AES-256-GCM для R2 | 🔴 TODO (Phase 8D) |
| KMS | HashiCorp Vault / AWS KMS для key management | 🔴 TODO |

### 11.8 AI Integration (Фаза 8)

| Функция | Описание | Приоритет |
|---------|---------|-----------|
| Summarize chat | "Перескажи что было за час" (SSE streaming) | Must |
| Translate | Inline перевод под сообщением | Must |
| Suggest reply | 3 варианта ответов | Should |
| Transcribe voice | Voice → text под кружком | Must |
| Semantic search | "Найди где обсуждали бюджет" (embeddings) | Should |
| @orbit-ai | Диалог с AI в любом чате | Nice |

### 11.9 Bots & Integrations (Фаза 8)

| Функция | Описание | Приоритет |
|---------|---------|-----------|
| Bot API | Telegram-совместимый (sendMessage, editMessage, etc.) | Must |
| Webhooks | Incoming webhook → сообщение в чат | Must |
| /commands | Автокомплит в ChatInput | Must |
| Inline keyboards | Кнопки под сообщением бота | Must |
| InsightFlow | Конверсии → алерт | Must |
| Keitaro | Постбеки → уведомление | Must |
| HR-бот | Прямая интеграция (перенос из TG) | Should |
| Bot management UI | Создать/изменить/удалить в настройках | Should |

---

## 12. Фазы разработки

### 12.1 Сводная таблица

| # | Фаза | Сервисы | Приоритет | Статус |
|---|------|---------|-----------|--------|
| 1 | Core Messaging | gateway, messaging | 🔴 Критично | ✅ Done |
| 2 | Groups | messaging+ | 🔴 Критично | ✅ Done |
| 3 | Media & Files | media | 🔴 Критично | ✅ Done |
| 4 | Search, Notifications & Settings | gateway+, Meilisearch | 🟡 Важно | ✅ Done |
| 5 | Rich Messaging | messaging+ | 🟡 Важно | ✅ Done |
| 6 | Calls | calls, coturn, Pion | 🟡 Важно | 🔴 Pending |
| 7 | At-rest Encryption + RBAC | messaging, auth | 🟠 Нужно | ✅ Done (Signal Protocol отклонён → at-rest) |
| 8A | AI Integration | ai | 🔴 Критично | ✅ Done |
| 8B | Bots | bots | 🔴 Критично | ✅ Done |
| 8C | Integrations | integrations | 🔴 Критично | ✅ Done |
| 8D | Production Hardening | ScyllaDB, monitoring, OWASP, backups | 🔴 Критично | 🔴 In Progress |
| — | Desktop & Mobile | Tauri + PWA | Параллельно | 🔴 Pending |

### 12.2 Критерии Production Ready

**Must Have (нельзя запускать без этого):**
- Фаза 1: текстовые сообщения 100% надёжно
- Фаза 2: группы и каналы
- Фаза 3: фото/файлы
- Фаза 4: push-уведомления
- Фаза 8: мониторинг + бэкапы

**Should Have (нужно для комфорта):**
- Фаза 5: стикеры, реакции, GIF
- Фаза 6: звонки
- Desktop: Tauri app

**Nice to Have (после запуска):**
- Фаза 6: Звонки (WebRTC)
- Desktop: Tauri app

### 12.3 Подробное описание каждой фазы

> Детальный breakdown каждой фазы с endpoints, DB schema, Saturn методами — см. `docs/plans/2026-03-23-orbit-phases-v2-design.md`

---

## 13. Инфраструктура и деплой

### 13.1 Saturn.ac (Production)

Saturn.ac — self-hosted PaaS (fork Coolify) для автоматического деплоя.

**Компоненты:**

| Component | Saturn Name | Path | Port |
|-----------|------------|------|------|
| Gateway | orbit-gateway | services/gateway/** | 8080 |
| Auth | orbit-auth | services/auth/** | 8081 |
| Messaging | orbit-messaging | services/messaging/** | 8082 |
| Media | orbit-media | services/media/** | 8083 |
| Calls | orbit-calls | services/calls/** | 8084 |
| AI | orbit-ai | services/ai/** | 8085 |
| Bots | orbit-bots | services/bots/** | 8086 |
| Frontend | orbit-web | web/** | 80 |

**Databases (Saturn-managed):**
- PostgreSQL 16
- Redis 7
- Meilisearch 1.7

**External:**
- NATS JetStream (Saturn container)
- coturn (Hetzner VPS)
- Cloudflare R2 (media storage)
- Cloudflare CDN (frontend)

### 13.2 Docker Compose (Development)

```yaml
services:
  postgres, redis, meilisearch, nats, coturn, gateway, frontend
```

### 13.3 Deploy Flow

```
git push origin main → Saturn auto-detects → build (~2-3 min) → deploy (blue-green) → health check → live
```

### 13.4 Backup Strategy

| Данные | Метод | Частота |
|--------|-------|---------|
| PostgreSQL | pg_dump + WAL archiving | Ежедневно + PITR |
| ScyllaDB | Snapshot + incremental | Ежедневно |
| Redis | RDB snapshot | Каждые 15 мин |
| R2 (media) | Cross-region replication | Real-time |
| Encryption keys | Env-based (ORBIT_MESSAGE_ENCRYPTION_KEY), TODO: KMS | При ротации ключей |

---

## 14. Кроссплатформенная стратегия

### 14.1 Подход: форк open-source клиентов Telegram

Telegram — один из немногих мессенджеров, чьи клиенты **полностью open-source**. Мы используем это как основу для всех платформ, заменяя MTProto протокол на наш Saturn HTTP API.

**Telegram open-source репозитории (наши референсы):**

| Клиент | Репо | Лицензия | Язык | Что берём |
|--------|------|----------|------|-----------|
| **Web A** | `nicegram/nicegram-web-a` | GPL-3.0 | TypeScript (Teact) | ✅ Уже форкнут — основной фронтенд |
| **Web K** | `nicegram/nicegram-web-z` | GPL-3.0 | TypeScript (React) | Альтернативный референс |
| **Desktop** | `nicegram/nicegram-desktop` (fork tdesktop) | GPL-3.0 | C++ / Qt | UI логика, анимации, системная интеграция |
| **iOS** | `nicegram/nicegram-ios` (fork Telegram-iOS) | GPL-2.0 | Swift / ObjC | Нативный iOS клиент |
| **Android** | `nicegram/nicegram-android` | GPL-2.0 | Kotlin / Java | Нативный Android клиент |
| **TDLib** | `nicegram/nicegram-tdlib` | BSL-1.0 | C++ | Кроссплатформенная бизнес-логика |

### 14.2 Сводная таблица платформ

| Платформа | Основа | Статус | Старт | Результат |
|-----------|--------|--------|-------|-----------|
| **Web** | TG Web A fork (Teact + Webpack) | 🟡 40% | Фаза 1 | SPA в браузере |
| **Desktop Mac** | Tauri 2.0 + Web codebase | 🔴 | Фаза 4+ | `.dmg` (10-15MB) |
| **Desktop Windows** | Tauri 2.0 + Web codebase | 🔴 | Фаза 4+ | `.exe` NSIS installer |
| **Desktop Linux** | Tauri 2.0 + Web codebase | 🔴 | Фаза 4+ | `.AppImage` / `.deb` / Flatpak |
| **Mobile PWA** | Service Worker + Web manifest | 🟡 Базово | Сразу | Installable web app |
| **iOS** | Fork Telegram-iOS → Saturn API | 🔴 | Фаза 6+ | App Store + TestFlight |
| **Android** | Fork Telegram-Android → Saturn API | 🔴 | Фаза 6+ | Google Play + APK |

### 14.3 Web-приложение (основная платформа)

**Текущая реализация.** Форк Telegram Web A — production-grade веб-клиент.

| Характеристика | Значение |
|----------------|---------|
| **Framework** | Teact (custom React-like, lightweight) |
| **Компоненты** | 931 файл — полный Telegram UI |
| **State** | Custom global state (Teactn) — Redux-like |
| **Сборка** | Webpack 5 (code splitting, HMR, tree shaking) |
| **Worker** | Web Worker для API вызовов (не блокирует UI) |
| **Animations** | fasterdom (DOM batching, 60fps) |
| **Localization** | 30+ языков, fallback.strings |
| **PWA** | Service Worker, offline mode, push notifications |
| **Bundle** | < 2MB gzipped |

**Что заменено относительно оригинала:**
- GramJS (MTProto) → Saturn HTTP Client + WebSocket
- Telegram auth (phone + SMS) → Invite code + email/password
- Telegram Premium checks → Все фичи бесплатные (`isPremium → true`)
- Branding: иконки, favicon, title, цвета → Orbit purple

### 14.4 Desktop-приложение (Tauri 2.0)

**Подход:** Tauri оборачивает тот же веб-код в нативное окно. Один исходный код для Web + Desktop.

| Характеристика | Значение |
|----------------|---------|
| **Runtime** | Tauri 2.0 (Rust backend + system webview) |
| **Web engine** | WKWebView (Mac), WebView2 (Windows), WebKitGTK (Linux) |
| **Размер installer** | 10-15MB (vs Electron ~150MB) |
| **RAM** | ~50MB (vs Electron ~300MB) |
| **Auto-update** | Tauri updater plugin (OTA, differential) |
| **Deep links** | `orbit://chat/{id}`, `orbit://user/{username}` |
| **System tray** | Иконка + badge (unread count) + контекстное меню |
| **Native notifications** | OS-native (не браузерные Web Push) |
| **Auto-launch** | Запуск при входе в систему (опционально) |
| **Global shortcut** | ⌘+Shift+O (Mac) / Ctrl+Shift+O (Win/Linux) |
| **Drag & Drop** | Файлы из файловой системы в чат |
| **Clipboard** | Нативный доступ к буферу обмена |

**Результат:**
- macOS: `Orbit.dmg` (Apple Silicon + Intel universal)
- Windows: `Orbit-Setup.exe` (NSIS installer) + portable `.zip`
- Linux: `Orbit.AppImage` + `.deb` (Ubuntu/Debian) + Flatpak

**Почему Tauri, а не Electron:**
- В 10x меньше по размеру (10MB vs 150MB)
- В 5x меньше RAM (50MB vs 300MB)
- Нативный webview = лучшая производительность
- Rust backend = безопасность, скорость
- Один код с вебом (не нужно поддерживать отдельную кодовую базу)

**Почему НЕ форк tdesktop (C++/Qt):**
- tdesktop написан на C++/Qt — совершенно другой стек
- Потребовал бы отдельную команду C++ разработчиков
- Замена MTProto в C++ коде — огромный объём работы
- Tauri + Web = 1 codebase для Web + Desktop, без дополнительной команды

### 14.5 iOS-приложение

**Подход:** Форк Telegram-iOS (Swift/ObjC) → замена MTProto на Saturn HTTP API.

| Характеристика | Значение |
|----------------|---------|
| **Основа** | Telegram-iOS (open-source, GPL-2.0) |
| **Язык** | Swift 5.9 + Objective-C (legacy) |
| **UI** | UIKit + custom components (Telegram's proven UI) |
| **Animations** | Core Animation + Metal shaders |
| **Media** | AVFoundation (камера, микрофон, видео) |
| **Calls** | CallKit integration (нативный UI звонков iOS) |
| **Push** | APNs (Apple Push Notification service) |
| **Storage** | SQLite (local cache) + Keychain (keys) |
| **Minimum iOS** | 15.0+ |
| **Distribution** | App Store + TestFlight |

**Что нужно заменить:**
1. **MTProto → Saturn HTTP Client** — основная работа. Все API вызовы переводятся на REST + WebSocket
2. **TDLib → Saturn SDK** — TDLib (C++ библиотека) заменяется на Swift HTTP client
3. **Phone auth → Invite code + email** — экран авторизации
4. **Server config** — endpoints, certificates, VAPID keys
5. **Branding** — иконки, splash screen, цвета, название
6. **Premium checks** — убрать все `isPremium` гейты

**Нативные iOS фичи (уже есть в Telegram-iOS):**
- Widgets (iOS 14+) — показывают последние чаты на Home Screen
- Share Extension — отправить из любого приложения в Orbit
- Notification Service Extension — расшифровка push на устройстве
- iCloud Keychain для secure storage
- Face ID / Touch ID для блокировки приложения
- CallKit — входящие звонки отображаются как нативные iOS calls
- Picture-in-Picture для видеозвонков
- Siri Shortcuts — "Отправь сообщение в Orbit"
- Live Activities (iOS 16.1+) — активный звонок на Dynamic Island
- Interactive notifications — ответить прямо из notification

**Объём работы:** ~2-3 недели на замену MTProto + тестирование

### 14.6 Android-приложение

**Подход:** Форк Telegram-Android / Nicegram-Android (Kotlin/Java) → замена MTProto на Saturn API.

| Характеристика | Значение |
|----------------|---------|
| **Основа** | Telegram-Android (open-source, GPL-2.0) |
| **Язык** | Kotlin + Java (legacy) |
| **UI** | Custom Views + RecyclerView + ViewPager2 |
| **Animations** | RLottie (стикеры), custom interpolators |
| **Media** | CameraX (камера), ExoPlayer (видео) |
| **Calls** | ConnectionService (нативный UI звонков Android) |
| **Push** | FCM (Firebase Cloud Messaging) |
| **Storage** | SQLite (local cache) + Android Keystore (keys) |
| **Minimum Android** | 6.0+ (API 23) |
| **Distribution** | Google Play + APK download |

**Что нужно заменить:**
1. **MTProto → Saturn HTTP Client** — аналогично iOS
2. **TDLib → Saturn SDK** — Kotlin HTTP client с coroutines
3. **Phone auth → Invite code + email** — экран авторизации
4. **Server config** — endpoints, FCM sender ID
5. **Branding** — иконки, Material You colours, название
6. **Premium checks** — убрать все Premium гейты

**Нативные Android фичи (уже есть в Telegram-Android):**
- Bubbles (Android 11+) — чат поверх других приложений
- Direct Share — быстрый шаринг в конкретный чат
- App Widgets — последние чаты на Home Screen
- Notification channels — отдельные настройки для DM/Groups/Calls
- ConnectionService — входящие звонки как нативные Android calls
- Picture-in-Picture для видеозвонков
- Adaptive Icons + Material You dynamic colors
- Split-screen / foldable support
- Quick Settings tile — статус / DND toggle
- Wear OS companion (будущее) — уведомления на часах

**Объём работы:** ~2-3 недели на замену MTProto + тестирование

### 14.7 Кроссплатформенная архитектура (code sharing)

```
                    ┌─────────────────────────┐
                    │    Saturn HTTP API       │
                    │  (единый бэкенд для всех)│
                    └──────────┬──────────────┘
                               │
          ┌────────────────────┼────────────────────┐
          │                    │                     │
    ┌─────▼─────┐      ┌──────▼──────┐      ┌──────▼──────┐
    │   WEB     │      │  DESKTOP    │      │   MOBILE    │
    │           │      │             │      │             │
    │ TG Web A  │      │ Tauri 2.0   │      │ ┌────┐┌───┐│
    │  (Teact)  │◄────►│ (same code) │      │ │ iOS││AND││
    │           │      │             │      │ │    ││   ││
    │ Saturn    │      │ Saturn      │      │ │TG  ││TG ││
    │ HTTP+WS   │      │ HTTP+WS    │      │ │fork││frk││
    └───────────┘      └─────────────┘      │ └────┘└───┘│
                                            └─────────────┘
    TypeScript            TypeScript          Swift / Kotlin
    (shared codebase)     (shared codebase)  (separate codebases)
```

**Стратегия code sharing:**

| Слой | Web | Desktop | iOS | Android |
|------|-----|---------|-----|---------|
| UI компоненты | Teact (931) | Teact (same) | UIKit (TG-iOS) | Custom Views (TG-Android) |
| API client | Saturn HTTP | Saturn HTTP (same) | Swift HTTP client | Kotlin HTTP client |
| WebSocket | JS WebSocket | JS WebSocket (same) | URLSessionWebSocket | OkHttp WebSocket |
| Encryption | Server-side at-rest | Server-side at-rest | Server-side at-rest | Server-side at-rest |
| State management | Teactn global | Teactn global (same) | Swift structs | Kotlin StateFlow |
| Storage | IndexedDB | IndexedDB (same) | SQLite + Keychain | SQLite + Keystore |
| Push | VAPID | Tauri notifications | APNs | FCM |
| Calls | WebRTC JS | WebRTC JS (same) | WebRTC (native) | WebRTC (native) |

**Web + Desktop = 100% shared code** (Tauri просто оборачивает)
**iOS + Android = отдельные кодовые базы** (форки нативных TG клиентов), но API слой идентичный

### 14.8 Почему форки Telegram, а не React Native для мобильных

| Критерий | Fork Telegram Native | React Native |
|----------|---------------------|-------------|
| **Производительность** | Нативная (Metal/Vulkan) | ~80% от нативной |
| **Анимации** | 60fps всегда (Lottie, custom) | Может лагать на сложных |
| **Камера/Микрофон** | AVFoundation/CameraX (оптимизировано) | Через bridge (задержка) |
| **Звонки** | CallKit/ConnectionService нативно | Кастомный UI (не нативные) |
| **Размер приложения** | ~30MB (как Telegram) | ~50-80MB |
| **UX качество** | Telegram-level (10 лет полировки) | Нужно строить с нуля |
| **Стикеры (TGS/Lottie)** | Встроенный рендерер rlottie | Нужна отдельная интеграция |
| **Объём работы** | 2-3 недели (замена API) | 3-6 месяцев (UI с нуля) |
| **Риск** | Низкий (proven codebase) | Высокий (новая реализация) |

**Вывод:** Форк нативных клиентов Telegram — быстрее, качественнее, меньше рисков.

### 14.9 Timeline разработки по платформам

| Платформа | Зависимость | Начало | Длительность | Релиз |
|-----------|-----------|--------|-------------|-------|
| **Web** | — | Фаза 1 (сейчас) | Непрерывно | С каждой фазой |
| **Desktop** | Web Phase 4 done | После Фазы 4 | 1-2 недели | Tauri wrapper |
| **iOS** | Backend Phase 1-3 done | После Фазы 3 | 2-3 недели | TestFlight → App Store |
| **Android** | Backend Phase 1-3 done | После Фазы 3 | 2-3 недели | Google Play + APK |
| **Mobile PWA** | — | Уже работает | — | Installable сейчас |

---

## 15. UI/UX требования

### 15.1 Дизайн-система

- **Базис:** Telegram Web A (proven UX, 931 компонент)
- **Брендинг:** Orbit purple (#6C63FF → #3B27CC gradient)
- **Тема:** Dark theme (primary) + Light theme + Auto
- **Иконки:** Custom orbit planet icon + TG Web A icon set
- **Шрифт:** Roboto (400/500/600)
- **Радиусы:** rounded-lg (8px)
- **Анимации:** fasterdom + RAF, 60fps target

### 15.2 Layout

```
┌─────────────────────────────────────────────────┐
│ ┌───────────┐ ┌─────────────────┐ ┌───────────┐ │
│ │   LEFT    │ │     MIDDLE      │ │   RIGHT   │ │
│ │           │ │                 │ │           │ │
│ │ Chat List │ │  Message Area   │ │  Profile  │ │
│ │ + Search  │ │  + Composer     │ │  + Media  │ │
│ │ + Folders │ │  + Pin Header   │ │  + Members│ │
│ │           │ │                 │ │           │ │
│ └───────────┘ └─────────────────┘ └───────────┘ │
└─────────────────────────────────────────────────┘
```

### 15.3 Mobile Adaptive

- < 768px: single column (chat list OR messages)
- 768-1024px: two columns (list + messages)
- > 1024px: three columns (list + messages + profile)

---

## 16. Производительность и масштабирование

### 16.1 Performance Targets

| Метрика | Target |
|---------|--------|
| Message delivery | p99 < 100ms |
| API response | p95 < 200ms |
| WebSocket connections | 500 concurrent / instance |
| Media upload throughput | > 100 MB/s aggregate |
| Search latency | < 50ms per query |
| Concurrent users | 150+ без деградации |
| Message throughput | 1000 msg/sec aggregate |
| Frontend TTI | < 3 seconds |
| Bundle size | < 2MB gzipped |

### 16.2 Scaling Strategy

| 150 users | 500 users | 1000+ users |
|-----------|-----------|-------------|
| Single PostgreSQL | Read replicas | Sharded PostgreSQL |
| Single Redis | Redis Sentinel | Redis Cluster |
| Messages in PostgreSQL | ScyllaDB migration | ScyllaDB multi-DC |
| Single Gateway instance | 2-3 instances + LB | Auto-scaling group |
| Meilisearch single | Meilisearch cluster | Dedicated search cluster |

---

## 17. Мониторинг и Observability

| Инструмент | Назначение |
|-----------|-----------|
| Prometheus | Метрики: RPS, latency, error rate, WS connections |
| Grafana | Real-time dashboards |
| Structured logging (JSON) | Loki или stdout → aggregation |
| OpenTelemetry | Distributed tracing через все сервисы |
| Uptime ping | Внешний healthcheck каждые 30 сек |
| Alerts | → Orbit канал "MST Monitoring" (dogfooding!) |

---

## 18. Конкурентный анализ

| Функция | Telegram | Slack | MS Teams | **Orbit** |
|---------|----------|-------|----------|-----------|
| At-rest encryption + compliance | ❌ | ❌ | ❌ | ✅ |
| Self-hosted | ❌ | ❌ | ❌ | ✅ |
| Контроль данных | ❌ | ❌ | ❌ | ✅ |
| AI ассистент | ❌ | ✅ (платно) | ✅ (Copilot) | ✅ (Claude) |
| Стоимость | Бесплатно | $7.25/user/mo | $4/user/mo | Бесплатно |
| Кастомизация | ❌ | Ограничена | Ограничена | ✅ Полная |
| Стикеры/Emoji | ✅ (Premium) | ✅ | ✅ | ✅ (всё бесплатно) |
| Звонки | ✅ | ✅ (платно) | ✅ | ✅ |
| Бот API | ✅ | ✅ | ✅ | ✅ (TG-compatible) |
| Desktop app | ✅ | ✅ | ✅ | ✅ (Tauri) |
| Mobile app | ✅ | ✅ | ✅ | ✅ (PWA + native forks) |

---

## 19. KILLER FEATURES

> **Полное ТЗ на Killer Features:** отдельный документ `docs/TZ-KILLER-FEATURES.md` (650+ строк, 12 разделов, каждая фича с UI mockups, API endpoints, DB schema, оценкой трудозатрат)

### Краткий обзор

| # | Feature | Описание | Effort | Фаза |
|---|---------|---------|--------|------|
| 1 | **Super Access** | C-Level AI аналитика ВСЕХ чатов (compliance доступ уже есть, нужен AI слой) | 27 дней | 9+ |
| 2 | **AI Meeting Notes** | Автозапись звонков → транскрипция → AI протокол + action items | 17 дней | 8 |
| 3 | **Smart Notifications** | AI приоритизация уведомлений (urgent/important/normal/low) | 10 дней | 8 |
| 4 | **Workflow Automations** | No-code IF→THEN правила (встроенный Zapier) | 15 дней | 8 |
| 5 | **Knowledge Base** | AI собирает знания из чатов → searchable Wiki | 12 дней | 9+ |
| 6 | **Live Translate** | Синхронный перевод каждого сообщения на язык получателя | 8 дней | 8 |
| 7 | **Video Notes Pro** | Loom-like запись экрана + камера до 5 мин | 10 дней | 3 |
| 8 | **Anonymous Feedback** | Анонимные сообщения (server-enforced anonymity, без ring signatures) | 12 дней | 9+ |
| 9 | **Status Automations** | Auto-DND, calendar sync, smart presence | 8 дней | 4 |
| 10 | **Team Pulse** | HR Dashboard: response time, sentiment, burnout detection | 15 дней | 9+ |
| 11 | **Orbit Spaces** | Always-on voice rooms (Discord-like виртуальный офис) | 12 дней | 6 |

**Итого Killer Features: ~146 дней дополнительной разработки**

---

## 20. Отличия от Telegram

### 20.1 Что ЛУЧШЕ чем в Telegram

> Развёрнутый контент Killer Features перенесён в `docs/TZ-KILLER-FEATURES.md`

1. **At-rest encryption + compliance доступ** — данные защищены от кражи, администрация контролирует переписку
2. **Все фичи бесплатные** — no Premium paywall
3. **Self-hosted** — полный контроль данных
4. **AI встроен** — Claude API нативно
5. **Интеграции с MST** — InsightFlow, Keitaro, HR-бот
6. **Invite-based регистрация** — без привязки к номеру телефона
7. **Super Access** — C-Level аналитика всех коммуникаций
8. **AI Meeting Notes** — автопротоколы звонков
9. **Orbit Spaces** — виртуальные офисы
10. **Anonymous Feedback** — криптографически анонимные каналы
11. **Live Translate** — синхронный перевод для международных команд

### 20.2 Что НЕ реализуем (Telegram-specific)

- Telegram Premium / Stars / Boost / Payments — нет монетизации
- Telegram Stories — не нужно для корпоративного мессенджера
- Telegram Passport — не нужно
- Telegram Ads — нет рекламы
- Nearby People — не релевантно
- Secret Chats (TG-specific) — не нужны, корпоративная compliance модель

---

## 21. Приложения

### 21.1 Глоссарий

| Термин | Определение |
|--------|-----------|
| Saturn.ac | Self-hosted PaaS для деплоя (fork Coolify) |
| Teact | Custom React-like framework из TG Web A |
| At-rest Encryption | AES-256-GCM шифрование данных в хранилище (PostgreSQL) |
| RBAC | Role-Based Access Control — система ролей (superadmin/compliance/admin/member) |
| Compliance role | Роль с правом чтения всей переписки для HR/legal целей |
| SFU | Selective Forwarding Unit — сервер для групповых видеозвонков |
| TURN | Traversal Using Relays around NAT — relay для WebRTC |
| VAPID | Voluntary Application Server Identification — Web Push auth |

### 21.2 Документы проекта

| Документ | Путь | Строки |
|----------|------|--------|
| Основное ТЗ | `docs/TZ-ORBIT-MESSENGER.md` | ~1200 |
| ТЗ Killer Features | `docs/TZ-KILLER-FEATURES.md` | ~650 |
| Phase Design v2 | `docs/plans/2026-03-23-orbit-phases-v2-design.md` | ~800 |
| Концепция | `CONCEPT.md` | ~775 |
| Архитектура | `docs/ARCHITECTURE.md` | ~231 |
| API | `docs/API.md` | ~226 |
| Безопасность | `docs/SECURITY.md` | ~183 |
| Деплой | `docs/DEPLOYMENT.md` | ~195 |

---

*Документ создан: 2026-03-24 | Обновлён: 2026-04-16*
*Версия: 2.0*
*Orbit Messenger — корпоративный мессенджер для MST*
