# Current State

> Активная фаза: **Phase 8D — Production Hardening**

## Прогресс по фазам

| Phase | Status | Ключевое |
|-------|--------|----------|
| Phase 1 | Done | Auth (JWT, 2FA, sessions) |
| Phase 2 | Done | Messaging (chats, messages, reactions) |
| Phase 3 | Done | Media upload (R2, thumbnails) |
| Phase 4 | Done | Calls (WebRTC signaling, 1:1) |
| Phase 5 | Done | AI integration (Claude API) |
| Phase 6 | Done | Bots, integrations, webhooks |
| Phase 7 | Done | At-rest encryption (AES-256-GCM). Signal Protocol E2E **откачено** — см. `divergences.md` |
| Phase 8 | Done | RBAC (chat-level), audit log, full-text search (Meilisearch), reply keyboards |
| Phase 8D | In Progress (~21%) | Security audit done; metrics foundation; observability/PITR backlog |

## Что в проде

- 8 Go-микросервисов + Meilisearch + web — все на Saturn.ac, auto-deploy с `main`
- PostgreSQL (миграции до 065), NATS (1 stream `ORBIT`, 24h), Redis (3 префикса), R2
- Frontend: форк Telegram Web A на Teact, TypeScript 5.9 strict, Webpack 5

## Открытые направления Phase 8D

См. memory `project_audit_2026_04_26.md` и `audits/FIX-PLAN.md` — 3 CRITICAL + 9 HIGH + 107 IMPORTANT findings, план разнесён по спринтам.

Известный долг: [`ts-debt.md`](ts-debt.md) — 22 `@ts-expect-error TODO(phase-8D-cleanup)` в 13 web-файлах, плюс ~298 stylelint warnings. TS-долг разгрести до Phase 9; lint — non-blocking, отдельным треком.

Параллельные backlog'и (deferred): WAL/PITR полный (`project_wal_pitr_backlog.md`), Smart Notifications (`project_smart_notifications_backlog.md`), SFU для group calls.

## Источники правды (вне этого файла)

- `PHASES.md` — детальный чек-лист задач
- `migrations/CHANGELOG.md` — история миграций
- `docs/canon/state.json` — машинно-читаемый снапшот
