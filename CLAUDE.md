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
| `.swarm/context.md` | Актуальный контекст Swarm (фаза, SME cache, patterns) | **Читай это ПЕРЕД началом работы** |
| `PHASES.md` | Чек-лист всех задач по фазам (~1200 строк) | Читай секцию текущей фазы с `offset` |
| `docs/TZ-ORBIT-MESSENGER.md` | Полное ТЗ v2.0 (архитектура, security, API, DB schema) | Референс при архитектурных решениях |
| `docs/TZ-KILLER-FEATURES.md` | 11 уникальных фич (146 дней, Waves 1-3) | Учитывай при проектировании расширяемости |
| `docs/TZ-PHASES-V2-DESIGN.md` | Детальный дизайн каждой фазы, Saturn API методы | Референс при реализации фаз |
| `.env.example` | Все env-переменные с описаниями | Копируй при добавлении новых env vars |
| `.swarm/knowledge.jsonl` | Knowledge base (lessons learned, patterns) | Используй `knowledge_recall` для поиска |

**Правило**: не дублируй содержимое этих документов в CLAUDE.md. Ссылайся и читай.

## Текущее состояние

> **Активная фаза: Phase 8D — Production Hardening**

| Phase | Status | Ключевое |
|-------|--------|----------|
| Phase 1-8 | Done | Все основные фичи реализованы |
| Phase 8D | In Progress (~21%) | Security audit done, metrics foundation |

## Расхождения ТЗ с реальностью

Эти пункты в документации **устарели** — НЕ реализуй их буквально:

| Документ | Что написано | Реальность |
|----------|-------------|------------|
| Phase 7 | Signal Protocol E2E | **Откачено**. At-rest AES-256-GCM only |
| TZ-ORBIT | superadmin/compliance | **Не реализовано** — только chat-level RBAC |
| TZ-ORBIT | channels | **Удалены** (migration 035) |
| Phase 8D | 4 NATS streams | **1 stream** ORBIT с 24h retention |
| Phase 8D | 5 Redis keys | **3 prefixes**: jwt_cache/jwt_blacklist/ratelimit |

При обнаружении новых расхождений — **обнови эту таблицу**.

## Архитектура: 8 Go-микросервисов

| Сервис | Порт | Зона ответственности |
|--------|------|---------------------|
| gateway | 8080 | API gateway, WebSocket hub |
| auth | 8081 | JWT, 2FA, sessions |
| messaging | 8082 | Messages, chats, reactions |
| media | 8083 | Upload, thumbnails, R2 |
| calls | 8084 | WebRTC signaling |
| ai | 8085 | Claude API |
| bots | 8086 | Bot API |
| integrations | 8087 | Webhooks |

### Shared packages

| Пакет | Назначение |
|-------|-----------|
| `apperror` | AppError type |
| `response` | JSON helpers |
| `config` | Env helpers |
| `permissions` | RBAC bitmask |
| `crypto` | AES-256-GCM |

## Правила разработки

### Обязательно

1. **Читай PHASES.md** перед работой — знай текущую фазу
2. **Читай `.swarm/context.md`** — актуальный контекст Swarm
3. **handler → service → store** — НЕ монолит
4. **Параметризованные SQL** — `$1, $2` ВСЕГДА
5. **IDOR prevention** — проверяй принадлежность
6. **Rate limiting** — на каждом endpoint
7. **Обработка ошибок** — НЕ `_ = err`
8. **Тесты** — handler тесты
9. **Секреты только через env**

### Запрещено

- `AllowOrigins: *` + `AllowCredentials`
- N+1 запросы
- `_ = someFunction()`
- Секреты в коммитах
- `go 1.25` (используй 1.24)
- Inline миграции
- HTTP client без timeout

## Код-конвенции Backend

### Структура сервиса

```
services/<name>/
├── cmd/main.go
├── internal/
│   ├── handler/
│   ├── service/
│   └── store/
├── go.mod
└── Dockerfile
```

### Database

- Таблицы: plural snake_case
- PK: `UUID DEFAULT gen_random_uuid()`
- Timestamps: `TIMESTAMPTZ DEFAULT NOW()`
- Миграции: `migrations/NNN_description.sql`
- Последняя миграция: **053**

## Память проекта — синхронизация

### Общие файлы (обе системы)

| Файл | Что содержит |
|------|-------------|
| `.swarm/context.md` | Фаза, SME cache, patterns |
| `.swarm/knowledge.jsonl` | Lessons learned |
| `PHASES.md` | Чек-лист задач |

### Правило синхронизации

**После завершения задачи:**
1. Обнови `.swarm/context.md`
2. Отметь `[x]` в `PHASES.md`
3. Добавь уроки в `knowledge.jsonl` через `knowledge_add`

### Claude Code -> читает

- `.swarm/context.md` — при старте
- `.swarm/knowledge.jsonl` — через поиск
- `PHASES.md` — текущая фаза

### OpenCode (Swarm) -> использует

- `.swarm/context.md` — авто.load
- `.swarm/knowledge.jsonl` — knowledge_recall
- `PHASES.md` — авто.read

Обе системы читают одни файлы → знания синхронизированы.

## Как запустить

```bash
# Вся инфра
docker compose up --build

# Отдельный сервис
cd services/auth && go run ./cmd/main.go

# Тесты
cd services/<name> && go test ./...

# Фронтенд
cd web && npm run dev
```

## Git-конвенции

- **Ветки**: `feat/phase-N-feature`, `fix/description`
- **Коммиты**: conventional commits на английском
- **PR**: через `gh pr create`

## Performance targets (SLO)

| Метрика | Цель |
|---------|------|
| Message delivery p99 | < 100ms |
| API response p95 | < 200ms |
| WebSocket connections | 500/instance |
| Message throughput | 1000 msg/sec |