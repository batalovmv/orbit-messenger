# Slot 08 — NATS Fanout

## Status: COMPLETED

## Scope

- `services/gateway/internal/ws/`
- `services/messaging/internal/service/nats_publisher.go`
- `services/messaging/internal/service/` (NATS-related only)

## Focus Areas

- subject schema consistency (`orbit.chat.*`, `orbit.user.*`)
- `NATSEvent` envelope (`MemberIDs`, `SenderID` exclusion)
- Publisher interface usage (`never raw nc.Publish`)
- WS delivery to `member_ids`
- message ordering guarantees
- missed events on reconnect
- JetStream ack/nack
- backpressure on slow subscribers

## File Checklist

- [x] `services/gateway/internal/ws/events.go`
- [x] `services/gateway/internal/ws/handler.go`
- [x] `services/gateway/internal/ws/hub.go`
- [x] `services/gateway/internal/ws/hub_test.go`
- [x] `services/gateway/internal/ws/nats_subscriber.go`
- [x] `services/gateway/internal/ws/nats_subscriber_test.go`
- [x] `services/messaging/internal/service/admin_service.go`
- [x] `services/messaging/internal/service/chat_service.go`
- [x] `services/messaging/internal/service/chat_service_test.go`
- [x] `services/messaging/internal/service/invite_service.go`
- [x] `services/messaging/internal/service/message_service.go`
- [x] `services/messaging/internal/service/nats_publisher.go`
- [x] `services/messaging/internal/service/poll_service.go`
- [x] `services/messaging/internal/service/reaction_service.go`
- [x] `services/messaging/internal/service/scheduled_service.go`
- [x] `services/messaging/internal/service/user_service.go`

## Findings

### HIGH — `user_deactivated` never reaches the WS layer, so deactivated users keep live realtime access

**Evidence**

- `AdminService.DeactivateUser()` explicitly publishes `orbit.user.<userID>.deactivated` with comment `force-disconnect WebSocket` (`services/messaging/internal/service/admin_service.go:101-107`).
- Gateway subscriber never subscribes to that subject family. `Start()` only binds `orbit.user.*.status`, `orbit.user.*.mention`, chat/media/call subjects (`services/gateway/internal/ws/nats_subscriber.go:55-73`).
- There is no `user_deactivated`/`deactivated` handling anywhere under `services/gateway/internal/ws/`.
- WS auth happens once during upgrade, then the connection stays registered until the read loop breaks naturally (`services/gateway/internal/ws/handler.go:68-158`).

**Impact**

After an admin deactivates an account, already-open WebSocket sessions are not terminated and continue receiving chat/member/status events in real time. That is an authenticated post-deactivation access bypass until the socket drops or the token expires.

### HIGH — fanout is synchronous and unbuffered; one slow WS recipient can stall NATS delivery for the rest of the chat

**Evidence**

- NATS callbacks deliver member-targeted events inline via `s.hub.SendToUsers(...)` (`services/gateway/internal/ws/nats_subscriber.go:492-503`).
- `Hub.SendToUsers()` walks recipients serially, and `SendToUser()` walks each connection serially (`services/gateway/internal/ws/hub.go:91-114`).
- `Conn.Send()` writes directly to the socket with a `10s` deadline and no per-connection outbound queue (`services/gateway/internal/ws/hub.go:27-40`).
- On write failure, the hub only logs the error and keeps the connection registered (`services/gateway/internal/ws/hub.go:100-103`).

**Impact**

Any authenticated member with a deliberately unread/slow socket can force repeated `10s` write stalls on every event addressed to that user. Because fanout is inline, later recipients on the same event are delayed behind that write, and the NATS callback path backpressures instead of isolating the bad subscriber. This is a practical chat-level DoS vector.

### MEDIUM — realtime fanout is strictly at-most-once: no JetStream durable consumer, no ack/nack, no replay path on reconnect

**Evidence**

- Publisher uses plain `nc.Publish()` (`services/messaging/internal/service/nats_publisher.go:38-57`).
- Gateway subscriber uses plain core-NATS `nc.Subscribe()` (`services/gateway/internal/ws/nats_subscriber.go:75-76`).
- No JetStream consumer APIs, ack/nack handling, durable cursor, replay token, or resume logic exist anywhere in the reviewed scope.

**Impact**

If gateway is disconnected/restarting when events are published, those events are lost to the WS layer. Reconnecting clients have no server-side resume/replay mechanism in this path, so realtime gaps are expected during transient outages.

### MEDIUM — self-leave drops `chat_member_removed` for the departing user’s other tabs/devices

**Evidence**

- In the self-leave branch, the service removes the member first, then fetches `memberIDs`, then publishes `chat_member_removed` (`services/messaging/internal/service/chat_service.go:415-437`).
- That post-removal membership set no longer contains the leaving user, so their other active sessions are excluded from the envelope.
- Tests cover only “self-leave is allowed” (`services/messaging/internal/service/chat_service_test.go:697-724`) while the non-self removal path explicitly tests that the removed user stays in `MemberIDs` (`services/messaging/internal/service/chat_service_test.go:142-185`).

**Impact**

Leaving a chat from one device does not fan out the removal event to the same user’s other devices/tabs. They keep stale chat state until some later refresh/reload corrects it.

## Low Bucket

- `services/gateway/internal/ws/handler.go` still publishes typing/status events via raw `h.NATS.Publish(...)` instead of going through the shared publisher/envelope helper.
- Subject naming for pin events is internally consistent but awkward: `orbit.chat.<chatID>.message.message_pinned` / `.message.message_unpinned`.
- `ChatService` publishes some lifecycle/member events even after `GetMemberIDs()` failure, but gateway fallback fetches only message-update/read/reaction/poll/pin events (`services/gateway/internal/ws/nats_subscriber.go:643-656`), so transient membership lookup failures can silently drop lifecycle/member fanout.
- No tests in reviewed scope cover `user_deactivated` delivery/force-disconnect or slow-subscriber backpressure behavior.

## Pass 2 Verification

- Static pass 2 completed over the full scoped code above.
- `go test ./internal/ws` in `services/gateway` passed.
- `go test ./internal/service -run 'Test(CreateChat_NATS_ChatCreated|AddMembers_NATS_PerMemberEvent|RemoveMember_NATS_IncludesRemovedUser|DeleteChat_NATS_SentBeforeDeletion|SendMessage_MentionEntityCreatesUserMentionEvent)'` in `services/messaging` failed before execution because existing test mocks no longer satisfy `store.ChatStore` (`mockChatStore` is missing `ListAllPaginated` in `chat_service_test.go` / `invite_service_test.go`). This blocked runtime verification on the messaging package but does not affect the static findings above.
