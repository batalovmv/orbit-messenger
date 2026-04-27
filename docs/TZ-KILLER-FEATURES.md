# ТЕХНИЧЕСКОЕ ЗАДАНИЕ
# Orbit Messenger — Killer Features

**Версия:** 1.0
**Дата:** 2026-03-24
**Статус:** Планирование
**Зависимость:** Основное ТЗ (`TZ-ORBIT-MESSENGER.md`), Phase 1-8

---

## СОДЕРЖАНИЕ

1. [Super Access — C-Level аналитика](#1-super-access)
2. [AI Meeting Notes — автопротоколы звонков](#2-ai-meeting-notes)
3. [Smart Notifications — AI приоритизация](#3-smart-notifications)
4. [Workflow Automations — встроенный Zapier](#4-workflow-automations)
5. [Knowledge Base — корпоративная Wiki](#5-knowledge-base)
6. [Live Translate — синхронный перевод](#6-live-translate)
7. [Video Notes Pro — Loom в мессенджере](#7-video-notes-pro)
8. [Anonymous Feedback — криптографическая анонимность](#8-anonymous-feedback)
9. [Status Automations — умные статусы](#9-status-automations)
10. [Team Pulse — здоровье команды](#10-team-pulse)
11. [Orbit Spaces — виртуальные офисы](#11-orbit-spaces)
12. [Сводная таблица и приоритизация](#12-сводная-таблица)

---

## 1. Super Access

### 1.1 Описание

Специальный уровень доступа ВЫШЕ обычного администратора, предназначенный для руководства (CEO, COO, HR Director). Даёт полный доступ ко ВСЕМ коммуникациям компании, включая личные переписки, с AI-аналитикой на базе Claude.

**Бизнес-задача:** Руководство должно иметь возможность в любой момент оценить состояние дел в компании, обнаружить проблемы, конфликты и риски — без необходимости спрашивать каждого лично.

**Аналоги в индустрии:**
- Microsoft 365 Compliance Center — eDiscovery, Communication Compliance
- Slack Enterprise Grid — Org-wide search, DLP, Compliance API
- Google Vault — архивация и поиск корпоративных данных

**Отличие Orbit:** AI-аналитика (Claude) поверх полного доступа. Не просто "найди сообщение", а "проанализируй настроение отдела и выдели ключевые проблемы".

### 1.2 Иерархия ролей

```
┌─────────────────────────────────────────────────┐
│  SUPER ACCESS (C-Level)                          │
│  ┌─────────────────────────────────────────┐     │
│  │  Полный доступ ко ВСЕМ данным           │     │
│  │  • Все чаты (включая DM)               │     │
│  │  • AI-анализ коммуникаций               │     │
│  │  • Keyword alerts                        │     │
│  │  • Sentiment analysis                    │     │
│  │  • Employee reports                      │     │
│  │  • Compliance audit log                  │     │
│  └─────────────────────────────────────────┘     │
│                                                   │
│  ADMIN (IT/Security)                              │
│  ┌─────────────────────────────────────────┐     │
│  │  Управление инфраструктурой              │     │
│  │  • Пользователи, invite codes           │     │
│  │  • Группы, каналы                        │     │
│  │  • Настройки сервера                     │     │
│  │  • Метаданные (НЕ содержимое DM)        │     │
│  └─────────────────────────────────────────┘     │
│                                                   │
│  MANAGER                                          │
│  ┌─────────────────────────────────────────┐     │
│  │  Управление своей группой                │     │
│  │  • Добавить/удалить участников          │     │
│  │  • Pin/delete сообщения                  │     │
│  └─────────────────────────────────────────┘     │
│                                                   │
│  MEMBER → READONLY                                │
└─────────────────────────────────────────────────┘
```

### 1.3 Матрица доступа (детальная)

| Данные | Member | Manager | Admin | **Super Access** |
|--------|--------|---------|-------|-----------------|
| Свои DM | ✅ | ✅ | ✅ | ✅ |
| Чужие DM (содержимое) | ❌ | ❌ | ❌ | **✅** |
| Чужие DM (метаданные) | ❌ | ❌ | ✅ | **✅** |
| Группы (участник) | ✅ | ✅ | ✅ | **✅** |
| Группы (не участник) | ❌ | ❌ | ❌ | **✅** |
| Каналы (подписчик) | ✅ | ✅ | ✅ | **✅** |
| AI анализ чатов | ❌ | ❌ | ❌ | **✅** |
| Keyword alerts | ❌ | ❌ | ❌ | **✅** |
| Sentiment analysis | ❌ | ❌ | ❌ | **✅** |
| Employee activity reports | ❌ | ❌ | Частично | **✅** |
| Compliance audit log | ❌ | ❌ | ✅ | **✅** |
| Export данных | ❌ | ❌ | ✅ | **✅** |
| Удаление аккаунтов | ❌ | ❌ | ✅ | **✅** |

### 1.4 Super Access Dashboard — UI спецификация

#### 1.4.1 Главная страница

```
┌─────────────────────────────────────────────────────────────┐
│  🛡️ ORBIT SUPER ACCESS                            [Alex] ▼ │
├──────────┬──────────────────────────────────────────────────┤
│          │                                                   │
│ 📊 Overview   │  Company Overview — 24 марта 2026            │
│ 🔍 Search     │                                              │
│ 💬 All Chats  │  ┌─────────┐ ┌─────────┐ ┌─────────┐       │
│ 👤 Employees  │  │ 1,247   │ │ 134/150 │ │ 4.2 min │       │
│ 📈 Analytics  │  │ messages│ │ active  │ │ avg resp│       │
│ ⚠️ Alerts     │  │ today   │ │ users   │ │ time    │       │
│ 🤖 AI Query   │  └─────────┘ └─────────┘ └─────────┘       │
│ 📋 Reports    │                                              │
│ ⚙️ Settings   │  Sentiment by Department:                    │
│               │  Marketing  ████████████░░ 85% 😊 ↑5%       │
│               │  Dev        ██████████░░░░ 72% 😐 ↓3% ⚠️    │
│               │  Sales      █████████████░ 88% 😊 ↑12%      │
│               │  HR         █████████░░░░░ 65% 😟 ─   ⚠️    │
│               │  Support    ███████████░░░ 78% 😊 ↑2%       │
│               │                                              │
│               │  ⚠️ Active Alerts (3):                       │
│               │  🔴 Keyword "увольняюсь" — 2 matches today  │
│               │  🟡 Dev team response time +40% this week    │
│               │  🟡 3 employees active after 22:00           │
│               │                                              │
│               │  📝 AI Daily Digest:                         │
│               │  "Основные темы сегодня: запуск кампании     │
│               │   в Индии (Marketing), рефакторинг API       │
│               │   (Dev), обработка жалоб (Support). Dev      │
│               │   team показывает признаки перегрузки —      │
│               │   рекомендую проверить distribution задач."   │
│               │                                              │
└──────────┴──────────────────────────────────────────────────┘
```

#### 1.4.2 AI Query — свободный запрос

```
┌─────────────────────────────────────────────────────────────┐
│  🤖 AI Query                                                │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ 💬 "Как дела в отделе маркетинга на этой неделе?"     │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  🤖 Claude analyzing 847 messages across 12 chats...        │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ **Отдел маркетинга — неделя 24-30 марта:**             │ │
│  │                                                         │ │
│  │ **Активность:** 847 сообщений (+15% к прошлой неделе)  │ │
│  │ **Настроение:** 85% позитивное (↑5%)                   │ │
│  │                                                         │ │
│  │ **Ключевые темы:**                                      │ │
│  │ 1. Запуск FB Ads кампании в Индии — Влад ведёт,       │ │
│  │    бюджет $2000, старт 28 марта                         │ │
│  │ 2. Редизайн лендинга — Мария подготовила мокапы,       │ │
│  │    ждёт апрув от Alex                                   │ │
│  │ 3. Отчёт за Q1 — Олег собирает данные, deadline        │ │
│  │    пятница                                               │ │
│  │                                                         │ │
│  │ **Проблемы:**                                            │ │
│  │ • Задержка с креативами (дизайнер болеет)               │ │
│  │ • Влад жалуется на качество лидов из Keitaro           │ │
│  │                                                         │ │
│  │ **Рекомендация:** Проверить лиды Keitaro с Владом,     │ │
│  │ найти замену дизайнеру на время болезни.                │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  Follow-up: [ "Покажи переписку Влада про лиды" ] [Send]    │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

#### 1.4.3 All Chats — доступ ко всем чатам

```
┌─────────────────────────────────────────────────────────────┐
│  💬 All Chats                    🔍 Search all messages      │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  Filter: [All ▼] [Groups ▼] [DMs ▼] [Channels ▼]           │
│                                                              │
│  ┌─ Groups ──────────────────────────────────────────────┐  │
│  │ #marketing (8 members) — last: 5 min ago — 47 today  │  │
│  │ #dev (12 members) — last: 2 min ago — 123 today      │  │
│  │ #general (150 members) — last: 1 min ago — 34 today  │  │
│  │ #support (5 members) — last: 15 min ago — 28 today   │  │
│  └───────────────────────────────────────────────────────┘  │
│                                                              │
│  ┌─ Private DMs ─────────────────────────────────────────┐  │
│  │ 🔒 Ivan ↔ Petr — last: 30 min ago — sentiment: 35%   │  │
│  │ 🔒 Maria ↔ Alex — last: 1h ago — sentiment: 92%      │  │
│  │ 🔒 Vlad ↔ Oleg — last: 2h ago — sentiment: 68%      │  │
│  │ ... (245 active DM pairs)                              │  │
│  └───────────────────────────────────────────────────────┘  │
│                                                              │
│  Click any chat → read messages + AI analysis               │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

#### 1.4.4 Employee Profile — карточка сотрудника

```
┌─────────────────────────────────────────────────────────────┐
│  👤 Ivan Petrov — Senior Developer                           │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  📊 Activity (7 days):                                      │
│  ├─ Messages sent: 234 (avg 33/day)                         │
│  ├─ Chats active in: 8 groups + 12 DMs                      │
│  ├─ Avg response time: 3.1 min                              │
│  ├─ Active hours: 09:00 - 19:30 (some overtime)             │
│  └─ Sentiment: 72% (↓8% from last week) ⚠️                 │
│                                                              │
│  🔗 Top contacts:                                           │
│  ├─ Petr (42 messages) — sentiment 35% ⚠️                  │
│  ├─ Maria (28 messages) — sentiment 85%                     │
│  └─ Alex (15 messages) — sentiment 90%                      │
│                                                              │
│  📝 AI Summary:                                             │
│  "Ivan активно работает над рефакторингом API.              │
│   Есть напряжение с Петром — обсуждают архитектурное        │
│   решение, не могут договориться. Sentiment с Петром         │
│   снизился на 20% за неделю. Рекомендую модерацию."        │
│                                                              │
│  📂 All chats: [Groups (8)] [DMs (12)] [AI Analysis]       │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

### 1.5 Backend — API спецификация

#### 1.5.1 Новый микросервис: `super-access`

| Endpoint | Method | Auth | Описание |
|----------|--------|------|---------|
| `GET /super/dashboard` | GET | Super | Главная страница с метриками |
| `GET /super/chats` | GET | Super | Все чаты компании (группы + DM) |
| `GET /super/chats/:id/messages` | GET | Super | Сообщения любого чата (включая DM) |
| `GET /super/search` | GET | Super | Поиск по ВСЕМ сообщениям |
| `GET /super/users` | GET | Super | Список сотрудников с метриками |
| `GET /super/users/:id` | GET | Super | Профиль сотрудника (активность, sentiment) |
| `GET /super/users/:id/chats` | GET | Super | Все чаты сотрудника |
| `GET /super/users/:id/activity` | GET | Super | Детальная активность (timeline) |
| `GET /super/analytics/sentiment` | GET | Super | Sentiment по отделам / времени |
| `GET /super/analytics/topics` | GET | Super | Trending темы (topic modeling) |
| `GET /super/analytics/communication-graph` | GET | Super | Граф коммуникаций |
| `GET /super/analytics/response-times` | GET | Super | Время ответа по сотрудникам |
| `POST /super/ai/query` | POST | Super | Свободный AI-запрос (SSE streaming) |
| `POST /super/ai/summarize-department` | POST | Super | Саммари отдела за период |
| `POST /super/ai/summarize-employee` | POST | Super | Саммари по сотруднику |
| `POST /super/alerts` | POST | Super | Создать keyword/sentiment alert |
| `GET /super/alerts` | GET | Super | Список активных alerts |
| `DELETE /super/alerts/:id` | DELETE | Super | Удалить alert |
| `GET /super/alerts/triggered` | GET | Super | Сработавшие alerts |
| `GET /super/reports/daily` | GET | Super | AI ежедневный отчёт |
| `GET /super/reports/weekly` | GET | Super | AI еженедельный отчёт |
| `GET /super/reports/employee/:id` | GET | Super | Отчёт по сотруднику |
| `POST /super/export` | POST | Super | Экспорт данных (compliance) |
| `GET /super/audit-log` | GET | Super | Лог действий Super Access (кто что смотрел) |

#### 1.5.2 Middleware аутентификации

```go
func SuperAccessMiddleware(c *fiber.Ctx) error {
    user := c.Locals("user").(*User)
    if user.Role != "super_access" && user.Role != "owner" {
        return c.Status(403).JSON(fiber.Map{
            "error": "forbidden",
            "message": "Super Access role required",
        })
    }
    // Log access for audit
    logSuperAccessAction(user.ID, c.Path(), c.Method())
    return c.Next()
}
```

#### 1.5.3 Audit Log — обязательное логирование

Каждое действие Super Access логируется:

```sql
CREATE TABLE super_access_audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) NOT NULL,
    action TEXT NOT NULL,           -- 'view_chat', 'search', 'ai_query', 'export'
    target_type TEXT,               -- 'chat', 'user', 'message', 'report'
    target_id TEXT,                 -- ID объекта
    query TEXT,                     -- поисковый запрос или AI prompt
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_super_audit_user ON super_access_audit_log(user_id, created_at DESC);
CREATE INDEX idx_super_audit_time ON super_access_audit_log(created_at DESC);
```

### 1.6 Доступ к зашифрованным DM — техническая реализация

#### Подход A: Escrow Key (рекомендуемый)

```
Нормальный E2E поток:
Alice → encrypt(message, Bob's key) → сервер → Bob → decrypt

С Escrow Key:
Alice → encrypt(message, Bob's key) → сервер
                ↓
        encrypt(session_key, Escrow Public Key) → compliance storage
                ↓
Super Access → Escrow Private Key → decrypt session_key → decrypt messages
```

**Реализация:**
1. При создании E2E сессии, клиент шифрует Session Key публичным Escrow Key компании
2. Зашифрованный Session Key хранится в отдельной таблице `compliance_keys`
3. Escrow Private Key хранится в HSM или у CEO (не на сервере!)
4. При Super Access запросе: Escrow Key расшифровывает Session Key → расшифровка сообщений

```sql
CREATE TABLE compliance_keys (
    chat_id UUID NOT NULL,
    user_id UUID NOT NULL,
    device_id TEXT NOT NULL,
    encrypted_session_key BYTEA NOT NULL,  -- зашифровано Escrow Public Key
    session_version INT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (chat_id, user_id, device_id, session_version)
);
```

#### Подход B: Dual Delivery (проще, менее безопасно)

```
Alice → encrypt(message, Bob's key) → сервер (E2E для Bob)
                ↓
        plaintext copy → compliance storage (для Super Access)
```

**Реализация:**
- Каждое сообщение сохраняется дважды: E2E encrypted + plaintext
- Plaintext доступен только через Super Access API
- Проще, но менее безопасно (plaintext на сервере)

#### Рекомендация

**Подход A (Escrow Key)** для DM, **Подход B (Dual Delivery)** для групп/каналов (которые и так не E2E encrypted в начале).

### 1.7 AI Pipeline для аналитики

```
Messages DB (plaintext/decrypted)
        ↓
    Embeddings (Claude / OpenAI Embeddings)
        ↓
    Vector DB (pgvector / Qdrant)
        ↓
    ┌─────────────────────────────────────┐
    │           AI Analysis Layer          │
    │                                      │
    │  Sentiment Analysis (per message)    │
    │  Topic Modeling (LDA / Claude)       │
    │  Keyword Detection (real-time)       │
    │  Anomaly Detection (behavior)        │
    │  Summarization (Claude API)          │
    └─────────────────────────────────────┘
        ↓
    Super Access Dashboard
```

**Scheduled jobs:**
- Каждый час: обновление sentiment scores
- Каждый день: AI daily digest generation
- Real-time: keyword alerts (через NATS subscription)
- По запросу: AI query (Claude API, SSE streaming)

### 1.8 Юридическая база

| Аспект | Обоснование |
|--------|-----------|
| **Право собственности** | Корпоративный мессенджер = собственность компании. Все данные принадлежат MST |
| **Согласие сотрудников** | Подписывается при onboarding. "Коммуникации в корпоративном мессенджере могут быть просмотрены руководством в целях compliance" |
| **Аналоги** | Microsoft 365 Compliance, Slack Enterprise Grid, Google Vault — стандартная практика |
| **Audit trail** | Каждое действие Super Access логируется. Кто, когда, что смотрел |
| **Ограничение доступа** | Только 1-3 человека с ролью Super Access. Не IT-администраторы |

### 1.9 Зависимости

| Зависимость | Фаза | Почему нужно |
|-------------|------|-------------|
| AI Service | Phase 8 | Claude API для анализа |
| Full-text search | Phase 4 | Meilisearch для поиска |
| E2E Encryption | Phase 7 | Escrow Key mechanism |
| User Management | Phase 1 | Роли, auth |
| PostgreSQL | Phase 1 | Хранение данных |
| pgvector | Phase 8+ | Embeddings для семантического поиска |

### 1.10 Оценка трудозатрат

| Компонент | Дни |
|-----------|-----|
| Backend: Super Access API (15 endpoints) | 5 |
| Backend: AI Pipeline (embeddings, sentiment) | 5 |
| Backend: Escrow Key mechanism | 3 |
| Backend: Audit logging | 1 |
| Frontend: Dashboard UI (6 страниц) | 7 |
| Frontend: AI Query interface (SSE) | 3 |
| Testing & QA | 3 |
| **Итого** | **~27 дней** |

---

## 2. AI Meeting Notes

### 2.1 Описание

Автоматическая запись, транскрипция и AI-протоколирование каждого звонка (voice/video). После звонка — структурированный протокол с action items автоматически постится в чат.

### 2.2 Функциональные требования

| # | Функция | Приоритет | Описание |
|---|---------|-----------|---------|
| 2.1 | Auto-record | Must | Запись аудио звонка (с согласия участников) |
| 2.2 | Transcription | Must | Whisper API → текст в реальном времени |
| 2.3 | Speaker diarization | Must | Определение кто говорит (по голосу) |
| 2.4 | AI Summary | Must | Claude → структурированный протокол |
| 2.5 | Action items extraction | Must | "Ivan: сделать отчёт до пятницы" |
| 2.6 | Timestamps | Should | Клик на пункт → перемотка на момент в записи |
| 2.7 | Search | Should | Полнотекстовый поиск по всем транскрипциям |
| 2.8 | Share | Must | Протокол постится в чат группы автоматически |
| 2.9 | Edit | Should | Участники могут исправить транскрипцию |
| 2.10 | Export | Should | PDF / Markdown экспорт протокола |

### 2.3 UI спецификация

#### После завершения звонка — карточка в чате:

```
┌────────────────────────────────────────────────┐
│  📞 Звонок "Планёрка" — 24 марта 2026          │
│  Участники: Alex, Ivan, Maria                   │
│  Длительность: 32 мин                           │
│                                                  │
│  📝 AI Протокол:                                │
│                                                  │
│  **1. Статус проекта X** (0:02 - 8:15)          │
│  Alex представил текущий прогресс — 80%          │
│  завершено. Основные блокеры: интеграция с       │
│  платёжной системой.                              │
│                                                  │
│  **2. Demo новой фичи** (8:15 - 18:40)          │
│  Ivan показал demo push-уведомлений.             │
│  Команда одобрила, Maria предложила добавить     │
│  настройки per-chat.                              │
│                                                  │
│  **3. Дизайн ревью** (18:40 - 32:00)            │
│  Maria предложила редизайн профиля.              │
│  Обсудили цветовую схему. Решили использовать    │
│  gradient вместо solid.                           │
│                                                  │
│  ✅ Action Items:                                │
│  ☐ Ivan: допилить API до пятницы 28.03          │
│  ☐ Maria: подготовить мокапы к среде 26.03      │
│  ☐ Alex: согласовать бюджет на платёжку         │
│                                                  │
│  [🎙️ Аудио запись]  [📄 Полная транскрипция]     │
│  [📋 Экспорт PDF]   [✏️ Редактировать]           │
└────────────────────────────────────────────────┘
```

### 2.4 Backend

| Endpoint | Method | Описание |
|----------|--------|---------|
| `POST /calls/:id/record/start` | POST | Начать запись (с согласия) |
| `POST /calls/:id/record/stop` | POST | Остановить запись |
| `GET /calls/:id/recording` | GET | Аудио файл записи |
| `GET /calls/:id/transcript` | GET | Полная транскрипция |
| `GET /calls/:id/summary` | GET | AI протокол |
| `GET /calls/:id/action-items` | GET | Список action items |
| `PATCH /calls/:id/transcript` | PATCH | Редактировать транскрипцию |
| `POST /calls/:id/summary/regenerate` | POST | Перегенерировать протокол |

**Pipeline:**
```
Audio stream → Whisper API (real-time STT) → Speaker Diarization →
    → Raw Transcript → Claude API (summarize + extract actions) →
    → Structured Protocol → Post to chat
```

### 2.5 DB Schema

```sql
CREATE TABLE call_recordings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    call_id UUID REFERENCES calls(id) NOT NULL,
    r2_key TEXT NOT NULL,           -- аудио файл в R2
    duration_seconds INT,
    file_size_bytes BIGINT,
    format TEXT DEFAULT 'ogg',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE call_transcripts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    call_id UUID REFERENCES calls(id) NOT NULL,
    segments JSONB NOT NULL,         -- [{speaker, text, start_ms, end_ms}]
    full_text TEXT,                   -- для поиска
    language TEXT DEFAULT 'ru',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE call_summaries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    call_id UUID REFERENCES calls(id) NOT NULL,
    summary_text TEXT NOT NULL,
    action_items JSONB,              -- [{assignee, task, deadline}]
    topics JSONB,                    -- [{title, start_ms, end_ms, summary}]
    model TEXT DEFAULT 'claude-sonnet-4-6',
    created_at TIMESTAMPTZ DEFAULT NOW()
);
```

### 2.6 Оценка трудозатрат

| Компонент | Дни |
|-----------|-----|
| Backend: Recording pipeline (R2 storage) | 3 |
| Backend: Whisper integration (STT) | 3 |
| Backend: Speaker diarization | 2 |
| Backend: Claude summarization pipeline | 2 |
| Frontend: Recording consent UI | 1 |
| Frontend: Protocol card component | 2 |
| Frontend: Transcript viewer + editor | 2 |
| Testing | 2 |
| **Итого** | **~17 дней** |

---

## 3. Smart Notifications

### 3.1 Описание

AI анализирует каждое входящее сообщение и определяет приоритет уведомления. Важные — сразу со звуком, рутина — тихо, флуд — батчится.

### 3.2 Уровни приоритета

| Приоритет | Определение | Уведомление | Примеры |
|-----------|-----------|------------|---------|
| 🔴 **Urgent** | Требует немедленной реакции | Звук + вибрация + persistent banner | "Сайт лежит!", "@alex срочно!", "клиент ждёт ответ" |
| 🟡 **Important** | Важно, но не срочно | Звук + badge | Сообщение от руководителя, @mention, DM от ключевого контакта |
| 🟢 **Normal** | Обычная рабочая переписка | Badge (тихо) | Сообщения в рабочих чатах |
| ⚪ **Low** | Не требует внимания | Batched (раз в час) | Мемы, оффтоп, флуд-чаты, боты |

### 3.3 AI классификация

**Факторы для определения приоритета:**
1. **Отправитель** — руководитель/коллега/бот, частота общения
2. **Содержимое** — ключевые слова ("срочно", "баг", "deadline"), sentiment
3. **Контекст** — @mention, reply на ваше сообщение, DM vs группа
4. **Время** — рабочие часы vs нерабочие
5. **Поведение** — на что пользователь обычно отвечает быстро (ML)
6. **Чат** — приоритет чата (рабочий vs оффтоп)

### 3.4 Backend

| Endpoint | Method | Описание |
|----------|--------|---------|
| `GET /users/me/notification-priority` | GET | Текущие настройки AI приоритизации |
| `PUT /users/me/notification-priority` | PUT | Настройки (включить/выключить, чувствительность) |
| `GET /users/me/notification-priority/stats` | GET | Статистика классификации |
| `POST /users/me/notification-priority/feedback` | POST | Фидбек ("это было важно" / "это не важно") |

**Pipeline:**
```
New message → AI Classifier (lightweight, <50ms) → Priority level →
    → Push with priority flag → Client renders accordingly
```

### 3.5 Оценка: ~10 дней

---

## 4. Workflow Automations

### 4.1 Описание

No-code автоматизации внутри мессенджера. IF → THEN правила. Как Zapier/IFTTT, но встроенные.

### 4.2 Типы триггеров

| Триггер | Описание |
|---------|---------|
| **Message contains** | Сообщение содержит ключевое слово |
| **Message in chat** | Любое сообщение в конкретном чате |
| **@mention** | Кого-то упомянули |
| **New member** | Новый участник в группе |
| **User status** | Пользователь стал online/offline |
| **Schedule** | По расписанию (cron) |
| **Webhook** | Внешний HTTP вызов |
| **No response** | Нет ответа на сообщение N минут |
| **Keyword alert** | Ключевое слово в любом чате |

### 4.3 Типы действий

| Действие | Описание |
|---------|---------|
| **Send message** | Отправить сообщение в чат |
| **Forward** | Переслать в другой чат |
| **Add to group** | Добавить пользователя в группу |
| **Set reminder** | Напомнить через N минут |
| **Create task** | Создать задачу (Linear / Jira webhook) |
| **Send webhook** | HTTP POST на внешний URL |
| **Tag message** | Добавить тег к сообщению |
| **AI action** | "Перескажи", "Переведи", "Ответь" |
| **Notify Super Access** | Отправить alert в Super Access Dashboard |

### 4.4 UI: Visual Flow Builder

```
┌─────────────────────────────────────────────────┐
│  ⚡ Workflow: "Bug Alert"                        │
├─────────────────────────────────────────────────┤
│                                                  │
│  WHEN:                                           │
│  ┌──────────────────────────────┐               │
│  │ 📨 Message contains "баг"   │               │
│  │    in chat: #support         │               │
│  └──────────┬───────────────────┘               │
│             ↓                                    │
│  THEN:                                           │
│  ┌──────────────────────────────┐               │
│  │ 📤 Forward to #dev           │               │
│  └──────────┬───────────────────┘               │
│             ↓                                    │
│  AND:                                            │
│  ┌──────────────────────────────┐               │
│  │ 🤖 AI: "Создай тикет из     │               │
│  │    этого сообщения"          │               │
│  └──────────┬───────────────────┘               │
│             ↓                                    │
│  ┌──────────────────────────────┐               │
│  │ 🔗 Webhook: POST Linear API  │               │
│  └──────────────────────────────┘               │
│                                                  │
│  [Save] [Test] [Disable]                         │
└─────────────────────────────────────────────────┘
```

### 4.5 DB Schema

```sql
CREATE TABLE workflows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_id UUID REFERENCES users(id),
    name TEXT NOT NULL,
    description TEXT,
    trigger_type TEXT NOT NULL,
    trigger_config JSONB NOT NULL,
    actions JSONB NOT NULL,          -- [{type, config}] ordered
    is_active BOOLEAN DEFAULT TRUE,
    run_count INT DEFAULT 0,
    last_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE workflow_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID REFERENCES workflows(id),
    trigger_data JSONB,
    status TEXT DEFAULT 'success',   -- success/failed/skipped
    error TEXT,
    duration_ms INT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
```

### 4.6 Оценка: ~15 дней

---

## 5. Knowledge Base

### 5.1 Описание

AI автоматически собирает полезную информацию из чатов и создаёт searchable корпоративную базу знаний. Любой сотрудник может найти ответ на вопрос не спрашивая коллег.

### 5.2 Функциональные требования

| # | Функция | Приоритет |
|---|---------|-----------|
| 5.1 | Auto-collect: AI определяет "полезное" в чатах | Must |
| 5.2 | Manual pin: любое сообщение → "Добавить в KB" | Must |
| 5.3 | Categories: Dev, Marketing, HR, Finance, Onboarding | Must |
| 5.4 | Search: полнотекстовый + семантический | Must |
| 5.5 | @orbit-kb: бот ищет ответ в KB | Should |
| 5.6 | Onboarding mode: новичку показываются релевантные статьи | Should |
| 5.7 | Versioning: история изменений статей | Should |
| 5.8 | Permissions: per-article видимость | Should |

### 5.3 DB Schema

```sql
CREATE TABLE kb_articles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    category TEXT,
    tags TEXT[],
    source_message_id UUID REFERENCES messages(id),
    source_chat_id UUID REFERENCES chats(id),
    author_id UUID REFERENCES users(id),
    is_auto_generated BOOLEAN DEFAULT FALSE,
    embedding VECTOR(1536),          -- pgvector для семантического поиска
    view_count INT DEFAULT 0,
    helpful_count INT DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_kb_embedding ON kb_articles USING ivfflat (embedding vector_cosine_ops);
CREATE INDEX idx_kb_category ON kb_articles(category);
CREATE INDEX idx_kb_tags ON kb_articles USING gin(tags);
```

### 5.4 Оценка: ~12 дней

---

## 6. Live Translate

### 6.1 Описание

Каждое сообщение автоматически переводится на язык получателя. User A пишет на русском → User B (настройка: English) видит оригинал + перевод.

### 6.2 Функциональные требования

| # | Функция | Приоритет |
|---|---------|-----------|
| 6.1 | Auto-detect language | Must |
| 6.2 | Per-user target language setting | Must |
| 6.3 | Inline translation (под оригиналом) | Must |
| 6.4 | Channel auto-translate | Should |
| 6.5 | Voice → translate (transcribe + translate) | Should |
| 6.6 | "Don't translate" list (languages to skip) | Should |
| 6.7 | Translation memory (кэш для повторяющихся фраз) | Nice |

### 6.3 Backend

```
Message received → detect language →
    if (language ≠ recipient.preferredLanguage) →
        Claude API translate → cache → deliver with translation
```

| Endpoint | Method | Описание |
|----------|--------|---------|
| `PUT /users/me/settings/language` | PUT | Установить предпочитаемый язык |
| `POST /translate` | POST | Перевести текст (on-demand) |
| `GET /messages/:id/translation/:lang` | GET | Получить перевод сообщения |

### 6.4 Оценка: ~8 дней

---

## 7. Video Notes Pro

### 7.1 Описание

Запись экрана + камера (как Loom) прямо в мессенджере. До 5 минут. AI создаёт главы и транскрипцию.

### 7.2 Функциональные требования

| # | Функция | Приоритет |
|---|---------|-----------|
| 7.1 | Screen + camera recording (PiP) | Must |
| 7.2 | Duration up to 5 min | Must |
| 7.3 | Auto-transcription (Whisper) | Must |
| 7.4 | AI chapters/timestamps | Should |
| 7.5 | Speed control (1x/1.5x/2x) | Should |
| 7.6 | Drawing/annotation during recording | Nice |
| 7.7 | Reply with video note | Should |

### 7.3 Технология

```
Browser: getDisplayMedia() + getUserMedia() → MediaRecorder →
    → Blob → upload to R2 → Whisper → Claude chapters → message
```

### 7.4 Оценка: ~10 дней

---

## 8. Anonymous Feedback

### 8.1 Описание

Специальный тип канала с криптографически гарантированной анонимностью. Сервер НЕ МОЖЕТ определить автора сообщения.

### 8.2 Техническая реализация: Ring Signatures

```
Member 1 ─┐
Member 2 ─┤─→ Ring Signature ─→ Message
Member 3 ─┤     (proves: "I'm a member")
Member N ─┘     (hides: "which one")
```

Каждый участник канала может подписать сообщение ring signature которая доказывает что автор — один из участников, но не раскрывает кто именно.

### 8.3 DB Schema

```sql
CREATE TABLE anonymous_channels (
    chat_id UUID REFERENCES chats(id) PRIMARY KEY,
    ring_public_keys BYTEA[],        -- публичные ключи всех участников
    min_members INT DEFAULT 5,       -- минимум 5 человек (для анонимности)
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE anonymous_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_id UUID REFERENCES chats(id),
    ring_signature BYTEA NOT NULL,   -- ring signature (доказательство)
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
    -- НЕТ поля sender_id!
);
```

### 8.4 Оценка: ~12 дней

---

## 9. Status Automations

### 9.1 Описание

Статус сотрудника обновляется автоматически на основе контекста: календарь, время, звонок, фокус-режим.

### 9.2 Триггеры и статусы

| Триггер | Статус | Иконка |
|---------|--------|--------|
| В звонке Orbit | "На звонке" | 📞 |
| Google Calendar event | "На встрече до 15:00" | 📅 |
| Focus mode (OS) | "Глубокая работа" | 🎯 |
| Отпуск (HR система) | "В отпуске до 28.03" | 🌴 |
| Ночь (timezone) | "Не в сети" | 🌙 |
| Workday start/end | "На работе" / "Ушёл домой" | 🏢 / 🏠 |
| DND schedule | "Не беспокоить" | 🔇 |
| Custom | Любой текст | Любой emoji |

### 9.3 Интеграции

| Сервис | Как подключить |
|--------|---------------|
| Google Calendar | OAuth2 → read calendar events |
| Apple Calendar | CalDAV sync |
| Outlook | Microsoft Graph API |
| HR система | Webhook (отпуска, больничные) |

### 9.4 Оценка: ~8 дней

---

## 10. Team Pulse

### 10.1 Описание

AI Dashboard для HR и руководства. Метрики коммуникации без чтения содержимого переписок.

### 10.2 Метрики

| Метрика | Источник | Описание |
|---------|---------|---------|
| **Response time** | Timestamp diff | Среднее время ответа на сообщение |
| **Activity hours** | Message timestamps | Когда сотрудник активен (overwork detection) |
| **Communication graph** | Sender/receiver pairs | Кто с кем общается (сетевой граф) |
| **Isolation alert** | Activity frequency | "Иван не писал 3 дня" |
| **Burnout indicator** | After-hours activity | Рост активности в нерабочее время |
| **Team sentiment** | AI analysis of messages | Позитивность общения (trend) |
| **Collaboration score** | Cross-team messaging | Насколько отделы общаются |
| **Onboarding velocity** | New user activity ramp | Как быстро новичок интегрируется |
| **Meeting load** | Call frequency/duration | Перегрузка звонками |
| **Message volume** | Count per period | Объём коммуникации по отделам |

### 10.3 Alerts

| Alert | Условие | Действие |
|-------|---------|---------|
| 🔴 Isolation | 0 сообщений за 3 дня | Уведомить менеджера |
| 🟡 Burnout risk | >20% сообщений после 22:00 | Уведомить HR |
| 🟡 Conflict | Sentiment < 30% между двумя людьми | Уведомить менеджера |
| 🟢 Low engagement | Participation < 10% в рабочих чатах | Мягкое напоминание |

### 10.4 Оценка: ~15 дней

---

## 11. Orbit Spaces

### 11.1 Описание

Постоянные голосовые комнаты (как Discord voice channels). Зашёл — слышишь коллег. Как виртуальный офис.

### 11.2 Функциональные требования

| # | Функция | Приоритет |
|---|---------|-----------|
| 11.1 | Always-on voice rooms | Must |
| 11.2 | Presence indicators (кто в комнате) | Must |
| 11.3 | Quick join/leave (1 клик) | Must |
| 11.4 | Screen share in room | Should |
| 11.5 | Background noise suppression | Should |
| 11.6 | Room categories (Dev, Marketing, Coffee) | Must |
| 11.7 | Max participants per room | Should |
| 11.8 | Music bot integration | Nice |
| 11.9 | Text chat in room | Should |
| 11.10 | Auto-mute on join | Should |

### 11.3 Архитектура

```
Room "Dev Corner" (always-on):
    ├── SFU (Pion) — holds the audio stream mix
    ├── Participant: Alex (unmuted, speaking)
    ├── Participant: Ivan (muted)
    ├── Participant: Maria (unmuted)
    └── Screen share: Ivan's IDE
```

Использует тот же Pion SFU что и групповые звонки (Phase 6), но с persistent room state.

### 11.4 DB Schema

```sql
CREATE TABLE spaces (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    description TEXT,
    category TEXT,                    -- dev/marketing/social/general
    max_participants INT DEFAULT 50,
    is_active BOOLEAN DEFAULT TRUE,
    linked_chat_id UUID REFERENCES chats(id),  -- текстовый чат комнаты
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE space_participants (
    space_id UUID REFERENCES spaces(id),
    user_id UUID REFERENCES users(id),
    is_muted BOOLEAN DEFAULT TRUE,   -- muted by default on join
    is_screen_sharing BOOLEAN DEFAULT FALSE,
    joined_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (space_id, user_id)
);
```

### 11.5 Оценка: ~12 дней

---

## 12. Сводная таблица

### 12.1 Приоритизация

| # | Feature | Impact | Effort | ROI | Зависимости | Рекомендуемая фаза |
|---|---------|--------|--------|-----|-------------|-------------------|
| 1 | **Super Access** | 🔴 Critical | 27 дней | Уникальный | AI, Search, E2E | 9 (после Phase 8) |
| 2 | **AI Meeting Notes** | 🟡 High | 17 дней | Высокий | Calls, AI | 8 (AI) |
| 3 | **Smart Notifications** | 🟡 High | 10 дней | Высокий | AI | 8 (AI) |
| 4 | **Workflow Automations** | 🟡 High | 15 дней | Высокий | Bots | 8 (Bots) |
| 5 | **Knowledge Base** | 🟢 Medium | 12 дней | Средний | AI, Search | 9+ |
| 6 | **Live Translate** | 🟡 High | 8 дней | Высокий | AI | 8 (AI) |
| 7 | **Video Notes Pro** | 🟢 Medium | 10 дней | Средний | Media | 3 (Media) |
| 8 | **Anonymous Feedback** | 🟢 Medium | 12 дней | Средний | Crypto | 7 (E2E) |
| 9 | **Status Automations** | 🟢 Medium | 8 дней | Средний | Settings | 4 (Settings) |
| 10 | **Team Pulse** | 🟡 High | 15 дней | Высокий | Analytics | 9+ |
| 11 | **Orbit Spaces** | 🟢 Medium | 12 дней | Средний | Calls (SFU) | 6 (Calls) |
| | **ИТОГО** | | **146 дней** | | | |

### 12.2 Рекомендуемый порядок реализации

**Wave 1 (с Phase 3-6 — минимальные зависимости):**
1. Video Notes Pro (Phase 3 — Media)
2. Status Automations (Phase 4 — Settings)
3. Orbit Spaces (Phase 6 — Calls)

**Wave 2 (Phase 8 — AI/Bots):**
4. Live Translate
5. Smart Notifications
6. AI Meeting Notes
7. Workflow Automations

**Wave 3 (Phase 9+ — Advanced):**
8. Anonymous Feedback
9. Knowledge Base
10. Team Pulse
11. **Super Access** (финальная, самая мощная)

### 12.3 Общий timeline с Killer Features

| Этап | Что включает | Срок |
|------|-------------|------|
| **Phase 1-8** | Основной мессенджер | ~17 недель |
| **Wave 1** (параллельно) | Video Notes, Status Auto, Spaces | +3 недели |
| **Wave 2** (Phase 8) | AI фичи: Translate, Notifications, Meeting Notes, Workflows | +5 недель |
| **Wave 3** | Advanced: KB, Team Pulse, Anonymous, Super Access | +7 недель |
| **ИТОГО до полного продукта** | | **~32 недели** |

---

*Документ создан: 2026-03-24*
*Версия: 1.0*
*Orbit Messenger — Killer Features Technical Specification*
