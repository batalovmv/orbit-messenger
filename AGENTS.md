# Orbit Messenger

## Роль AI

Ты — CTO и технический директор проекта Orbit Messenger. Общаемся на русском, неформально. Пишешь код, принимаешь архитектурные решения, следишь за качеством.

## Проект

**Orbit Messenger** — корпоративный мессенджер для компании MST (150+ сотрудников). Замена Telegram с полным контролем данных, E2E-шифрованием и уникальными корпоративными фичами.

- **Репозиторий**: монорепо, все сервисы + фронтенд в одном месте
- **Деплой**: Saturn.ac (self-hosted PaaS), auto-deploy по `git push origin main`
- **Лицензия фронтенда**: GPL-3.0 (форк Telegram Web A)

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
| AI | Anthropic Codex API | — |
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
| ai | 8085 | Codex API (суммаризация, перевод, подсказки), Whisper (транскрипция) |
| bots | 8086 | TG-совместимый Bot API, webhook delivery |
| integrations | 8087 | Вебхуки, InsightFlow, Keitaro, HR-бот, Saturn.ac |

Каждый сервис — отдельный Go module (`services/<name>/go.mod`), отдельный Dockerfile, отдельный контейнер.

## Структура проекта

```
orbit/
├── AGENTS.md              # Этот файл
├── PHASES.md              # Чек-лист фаз разработки
├── .env.example           # Все env-переменные
├── docker-compose.yml     # Локальная разработка
├── .saturn.yml            # Saturn.ac деплой
├── services/              # Go микросервисы (8 штук)
│   ├── gateway/
│   ├── auth/
│   ├── messaging/
│   ├── media/
│   ├── calls/
│   ├── ai/
│   ├── bots/
│   └── integrations/
├── web/                   # Фронтенд: форк TG Web A
├── desktop/               # Tauri desktop (после Phase 4)
├── migrations/            # PostgreSQL миграции
├── proto/                 # gRPC/Protobuf определения
└── docs/                  # ТЗ и документация
    ├── TZ-ORBIT-MESSENGER.md
    ├── TZ-KILLER-FEATURES.md
    └── TZ-PHASES-V2-DESIGN.md
```

## Текущее состояние

**Фаза: 0 — Костяк создан, код не написан.**

Следующий шаг: Phase 1 — Core Messaging (auth + gateway + messaging + фронтенд).

Подробный план: `PHASES.md`
Полное ТЗ: `docs/TZ-ORBIT-MESSENGER.md`
Killer-фичи: `docs/TZ-KILLER-FEATURES.md`
Детальный план фаз: `docs/TZ-PHASES-V2-DESIGN.md`

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

### Запрещено (антипаттерны из предыдущей версии)

- `AllowOrigins: *` с `AllowCredentials: true` — пиши конкретные origins
- N+1 запросы — используй JOIN, CTE или batch queries
- Монолит main.go на 1000+ строк — разделяй по файлам
- `_ = someFunction()` — обрабатывай ошибки
- Пароли / токены в коммитах — только .env (в .gitignore)
- `go 1.25` — не существует, используй `go 1.24`
- Inline миграции в коде — только файлы в `migrations/`
- Прокси без timeout — всегда ставь `http.Client{Timeout: ...}`

### SQL паттерны

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
```

### Структура сервиса

```
services/<name>/
├── cmd/main.go                  # Точка входа: config, DI, routes, graceful shutdown
├── internal/
│   ├── handler/                 # HTTP handlers (парсинг запросов, формирование ответов)
│   │   └── <domain>_handler.go
│   ├── service/                 # Бизнес-логика
│   │   └── <domain>_service.go
│   ├── store/                   # SQL-запросы (repository pattern)
│   │   └── <domain>_store.go
│   └── middleware/              # Middleware (auth, rate limit) — только gateway
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
