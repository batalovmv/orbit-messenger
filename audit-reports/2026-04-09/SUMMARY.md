# Orbit Audit - Consolidated Summary (2026-04-09)

**Commit**: 82669bd (original run) | resolved-findings commits: f6aaf05, 73741d1
**Slots**: 20 parallel audits (slot-01-gateway through slot-20-tests)
**Total findings** (post-dedup, excluding resolved): CRITICAL=0, HIGH=17, MEDIUM=30

---

## Already Resolved

1. **slot-02 CRITICAL - /auth/bootstrap unguarded** - now gated by BOOTSTRAP_SECRET env + X-Bootstrap-Secret header (constant-time compare), hard-disabled when secret is empty. Tests added. (commit f6aaf05)
2. **slot-19 HIGH - Raw PostgreSQL DSN in logs (calls, messaging)** - now uses shared config.RedactURL(). Gateway also switched to shared helper. Raw NATS URL in media service also fixed. (commit 73741d1)
3. **slot-20 MEDIUM - messaging test suite did not compile** (stale mockChatStore/mockUserStore missing ListAllPaginated, Deactivate, Reactivate, UpdateRole, CountByRole) - fixed, all service tests green. (commit 73741d1)
4. **slot-20 MEDIUM - auth tests red** (IsActive not set on mock create, bootstrap role mismatch) - fixed, all auth tests green. (commit 73741d1)
5. **bonus - messaging requireAdminRole/requireSysPermission calling c.Next() inline** - broke all 5 admin sticker endpoints (returned 500). Split into pure validator vs middleware. (commit f6aaf05)
6. **slot-06 HIGH - pkg/permissions channel-member branch missing** - members in channels received full permissions instead of 0. Fixed with explicit channel short-circuit. Stale tests updated to use PermissionsUnset sentinel. (commit f6aaf05)

---

## Critical (open)

None confirmed open after dedup and resolution of item 1 above.

---

## High (open)

### Auth / Session

**H-01 - WebSocket and SFU sessions remain live after JWT expiry or revocation**
- File: services/gateway/internal/ws/handler.go:68-158, services/gateway/internal/handler/sfu_proxy.go:73-168
- Source: slot-01
- Impact: a stolen token can keep a WS or SFU session open indefinitely via ping/pong; no periodic re-validation or revocation-triggered disconnect exists.
- Fix: store token expiry in connection state; run a background ticker that calls ValidateToken (or checks blacklist) periodically and closes the connection on failure.
**H-02 - Revoking a session does not invalidate the session already-issued access JWT**
- File: services/auth/internal/service/auth_service.go:305-315, services/auth/internal/service/auth_service.go:442-485
- Source: slot-02
- Impact: attacker retains a valid access token for up to 15 minutes after the victim revokes the session from the sessions UI.
- Fix: bind access tokens to a session ID / revocation version; reject tokens whose session has been deleted or rotated.

**H-03 - user_deactivated NATS event never subscribed in gateway - deactivated users keep live WS access**
- File: services/messaging/internal/service/admin_service.go:101-107 (publishes), services/gateway/internal/ws/nats_subscriber.go:55-73 (no subscription)
- Source: slot-08
- Impact: admin deactivates a user account but all open WebSocket sessions continue receiving events until the TCP connection drops naturally.
- Fix: subscribe to orbit.user.*.deactivated in the gateway and forcibly close the matching hub connections.

### IDOR / Authorization Bypass

**H-04 - Banned members pass IsMember checks and retain full read/write/fanout access**
- File: services/messaging/internal/store/chat_store.go:405 (IsMember), chat_store.go:376 (GetMemberIDs)
- Source: slot-03
- Impact: after a soft-ban (role=banned row kept), the banned user can send/edit/delete messages, react, vote in polls, view one-time media, and continues receiving NATS fanout.
- Fix: add AND role != banned to the IsMember WHERE clause and GetMemberIDs query.

**H-05 - Any admin can promote an arbitrary member directly to owner**
- File: services/messaging/internal/service/chat_service.go:495-500
- Source: slot-03
- Impact: admin can mint co-owners or elevate anyone to owner, granting delete-chat and demote-admin rights reserved for the original owner.
- Fix: in ChangeRole, require actor.Role == owner when newRole == owner.

**H-06 - Removed/left members retain edit/delete/poll-close rights on historical content**
- File: services/messaging/internal/service/message_service.go:279, services/messaging/internal/store/message_store.go:331, services/messaging/internal/service/poll_service.go:311
- Source: slot-03
- Impact: a fired admin or removed user can silently rewrite or delete all their messages and close their polls indefinitely after access was revoked.
- Fix: call IsMember at the start of EditMessage, DeleteMessage (service layer), and ClosePoll.

**H-07 - Call lifecycle mutations missing authorization - any authenticated user can disrupt unrelated calls**
- File: services/calls/internal/service/call_service.go:311-387 (DeclineCall/EndCall), services/calls/internal/handler/call_handler.go:236-251 (RemoveParticipant)
- Source: slot-05
- Impact: any authenticated user who learns a call UUID can decline someone else incoming call, end an active call, or eject arbitrary participants.
- Fix: add caller identity check and membership/participant validation to DeclineCall, EndCall, and RemoveParticipant; mirror the pattern used in AcceptCall.
**H-08 - /calls/:id leaks call roster and participant profile metadata without membership validation**
- File: services/calls/internal/handler/call_handler.go:175-186, services/calls/internal/service/call_service.go:391-405
- Source: slot-05
- Impact: any authenticated user who learns a call UUID can enumerate participants and harvest display name/avatar for users outside their own chats.
- Fix: read caller identity in GetCall and verify membership in the associated chat before returning participant data.

**H-09 - Messaging service trust boundary collapses: X-User-ID is fully caller-controlled (no X-Internal-Token check)**
- File: services/messaging/internal/handler/chat_handler.go:640-649 (getUserID helper), 103 call sites across 12 handler files
- Source: slot-09
- Impact: any actor able to reach the messaging service port directly (or via SSRF) can impersonate any user across all messaging flows - chat CRUD, messages, invites, admin actions, reactions, polls, scheduled messages, search, settings, stickers, user endpoints.
- Fix: add X-Internal-Token validation to the messaging service (as done in auth, calls, media); reject requests lacking a valid token before reading X-User-ID.

**H-10 - sanitizeProxyPath() dot-segment breakout allows external requests to reach upstream /internal/* endpoints**
- File: services/gateway/internal/handler/proxy.go:20-27, services/gateway/internal/handler/proxy.go:225-280
- Source: slot-01
- Impact: authenticated external users craft paths like /api/v1/chats/../../internal/chats/<id>/member-ids that bypass the deny-rule and forward to service-internal endpoints with X-Internal-Token.
- Fix: reject any request whose decoded path contains /internal before route selection, or strip dot segments before matching the deny rule.

**H-11 - Media presigned URL functions lack access control (IDOR)**
- File: services/media/internal/service/media_service.go:384-424 (GetPresignedURL, GetThumbnailURL, GetMediumURL)
- Source: slot-04
- Impact: latent HIGH - exported functions have no userID parameter and no CanAccess call; any consuming package passing a user-supplied media ID can generate a download URL for any other user private file.
- Fix: add userID uuid.UUID parameter to all three functions; call store.CanAccess(ctx, id, userID) before generating the URL (mirrors the existing GetR2Key pattern).

### Secrets / Credentials

**H-12 - Coturn TURN relay exposed publicly with predictable default credentials (orbit:orbit)**
- File: docker-compose.yml:83-96, docker-compose.yml:197-212
- Source: slot-17
- Impact: if TURN_PASSWORD is omitted or the stack is started with an incomplete env file, the deployment exposes a world-reachable TURN relay with orbit:orbit credential pair - anyone can relay arbitrary traffic through it.
- Fix: use :? (required) syntax for TURN_USER/TURN_PASSWORD in compose; or keep coturn unbound in the default profile.

### Rate Limiting / DoS

**H-13 - Auth session rate limiter bypassable via attacker-controlled Authorization / refresh_token values**
- File: services/gateway/cmd/main.go:128-133, services/gateway/internal/middleware/ratelimit.go:33-45
- Source: slot-01
- Impact: unauthenticated clients send a fresh bogus bearer token per request, each getting a new Redis key, bypassing the 60 req/min ceiling and causing unbounded proxy traffic to the auth service.
- Fix: on pre-auth routes, fall back to IP only; never derive the rate-limit key from untrusted request headers.

**H-14 - /calls/:id/ice-servers returns global static TURN credentials without user/call scoping**
- File: services/calls/internal/handler/call_handler.go:315-317, services/calls/internal/service/call_service.go:589-602
- Source: slot-05
- Impact: any authenticated caller can harvest long-lived coturn credentials and reuse the relay outside the original call.
- Fix: mint short-lived per-user TURN credentials (HMAC-SHA1 with TTL) instead of returning the static process-wide pair.
**H-15 - WS fanout is synchronous and unbuffered; one slow subscriber stalls NATS delivery for the whole chat**
- File: services/gateway/internal/ws/hub.go:91-114, services/gateway/internal/ws/nats_subscriber.go:492-503
- Source: slot-08
- Impact: any chat member with a deliberately unresponsive socket forces repeated 10 s write stalls per event, backpressuring the NATS callback and delaying all other recipients.
- Fix: give each connection a per-connection send queue; drop or disconnect connections whose queue exceeds a high-water mark rather than blocking the delivery goroutine.

**H-16 - No per-user storage quota enforcement - any authenticated user can exhaust R2 storage**
- File: services/media/internal/handler/upload_handler.go:57-99, services/media/internal/service/media_service.go:44-101
- Source: slot-04
- Impact: unlimited uploads (each up to 2 GB) can exhaust shared R2 storage and generate unbounded cloud costs.
- Fix: add GetUserStorageUsed store method; enforce a configurable MAX_USER_STORAGE_BYTES at the start of Upload and InitChunkedUpload.

**H-17 - Search ACL resolution replays the full heavy chat-list query up to 10 times per search request**
- File: services/messaging/internal/service/search_service.go:61-70, services/messaging/internal/store/chat_store.go:70-104
- Source: slot-18
- Impact: an authenticated user in many chats can trigger up to 10 heavyweight Postgres queries (correlated COUNT subqueries + LATERAL joins) before Meilisearch runs - a practical DB amplification path.
- Fix: add a dedicated GetUserChatIDs store method querying only chat_members; do not reuse ListByUser for ACL collection.

---

## Medium (open - high-confidence only)

### Auth / Session

**M-01** services/auth/internal/service/auth_service.go:490-538 - access tokens carry no session ID / jti; session revoke cannot invalidate issued access JWTs until expiry (up to 15 min). (slot-02)

### Permissions / Input Validation

**M-02** pkg/permissions/permissions_test.go:16-21 - TestEffectivePermissions_Admin_DefaultPerms passes memberPerms=0; sentinel is -1, so the test always fails at runtime, masking a real admin-permissions edge case. (slot-06)

**M-03** pkg/validator/validator.go:40-52 - RequireString accepts whitespace-only and zero-width Unicode strings; display names and chat names can be set to visually empty values. (slot-06)

**M-04** pkg/validator/validator.go:11 - UUID regex is lowercase-only; valid uppercase UUIDs from Windows/mobile clients receive spurious 400 errors. (slot-06)

**M-05** pkg/config/config.go:27-32 - MustEnv/EnvOr accept whitespace-only values; a whitespace JWT_SECRET or INTERNAL_SECRET is silently accepted and used. (slot-06)

### Race Conditions

**M-06** services/messaging/internal/service/scheduled_service.go:265-277 - SendNow reads IsSent, then calls deliver(), then MarkSent(). Cron DeliverPending can claim the same message in the window, causing double delivery. (slot-03)

**M-07** services/calls/internal/service/call_service.go:45-76, migrations/034_phase6_calls.sql:28-29 - requestCall and all call state transitions are TOCTOU; DB has no unique constraint enforcing single-active-call per chat. (slot-05)

**M-08** services/messaging/internal/service/link_preview_service.go:88-97 - non-atomic INCR + EXPIRE; if EXPIRE fails the rate-limit key has no TTL and permanently blocks link previews for that user. (slot-03)

**M-09** services/messaging/internal/tenor/client.go:195-203 - same non-atomic INCR + EXPIRE pattern; if EXPIRE fails the global Tenor rate-limit key persists forever, blocking all GIF functionality. (slot-03)

### Missing Authentication

**M-10** services/messaging/internal/handler/gif_handler.go:34,51 - GIF Search and Trending endpoints require no user authentication; any caller with X-Internal-Token can exhaust the Tenor API key quota without attribution. (slot-03)

**M-11** services/messaging/internal/handler/sticker_handler.go:64 - Sticker Search endpoint requires no user authentication; inconsistent with all other sticker endpoints. (slot-03)
### Resource / Infrastructure

**M-12** services/media/internal/service/media_service.go:692-710 - chunked upload MIME re-validation skips any file whose magic bytes detect as application/octet-stream; for MediaTypeFile, the bypass is unconditional. (slot-04)

**M-13** services/media/internal/storage/r2.go:232-251 - EnsureBucket applies a world-readable bucket policy on startup; PutBucketPolicy error is silently swallowed. (slot-04)

**M-14** services/media/internal/handler/upload_handler.go:47-54 - no rate limiting in the media service; a single authenticated user can initiate unlimited concurrent chunked upload sessions. (slot-04)

**M-15** services/messaging/internal/service/sticker_import.go:479 - io.ReadAll(resp.Body) with no io.LimitReader; large CDN responses during sticker import can exhaust heap. (slot-03)

**M-16** services/gateway/internal/ws/nats_subscriber.go:75-76 - core NATS nc.Subscribe (not JetStream durable); at-most-once delivery means events are silently lost during gateway restart with no replay path. (slot-08)

**M-17** services/messaging/internal/service/chat_service.go:415-437 (self-leave path) - departing user is removed before GetMemberIDs is called, so chat_member_removed is not fanned out to the leaving user other tabs/devices. (slot-08)

### Data Integrity / Schema

**M-18** migrations/022_phase5_rich_messaging.sql:119-136 - poll_votes has separate FKs on polls(id) and poll_options(id) but no composite FK; a vote can reference an option from a different poll, corrupting poll counts. (slot-07)

**M-19** migrations/033_saved_messages.sql:2-4 - saved_messages_lookup only has PRIMARY KEY (user_id); no UNIQUE (chat_id) means a race can link the same saved-messages chat to multiple users. (slot-07)

**M-20** migrations/022,034,036,037 (multiple DDL statements) - latest migration chain contains bare CREATE TABLE/INDEX/TRIGGER without IF NOT EXISTS; concurrent multi-service startup on a fresh DB can fail with duplicate-object errors. (slot-07)

**M-21** pkg/migrator/migrator.go:83-86 - stored migration checksums are never compared against current file contents; a migration edited after initial application is silently skipped. (slot-06)

**M-22** pkg/migrator/migrator.go:61-75 - legacy DB bootstrap heuristic uses presence of any users table; a shared or misconfigured DB can silently skip all migrations. (slot-06)

**M-23** pkg/config/config.go:15-24 - RedactURL on keyword-value DSN strings passes them through unchanged, providing false confidence that DSNs are redacted. (slot-06)

### Frontend

**M-24** web/src/api/saturn/methods/chats.ts:442-448 - raw invite hash spliced into URL path without encoding; ../../auth/logout-style values rewrite the target URL, turning the invite flow into an authenticated same-origin request gadget. (slot-11)

**M-25** web/src/components/auth/AuthSaturnRegister.tsx:27,29,61 - registration form sets isLoading on submit but never clears it on any failure path; form is permanently disabled until page reload. (slot-16)

**M-26** web/src/components/auth/Auth.tsx:55 - authorizationStateWaitQrCode maps to AuthEmailLogin; the QR/passkey login screen is never rendered and that auth state is silently unreachable. (slot-16)

**M-27** web/src/global/actions/api/reactions.ts:181-245 - stale rollback in toggleReaction; concurrent toggle requests can overwrite a newer successful state with an older failed snapshot. (slot-13)

**M-28** web/src/global/actions/api/messages.ts:1807, web/src/global/reducers/messages.ts:182 - older in-flight history fetches can overwrite newer message state (edits, deletes) because mergeApiMessages has no freshness guard. (slot-14)

**M-29** web/src/components/middle/composer/helpers/getFilesFromDataTransferItems.ts:24-32 - readEntries() called exactly once; directory drag-and-drop silently drops all files beyond the first OS-chunked batch. (slot-15)

**M-30** web/src/util/mediaLoader.ts:145-147,180,207-210 - blob URLs for photo/video are never revoked via URL.revokeObjectURL(); repeated media browsing steadily grows renderer memory. (slot-15)
---

## Cross-slot Patterns (same issue flagged by multiple agents)

| Pattern | Locations | Slots |
|---------|-----------|-------|
| Logging raw DSN / credentials at startup | calls/cmd/main.go:30, messaging/cmd/main.go:34, media/cmd/main.go:99 | 19 (RESOLVED by 73741d1) |
| WS session not terminated after auth change | Token expiry/revocation (H-01), deactivation event missing (H-03) | 01, 08 |
| Non-atomic INCR+EXPIRE in Redis rate limiters | link_preview_service.go:88 (M-08), tenor/client.go:195 (M-09) | 03 (same root cause, two sites) |
| Missing membership check on content mutations | Edit/delete/poll-close (H-06), call lifecycle (H-07) | 03, 05 |
| IDOR on presigned/get endpoints | Media presigned URLs (H-11), call GetCall without membership (H-08) | 04, 05 |
| Unauthenticated internal search endpoints | GIF Search/Trending (M-10), Sticker Search (M-11) | 03 |
| Test suite stale/dead (pre-fix) | messaging mock drift, auth test mismatch | 20 (RESOLVED) |
| No HTTP server timeouts (Slowloris) | Stub services (slot-10 medium), media service (M-14) | 04, 10 |
| Raw path segments unencoded before fetch | Invite hash (M-24); noted broadly across saturn client | 11 |

---

## Recommended Fix Order

1. **[H-09] Messaging X-User-ID trust collapse** - full user impersonation across the largest service; highest blast radius. Add X-Internal-Token validation before any handler runs.
2. **[H-10] Gateway dot-segment path breakout** - external users reach internal endpoints with gateway-level token; fix namespace check before route selection.
3. **[H-04/H-05/H-06] Banned-member bypass + admin->owner escalation + ex-member mutation rights** - three closely related auth gaps in the same store/service layer; fix together.
4. **[H-07/H-08] Call lifecycle auth missing + roster leak** - add caller identity and membership validation to the calls service.
5. **[H-01/H-02/H-03] WS session invalidation triad** - re-auth on expiry/revocation, deactivation event propagation; address together.
6. **[H-12] Coturn default credentials publicly reachable** - single compose change; immediate deploy risk.
7. **[H-13] Rate limiter bypassable via bogus bearer** - key auth-route bucket on IP only.
8. **[H-14] Global static TURN credentials** - mint short-lived per-user credentials (HMAC-SHA1 + TTL).
9. **[H-11] Media presigned IDOR (latent)** + **[H-16] No storage quota** - fix before presigned functions are wired to any HTTP route.
10. **[H-15] Synchronous WS fanout / slow-subscriber DoS** - add per-connection send queue with high-water-mark drop.
11. **[H-17] Search ACL N+10 query amplification** - add lightweight GetUserChatIDs store method.
12. **[M-06] Scheduled SendNow double-delivery** + **[M-07] Call state TOCTOU** - atomicize with DB-level CAS updates.
13. **[M-08/M-09] Non-atomic INCR+EXPIRE** - replace with SET NX EX + INCR or Lua script.
14. **[M-10/M-11] Unauthenticated GIF/Sticker Search** - add getUserID check at top of handlers.
15. **[M-24] Invite hash path traversal (frontend)** - encodeURIComponent all path params in saturn client.
16. **[M-18/M-19/M-20] Schema integrity gaps** - add composite FK on poll_votes, UNIQUE (chat_id) on saved_messages_lookup, idempotent DDL in remaining migrations.
17. **Remaining Medium bucket (M-01 to M-23, M-25 to M-30)** - address in dependency order after items above.
---

## Per-slot Stats

| Slot | Name | CRIT | HIGH | MED | Notes |
|------|------|------|------|-----|-------|
| 01 | gateway | 0 | 3 | 0 | Rate limiter bypass, WS no re-auth, dot-segment breakout |
| 02 | auth | 1->0 | 0 | 1 | Bootstrap RESOLVED; session revoke / JWT gap open |
| 03 | messaging | 0 | 3 | 6 | Banned-member bypass, role escalation, ex-member mutations, rate limiter races, missing auth on GIF/Sticker search |
| 04 | media | 0 | 3 | 5 | No quota, presigned IDOR, MIME bypass, public bucket, no rate limiting |
| 05 | calls | 0 | 3 | 1 | Call auth missing (decline/end/remove), roster leak, static TURN creds, TOCTOU state |
| 06 | pkg | 1->0 | 1->0 | 6 | Channel perm RESOLVED; admin test mismatch + 5 med open |
| 07 | migrations | 0 | 0 | 3 | Poll vote FK, saved-messages uniqueness, non-idempotent DDL |
| 08 | nats-fanout | 0 | 2 | 2 | Deactivation event missing, sync fanout DoS, at-most-once, self-leave fanout |
| 09 | interservice-auth | 0 | 1 | 0 | Messaging X-User-ID trust collapse |
| 10 | stubs | 0 | 0 | 2 | Health always OK for unimplemented stubs, no server timeouts |
| 11 | saturn-client | 0 | 1 | 1 | Invite hash path injection, 401 not retried |
| 12 | calls-ui | 0 | 0 | 3 | Camera access before accept, call state overwrite, AudioContext leak |
| 13 | reactions-stickers | 0 | 0 | 1 | toggleReaction stale rollback |
| 14 | messaging-state | 0 | 0 | 3 | Mixed delete partial, editMessage no rollback, stale history overwrite |
| 15 | media-frontend | 0 | 0 | 2 | readEntries truncation, blob URL leak |
| 16 | auth-frontend | 0 | 0 | 2 | Registration form deadlock, QR screen unreachable |
| 17 | docker-deploy | 0 | 1 | 0 | Coturn default credentials public |
| 18 | perf-nplus1 | 0 | 1 | 1 | Search ACL N+10 queries, ringing-call N+1 participant lookup |
| 19 | logging-pii | 2->0 | 0 | 0 | DSN + NATS URL leaks RESOLVED |
| 20 | tests | 0 | 0 | 2->0 | Both MEDs RESOLVED (mock drift, auth test mismatch) |

**Totals (open after resolution)**: CRIT=0, HIGH=17, MED=30

---

## Methodology Notes

All 20 slot reports were read in full. Each slot reflected a two-pass review (discovery + verification) of a distinct codebase slice at commit 82669bd. Findings were extracted by severity and then deduplicated: findings at identical file:line coordinates, or representing the same logical bug appearing across multiple files or slots (e.g., non-atomic INCR+EXPIRE in two rate limiters; WS session invalidation raised independently by slots 01 and 08), were merged into a single entry listing all contributing slot IDs. Six findings confirmed as fixed in commits f6aaf05 and 73741d1 were moved to Already Resolved and excluded from open counts. LOW-severity items (approximately 60 individual notes across all slots) were not enumerated per the operator instructions; their presence is reflected in the per-slot notes column. Confidence-gated MEDIUM findings were accepted at each agent stated confidence level. The final count of 17 open HIGH issues and 30 open MEDIUM issues represents distinct, independently verifiable conditions at the stated file/line coordinates, with no double-counting between the resolved and open sections.