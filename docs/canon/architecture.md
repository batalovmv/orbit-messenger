# Architecture

Orbit Messenger — корпоративный мессенджер для MST. Backend: Go-микросервисы, общая БД PostgreSQL, NATS (1 stream `ORBIT`, 24h retention), Redis, Meilisearch, R2 (object storage). Frontend: форк Telegram Web A на Teact (НЕ React).

## Сервисы

| Сервис | Порт | Go | Зона ответственности |
|--------|------|----|---------------------|
| gateway | 8080 | 1.25 | API gateway, WebSocket hub, routing |
| auth | 8081 | 1.24 | JWT, 2FA, sessions, RBAC |
| messaging | 8082 | 1.24 | Messages, chats, reactions, threads |
| media | 8083 | 1.24 | Upload, thumbnails, R2 |
| calls | 8084 | 1.24 | WebRTC signaling |
| ai | 8085 | 1.24 | Claude API integration |
| bots | 8086 | 1.24 | Bot API |
| integrations | 8087 | 1.24 | Webhooks |
| meilisearch | 7700 | — | Full-text search (getmeili/meilisearch:v1.7) |

`gateway` пин на Go 1.25 из-за embedded `nats-server/v2` в тестах (см. комментарий в `services/gateway/go.mod`). Все остальные новые сервисы — Go 1.24.

## Структура сервиса

```
services/<name>/
├── cmd/main.go
├── internal/
│   ├── handler/   # HTTP/WS handlers, request/response
│   ├── service/   # бизнес-логика
│   └── store/     # SQL, кеш
├── go.mod
└── Dockerfile
```

## Shared packages

| Пакет | Назначение |
|-------|-----------|
| `apperror` | AppError type, классификация ошибок |
| `response` | JSON helpers (success/error envelope) |
| `config` | Env helpers, типизированный загрузчик |
| `permissions` | RBAC bitmask (chat-level) |
| `crypto` | AES-256-GCM (at-rest encryption) |

## Деплой

Saturn.ac (self-hosted PaaS), auto-deploy по `git push origin main`. Каждый сервис + `web` + `meilisearch` — отдельный Saturn-компонент.
