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

## CI

Saturn auto-deploy запускается на `git push origin main` и **не имеет отдельной test-стадии в `.saturn.yml`**. Гейт качества встроен в Docker-сборку:

- **Go-сервисы (`services/*/Dockerfile`)** — multi-stage: `FROM golang:1.24-alpine AS test` (для `gateway` — `1.25`) → `RUN go test ./... -count=1`. Затем `builder` делает `COPY --from=test`, и без зелёных тестов до `go build` дело не доходит, образ не собирается, Saturn не деплоит.
- **Frontend (`web/Dockerfile`)** — `RUN npm run check` (tsc + stylelint + eslint) перед `npm run build:production`. Падение проверок ломает сборку.

### Pre-commit drift checks

Локальный хук `scripts/hooks/pre-commit` (ставится через `bash scripts/install-hooks.sh`) ловит дешёвые ошибки до push:

1. `docs/canon/state.json.last_migration` совпадает с максимумом в `migrations/`.
2. Новые `@ts-ignore` / `@ts-expect-error` обязаны иметь `// TODO(...)`-комментарий.
3. `AGENTS.md`, `docs/canon/state.json`, `docs/canon/README.md` — tracked.

Хук не покрывает компиляцию/тесты — это делает Docker-стадия. Цель хука — не дать дрифту между canon и кодом утечь в main.

## Performance targets (SLO)

| Метрика | Цель |
|---------|------|
| Message delivery p99 | < 100ms |
| API response p95 | < 200ms |
| WebSocket connections | 500 / instance |
| Message throughput | 1000 msg/sec |
