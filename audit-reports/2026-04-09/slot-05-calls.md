# Slot 05 Audit Report

## Scope
- Path: `services/calls/`
- Commit: `82669bd35a1568f24eff710b1dd0074342f12dff`
- Focus: WebRTC signaling, Pion SFU integration, coturn coordination, requestCall dedupe (`cb0646c`), network quality indicator, call rating, migration 037 idempotency (`d6b25e3`), ICE candidate validation, race conditions on call state transitions, TURN credentials scoping

## Status: COMPLETED
- Pass 1: completed
- Pass 2: completed
- Report: completed

## File Checklist
- [x] `services/calls/cmd/main.go`
- [x] `services/calls/internal/handler/call_handler.go`
- [x] `services/calls/internal/handler/mock_stores_test.go`
- [x] `services/calls/internal/handler/rate_call_test.go`
- [x] `services/calls/internal/handler/sfu_handler.go`
- [x] `services/calls/internal/model/models.go`
- [x] `services/calls/internal/service/call_service.go`
- [x] `services/calls/internal/service/nats_publisher.go`
- [x] `services/calls/internal/store/call_store.go`
- [x] `services/calls/internal/store/participant_store.go`
- [x] `services/calls/internal/webrtc/codec.go`
- [x] `services/calls/internal/webrtc/peer.go`
- [x] `services/calls/internal/webrtc/room.go`
- [x] `services/calls/internal/webrtc/sfu.go`
- [x] `services/calls/internal/webrtc/sfu_test.go`
- [x] `migrations/037_call_rating.sql` (read-only dependency for focus area)

## Findings
### HIGH — `/calls/:id/ice-servers` returns global TURN credentials without any call/user scoping
- Evidence:
  - `services/calls/cmd/main.go:36-38` loads one process-wide `TURN_USER` / `TURN_PASSWORD` pair from env.
  - `services/calls/internal/handler/call_handler.go:315-317` returns ICE servers without parsing `:id` and without reading `X-User-ID`.
  - `services/calls/internal/service/call_service.go:589-602` serializes the same static TURN credentials into every response.
- Impact: any caller that can reach this trusted route can harvest long-lived coturn credentials and reuse the relay outside the original call, which opens a real abuse/DoS-cost path and defeats the requested TURN credential scoping.
- Pass 2 verification: confirmed end-to-end across `main.go` + handler + service that there is no per-user, per-call or TTL-bound minting layer; the route parameter is unused.

### HIGH — Call lifecycle mutations are missing authorization checks, so any authenticated caller with a call ID can disrupt unrelated calls
- Evidence:
  - `services/calls/internal/service/call_service.go:311-340` (`DeclineCall`) changes call state with no membership/participant check.
  - `services/calls/internal/service/call_service.go:344-387` (`EndCall`) also updates state with no authorization check.
  - `services/calls/internal/handler/call_handler.go:236-251` (`RemoveParticipant`) never reads the caller identity at all.
  - `services/calls/internal/service/call_service.go:504-518` removes the target participant row and emits `call_participant_left` with no permission check.
- Impact: any authenticated user who learns a call UUID can decline someone else’s incoming call, end an active call, or eject arbitrary participants. That is an authenticated DoS / privilege-boundary break.
- Pass 2 verification: compared these paths with `AcceptCall` / `AddParticipant`, which do call/chat membership checks (`services/calls/internal/service/call_service.go:266-275`, `432-446`). The missing checks are specific to these mutating paths, not a generic service invariant.

### HIGH — `/calls/:id` leaks call roster and participant profile metadata without membership validation
- Evidence:
  - `services/calls/internal/handler/call_handler.go:175-186` serves `GetCall` without reading caller identity.
  - `services/calls/internal/service/call_service.go:391-405` returns the call plus active participants.
  - `services/calls/internal/store/participant_store.go:80-105` joins `users.display_name` and `users.avatar_url` into that participant list.
- Impact: any authenticated caller who learns a call UUID can enumerate who is currently in the call and collect profile metadata for users outside their own chats.
- Pass 2 verification: no compensating validation exists in handler/service/store, and there are no handler tests covering this path.

### MEDIUM — Request-call dedupe and call-state transitions are still TOCTOU, so concurrent requests can create duplicate live calls or contradictory end states
- Evidence:
  - `services/calls/internal/service/call_service.go:45-76` does `GetActiveForChat` and `Create` as two separate steps.
  - `migrations/034_phase6_calls.sql:28-29` adds only a non-unique partial index on active calls, so the database does not enforce single-active-call uniqueness.
  - `services/calls/internal/service/call_service.go:254-387` checks status in application code before changing it.
  - `services/calls/internal/store/call_store.go:85-96` executes `UPDATE calls ... WHERE id = $1` with no guard on the previous status.
- Impact: simultaneous `requestCall`/create requests can race past the dedupe check and insert parallel `ringing` rows for one chat; simultaneous accept/decline/end requests can all pass pre-checks and whichever update lands last wins, leaving the call state machine inconsistent.
- Pass 2 verification: schema + service/store path verified together. `go test ./...` passes, but `go test -race` could not be executed in this environment because `gcc` is missing, so this is source-level confirmation rather than runtime race-detector confirmation.

## Low Bucket
- `services/calls/internal/store/call_store.go:48-58` and all `SELECT` sites still omit `rating`, `rating_comment`, `rated_by`, `rated_at`, so migration 037 persists rating data that the API never reads back into `model.Call`.
- No handler tests cover the auth-sensitive paths identified above: `GetCall`, `GetICEServers`, `DeclineCall`, `EndCall`, `RemoveParticipant`.
- `services/calls/internal/store/participant_store.go:62-77` ignores `RowsAffected()` for mute/screen-share updates, and the service still emits NATS events afterward, so non-participant/self-stale updates can broadcast phantom media state changes.

## Notes
- Verified `migrations/037_call_rating.sql` itself is idempotent: every DDL uses `IF NOT EXISTS`.
- Verification commands:
  - `go test ./...` from `services/calls` ✅
  - `go test -race ./internal/webrtc ./internal/handler` ❌ environment limitation (`CGO_ENABLED=1` requires `gcc`, not present in PATH)
