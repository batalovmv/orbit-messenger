# Conventions

## Backend (Go)

### Обязательно

1. **handler → service → store** — никаких монолитных handler'ов с SQL.
2. **Параметризованные SQL** — `$1, $2, ...` ВСЕГДА. Никакой строковой склейки.
3. **IDOR prevention** — проверяй принадлежность ресурса пользователю/чату на каждом endpoint.
4. **Rate limiting** — на каждом публичном endpoint (Redis-based).
5. **Обработка ошибок** — НЕ `_ = err`. Логируй или возвращай. Используй `apperror`.
6. **Тесты** — handler-тесты обязательны для нового функционала.
7. **Секреты только через env** — никаких хардкоднутых ключей/паролей.
8. **HTTP client с timeout** — всегда. Никаких голых `http.DefaultClient` в проде.

### Запрещено

- `AllowOrigins: *` + `AllowCredentials: true` (CORS-дыра).
- N+1 запросы — батчи или JOIN.
- `_ = someFunction()` без причины.
- Секреты в коммитах.
- Inline миграции в коде сервиса — только `migrations/NNN_description.sql`.
- `go 1.25` в новых сервисах. Используй `1.24`. Исключение — `services/gateway` (см. `architecture.md`).

## Database

- Таблицы: plural snake_case (`users`, `chat_members`).
- PK: `UUID DEFAULT gen_random_uuid()`.
- Timestamps: `TIMESTAMPTZ DEFAULT NOW()`.
- Миграции: `migrations/NNN_description.sql`, нумерация монотонная. Текущий счётчик — см. `migrations/CHANGELOG.md`.

## Git

- **Ветки:** `feat/phase-N-feature`, `fix/description`, `docs/...`, `chore/...`.
- **Коммиты:** conventional commits на английском (`feat:`, `fix:`, `docs:`, `chore:`, `refactor:`).
- **PR:** через `gh pr create`.

## Язык

- Общение с пользователем (CTO режим): русский, неформально.
- Код, комментарии, commit-messages, PR-описания: английский.
- UI мессенджера: русский + английский (i18n).

## Запуск

```bash
docker compose up --build              # вся инфра
cd services/<name> && go run ./cmd/main.go   # отдельный сервис
cd services/<name> && go test ./...    # тесты
cd web && npm run dev                  # фронт
```

## Performance targets (SLO)

| Метрика | Цель |
|---------|------|
| Message delivery p99 | < 100ms |
| API response p95 | < 200ms |
| WebSocket connections | 500 / instance |
| Message throughput | 1000 msg/sec |
