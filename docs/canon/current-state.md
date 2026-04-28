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
- PostgreSQL (миграции до 068), NATS (1 stream `ORBIT`, 24h), Redis (3 префикса), R2
- Frontend: форк Telegram Web A на Teact, TypeScript 5.9 strict, Webpack 5

### Operator controls (мig 066)

- **Feature flags**: `pkg/featureflags/registry.go` — единый реестр известных ключей с уровнем экспозиции (`unauth/auth/admin/server_only`) и safety class. Админский CRUD: `PATCH /api/v1/admin/feature-flags/:key` (gate `SysManageSettings`, audit fail-closed). Клиент полит `/system/config` (auth) и `/public/system/config` (без JWT) с TTL-кэшем 30s в messaging.
- **Maintenance banner**: одна строка в `feature_flags(maintenance_mode)`. Метаданные `{message, block_writes, since, updated_by}`. Gateway middleware (`services/gateway/internal/middleware/maintenance.go`) при `block_writes=true` отклоняет POST/PUT/PATCH/DELETE с 503+`Retry-After` для не-superadmin; superadmin проходит сквозь. Веб-баннер рисуется через `MaintenanceBanner.tsx` поверх Main.
- **Audit log search**: `audit_store.AuditFilter.Q` — ILIKE по `action / target_type / target_id / actor.display_name / details::text / ip`. Backslash/`%`/`_` экранируются. На 150-user pilot хватает; pg_trgm/tsvector — отложено.
- **Утечка operator metadata**: `MaintenanceState` (full) с `since/updated_by` идёт ТОЛЬКО в админский `/admin/feature-flags`. Public + auth `/system/config` отдают `PublicMaintenanceState` (active/message/block_writes only). Locked test: `TestFeatureFlagService_PublicMaintenance_StripsOperatorMetadata`.

### Cross-device read-receipt sync (PR #13 + #14, 2026-04-28)

- **Новый NATS event** `orbit.user.<userID>.read_sync` (self-only). Messaging публикует после `MarkRead` payload `{chat_id, last_read_message_id, last_read_seq_num, unread_count, read_at, origin_session_id}` ТОЛЬКО на user'а-маркера. Существующий `orbit.chat.*.messages.read` (cross-user receipts) живёт параллельно с slim payload — без `unread_count`, чтобы не утекать другим участникам.
- **WS Conn carries SessionID** (per-tab, opaque, sessionStorage). Передаётся в auth-frame `{token, session_id}` и в `X-Session-ID` REST header. Сервер генерит UUID если клиент не прислал.
- **Hub.SendToUserExceptSession(userID, excludeSessionID, msg)** — primitive для cross-device фанаута (read-sync, в будущем typing/draft). `CountConnectionsExcluding` — gate "никто кроме origin не получил WS frame".
- **Offline silent push fallback** (Day 4b): если у user'а 0 non-origin WS connections И `unread_count == 0` → silent web-push с TTL=60s, urgency=low. Coalesce per `(userID, chatID)` 1.5s, latest-payload-wins, generation-counter защищает от Reset+queued-callback race. Bounded через `s.sem`. SW push handler `type=read_sync` → `closeNotifications` без UI.
- Frontend: `wsHandler.handleReadSync` → `updateChat(readState)` + `serviceWorker.postMessage('closeMessageNotifications', {chatId, lastReadInboxMessageId: seq_num})`.

## Открытые направления Phase 8D

См. memory `project_audit_2026_04_26.md` и `audits/FIX-PLAN.md` — 3 CRITICAL + 9 HIGH + 107 IMPORTANT findings, план разнесён по спринтам.

Известный долг: [`ts-debt.md`](ts-debt.md) — 22 `@ts-expect-error TODO(phase-8D-cleanup)` в 13 web-файлах, плюс ~298 stylelint warnings. TS-долг разгрести до Phase 9; lint — non-blocking, отдельным треком.

Параллельные backlog'и (deferred): WAL/PITR полный (`project_wal_pitr_backlog.md`), Smart Notifications (`project_smart_notifications_backlog.md`), SFU для group calls.

## Источники правды (вне этого файла)

- `PHASES.md` — детальный чек-лист задач
- `migrations/CHANGELOG.md` — история миграций
- `docs/canon/state.json` — машинно-читаемый снапшот
