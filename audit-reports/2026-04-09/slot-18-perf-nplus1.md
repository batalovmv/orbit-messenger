# Slot 18 — Perf / N+1 Audit

## Status: COMPLETED

## Scope

- Commit: `82669bd35a1568f24eff710b1dd0074342f12dff`
- Target: `services/**/internal/{store,service}/*.go`
- Focus: N+1 queries, missing JOINs, tree-like queries without CTE, prepared statement reuse, `pgxpool` config usage, Redis pipelining opportunities, WS broadcast backpressure, memory allocations in hot paths, unbuffered channels in fan-out
- Reporting gate: HIGH / CRITICAL individually, MEDIUM only with high confidence, LOW in bucket

## File Checklist

- [x] `services/auth/internal/service/auth_service.go`
- [x] `services/auth/internal/store/invite_store.go`
- [x] `services/auth/internal/store/session_store.go`
- [x] `services/auth/internal/store/user_store.go`
- [x] `services/calls/internal/service/call_service.go`
- [x] `services/calls/internal/service/nats_publisher.go`
- [x] `services/calls/internal/store/call_store.go`
- [x] `services/calls/internal/store/participant_store.go`
- [x] `services/media/internal/service/media_service.go`
- [x] `services/media/internal/service/processor.go`
- [x] `services/media/internal/store/media_store.go`
- [x] `services/messaging/internal/service/admin_service.go`
- [x] `services/messaging/internal/service/chat_service.go`
- [x] `services/messaging/internal/service/chat_service_test.go`
- [x] `services/messaging/internal/service/gif_service.go`
- [x] `services/messaging/internal/service/gif_service_test.go`
- [x] `services/messaging/internal/service/invite_service.go`
- [x] `services/messaging/internal/service/invite_service_test.go`
- [x] `services/messaging/internal/service/link_preview_service.go`
- [x] `services/messaging/internal/service/message_service.go`
- [x] `services/messaging/internal/service/message_service_test.go`
- [x] `services/messaging/internal/service/miniredis_test.go`
- [x] `services/messaging/internal/service/mock_phase5_stores_test.go`
- [x] `services/messaging/internal/service/mock_stores_test.go`
- [x] `services/messaging/internal/service/nats_publisher.go`
- [x] `services/messaging/internal/service/poll_service.go`
- [x] `services/messaging/internal/service/poll_service_test.go`
- [x] `services/messaging/internal/service/reaction_service.go`
- [x] `services/messaging/internal/service/reaction_service_test.go`
- [x] `services/messaging/internal/service/recording_publisher_test.go`
- [x] `services/messaging/internal/service/scheduled_service.go`
- [x] `services/messaging/internal/service/scheduled_service_test.go`
- [x] `services/messaging/internal/service/search_service.go`
- [x] `services/messaging/internal/service/settings_service.go`
- [x] `services/messaging/internal/service/sticker_import.go`
- [x] `services/messaging/internal/service/sticker_service.go`
- [x] `services/messaging/internal/service/sticker_service_test.go`
- [x] `services/messaging/internal/service/user_service.go`
- [x] `services/messaging/internal/store/audit_store.go`
- [x] `services/messaging/internal/store/chat_store.go`
- [x] `services/messaging/internal/store/gif_store.go`
- [x] `services/messaging/internal/store/invite_store.go`
- [x] `services/messaging/internal/store/media_message_store.go`
- [x] `services/messaging/internal/store/message_store.go`
- [x] `services/messaging/internal/store/poll_store.go`
- [x] `services/messaging/internal/store/reaction_store.go`
- [x] `services/messaging/internal/store/scheduled_message_store.go`
- [x] `services/messaging/internal/store/search_history_store.go`
- [x] `services/messaging/internal/store/settings_store.go`
- [x] `services/messaging/internal/store/sticker_store.go`
- [x] `services/messaging/internal/store/user_store.go`

## Pass 1 Notes

- Pattern-scanned all 51 `store/` and `service/` Go files in scope for loops with SQL/Redis/NATS inside, correlated SQL subqueries, dynamic query building, channel usage, and obvious hot-path allocation patterns.
- Deep-read candidate runtime paths after the sweep: `services/messaging/internal/service/search_service.go`, `services/messaging/internal/store/chat_store.go`, `services/messaging/internal/service/message_service.go`, `services/messaging/internal/store/message_store.go`, `services/messaging/internal/store/media_message_store.go`, `services/calls/internal/service/call_service.go`, `services/calls/internal/store/call_store.go`, `services/messaging/internal/service/nats_publisher.go`, `services/calls/internal/service/nats_publisher.go`.
- No unbuffered fan-out channels found in scope. The only channel hit was buffered concurrency limiting in `services/messaging/internal/service/sticker_import.go`.
- `pgxpool` sizing/idle timeout tuning was not assessable in this slot without leaving `store/` and `service/` scope.

## Pass 2 Verification

- Re-validated the search path end-to-end: `SearchMessages` / `SearchChats` call `userChatIDs`, and `userChatIDs` pages through `chatStore.ListByUser`; `ListByUser` is a full chat-list query with correlated `COUNT(*)` subqueries plus two `LATERAL` lookups, even though the caller only needs chat IDs.
- Re-validated the ringing-call expiry path end-to-end: `callStore.ExpireRinging` already returns the expired call rows in one query, but `CallService.ExpireRingingCalls` immediately performs `participants.ListByCall` once per expired call before publishing.
- Re-checked the forwarding path and kept it out of individual findings: the O(messages + attachments) round-trip shape is real, but within this slot I could not verify request-size caps outside `service/` and `store/`, so severity would be too speculative.

## Findings

### [HIGH] Search ACL resolution replays the full chat-list query up to 10 times per search request

- Evidence:
  - `services/messaging/internal/service/search_service.go:61-70` and `services/messaging/internal/service/search_service.go:123-135` call `userChatIDs` on every message/chat search request.
  - `services/messaging/internal/service/search_service.go:181-200` pages through `chatStore.ListByUser` in chunks of 200 until it accumulates as many as 2000 chats.
  - `services/messaging/internal/store/chat_store.go:70-104` shows `ListByUser` doing far more than ID lookup: correlated `COUNT(*)` for `member_count`, correlated unread counting over `messages`, plus `LATERAL` last-message and peer-user fetches.
- Why this matters:
  - An authenticated user who belongs to many chats can turn every search request into as many as 10 heavyweight Postgres queries before Meilisearch even runs.
  - This defeats the point of offloading search to Meilisearch and creates a straightforward DB amplification path for request spam.
- Fix direction:
  - Add a dedicated `GetUserChatIDs` store method that returns only IDs from `chat_members` (or a precomputed ACL source) and use that in `SearchMessages` / `SearchChats`.
  - Do not reuse `ListByUser` for ACL collection.

### [MEDIUM] Ringing-call expiry does an N+1 participant lookup during the periodic sweep

- Evidence:
  - `services/calls/internal/store/call_store.go:148-170` already expires and returns all ringing calls in one query.
  - `services/calls/internal/service/call_service.go:480-497` then loops over that result set and issues `participants.ListByCall` once per expired call before publishing `call_ended`.
- Why this matters:
  - The expiry worker is a background sweep, so this extra query cost is paid in a burst exactly when the system is already catching up on stale calls.
  - Under a backlog of many ringing calls, the worker shifts from one DB round-trip to 1 + N round-trips, which is a real and avoidable latency/load multiplier.
- Fix direction:
  - Batch-load participant user IDs for all expired call IDs in one query (`WHERE call_id = ANY($1)`) and build the per-call fan-out map in memory before publishing.

## Low Bucket

- `services/messaging/internal/store/message_store.go:430-468`: `CreateForwarded` does two SQL round-trips per forwarded message (`UPDATE chats ... RETURNING` plus `INSERT ... RETURNING`). If forwarding large batches is expected, reserve a sequence range once and bulk-insert from `UNNEST`.
- `services/messaging/internal/store/media_message_store.go:72-82` and `services/messaging/internal/store/media_message_store.go:175-192`: media-link creation/copy is still one `INSERT` per attachment. This is a clear batching opportunity via `UNNEST`, `SendBatch`, or `CopyFrom`.
- `services/messaging/internal/service/message_service.go:456-485`: forwarding also emits one NATS publish per created message, with repeated JSON marshal of the same recipient list. Keep only if the websocket contract truly requires per-message events.
- `services/messaging/internal/service/chat_service.go:400-407`: bulk member adds write members in batches, but post-write notification still loops and publishes one event per new member.
- `services/messaging/internal/store/chat_store.go:70-104`: correlated `member_count` / `unread_count` subqueries make `ListByUser` expensive even outside search. Acceptable for the actual chat list, but it is too heavy to be reused as a generic ACL/ID source.
- `services/messaging/internal/service/nats_publisher.go:33-56` and `services/calls/internal/service/nats_publisher.go:31-58`: publisher path is synchronous JSON marshal + `nc.Publish` with no explicit producer-side backpressure accounting in scope. I did not escalate this because consumer buffering and websocket drain logic live outside this slot.
