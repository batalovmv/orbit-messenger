# Orbit Audit Run — 2026-04-09

**Commit hash (frozen)**: `82669bd35a1568f24eff710b1dd0074342f12dff`
**Branch**: `master`
**Working tree**: clean
**Agents**: 20 parallel codex sessions
**Severity gate**: only HIGH / CRITICAL reported individually; MEDIUM if 90%+ confident; LOW in bucket only.

Each agent owns exactly one file in this directory. No cross-writes.

## Slot roster

### Backend Go (10)
- [ ] [slot-01-gateway.md](slot-01-gateway.md) — `services/gateway/` — routing, WS hub, middleware, proxy, CORS/CSP, rate limiting, X-Internal-Token, push dispatcher
- [ ] [slot-02-auth.md](slot-02-auth.md) — `services/auth/` — JWT, Redis blacklist, bcrypt, 2FA TOTP, invites, sessions, refresh rotation
- [ ] [slot-03-messaging.md](slot-03-messaging.md) — `services/messaging/` — CRUD, chat/group, permissions bitmask, reactions, stickers, polls, scheduled, sequence ordering
- [ ] [slot-04-media.md](slot-04-media.md) — `services/media/` — R2 pipeline, presigned URLs, chunked upload, EXIF, thumbnails, SSRF, path traversal
- [ ] [slot-05-calls.md](slot-05-calls.md) — `services/calls/` — Pion SFU, signaling, coturn, requestCall dedupe, network quality, migration 037, ICE validation
- [ ] [slot-06-pkg.md](slot-06-pkg.md) — `pkg/` — apperror, response, config, validator, permissions, constant-time compare
- [ ] [slot-07-migrations.md](slot-07-migrations.md) — `migrations/` — indexes, FK, CASCADE, idempotency, soft-delete, constraints, enum TEXT consistency
- [ ] [slot-08-nats-fanout.md](slot-08-nats-fanout.md) — NATS subject schema, envelope, Publisher interface, WS delivery, JetStream, backpressure
- [ ] [slot-09-interservice-auth.md](slot-09-interservice-auth.md) — X-Internal-Token HMAC, X-User-ID trust, service mesh contracts, SSRF between services
- [ ] [slot-10-stubs.md](slot-10-stubs.md) — `services/ai/`, `services/bots/`, `services/integrations/` — stub readiness, dead routes, health checks

### Frontend (6)
- [ ] [slot-11-saturn-client.md](slot-11-saturn-client.md) — `web/src/api/saturn/` — fetch retry, auth refresh, token storage, cancellation, dedupe
- [ ] [slot-12-calls-ui.md](slot-12-calls-ui.md) — `web/src/components/calls/` + call reducers/actions — WebRTC client, quality indicator, rating, state machine
- [ ] [slot-13-reactions-stickers.md](slot-13-reactions-stickers.md) — reactions/stickers frontend — pointer-events, diff logic, URL rewriting, Lottie lifecycles
- [ ] [slot-14-messaging-state.md](slot-14-messaging-state.md) — message list, edit/delete, withGlobal memo, optimistic updates, pagination, scheduled
- [ ] [slot-15-media-frontend.md](slot-15-media-frontend.md) — MediaViewer, upload progress, chunked recovery, blob URL revocation, presigned refresh
- [ ] [slot-16-auth-frontend.md](slot-16-auth-frontend.md) — login/register/2FA/invite landing, session persistence, logout cleanup, XSS surface

### Infra / cross-cutting (4)
- [ ] [slot-17-docker-deploy.md](slot-17-docker-deploy.md) — `docker-compose.yml`, `.saturn.yml`, Dockerfiles, `.env.example` — secrets, healthchecks, non-root, build cache
- [ ] [slot-18-perf-nplus1.md](slot-18-perf-nplus1.md) — cross-service N+1 hunt, JOINs, pgxpool config, Redis pipelining, WS backpressure, allocations
- [ ] [slot-19-logging-pii.md](slot-19-logging-pii.md) — PII leakage in logs, structured logging consistency, error exposure, trace IDs, panic recovery
- [ ] [slot-20-tests.md](slot-20-tests.md) — handler test coverage, fn-field mock compliance, miniredis, test isolation, critical path gaps

## How to mark progress

When an agent writes `## Status: COMPLETED` to its file, tick the checkbox above manually (this index is maintained by the operator, not the agents).

## Post-run

After all slots complete, run a 21st aggregator agent to produce `SUMMARY.md` with:
- Top findings by severity across all slots
- Deduped cross-slot issues (things multiple agents flagged)
- Recommended fix priority order
