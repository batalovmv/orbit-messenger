# Slot 03: Messaging Service â Deep Security Audit

## Scope
- `services/messaging/` â all ~75 Go files
- Focus: message CRUD, chat/group/channel logic, chat_members permissions bitmask checks, reactions (Unicode validation), stickers/polls/scheduled, sequence_number ordering, NATS publisher usage, slow mode, pinning, admin endpoints, sticker import, privacy settings, notifications
- Commit: `82669bd35a1568f24eff710b1dd0074342f12dff`

## Files Reviewed
All handler, service, store, model, search, tenor, and test files under `services/messaging/internal/` plus `services/messaging/cmd/main.go`.

## Progress Log
- Pass 1 (discovery): read every handler, service, store, model file in parallel batches
- Pass 2 (verification): re-read exact lines, traced callers via grep, cross-referenced store interfaces

---

## Findings

### FINDING-01 [HIGH] Banned members still pass IsMember checks â retain read/write/fanout access

**File:line:** `services/messaging/internal/store/chat_store.go:405`
**Category:** Authorization bypass
**Description:** `IsMember` returns `(true, member, nil)` for any row in `chat_members` regardless of role. The codebase soft-bans by setting role='banned' but keeps the row in `chat_members`. All permission gates that call `IsMember` (message send, edit, delete, reaction, poll vote, one-time media view) therefore remain open for banned users.
**Evidence:**
- `chat_store.go:405` â `IsMember` scans any `chat_members` row; no `role != 'banned'` filter
- `chat_store.go:376` â `GetMemberIDs` (used for NATS fanout) returns every `chat_members.user_id` including banned rows
- `chat_store.go:776` â the `GetCommonChats` query explicitly adds `cm1.role != 'banned' AND cm2.role != 'banned'`, proving the project understands the banned-row pattern but failed to apply it consistently
- `message_service.go:39`, `reaction_service.go:245`, `poll_service.go:211`, `message_store.go:276` â all call `IsMember`/`checkChatAccess` as the sole gate
**Verified:** Yes â grep confirmed no `role != 'banned'` filter in `IsMember`, `GetMemberIDs`, or `checkChatAccess`
**Impact:** After a moderator bans a user, that user retains: history read access, ability to send/edit/delete messages, react to messages, vote in polls, view one-time media, and continues receiving all NATS real-time events for the chat.
**Fix:** Add `AND role != 'banned'` to the `IsMember` WHERE clause and `GetMemberIDs` query. Alternatively, add a dedicated `IsBanned` check in `checkChatAccess` that returns Forbidden before any other check.

---

### FINDING-02 [HIGH] Any admin can promote a regular member directly to owner

**File:line:** `services/messaging/internal/service/chat_service.go:495`
**Category:** Privilege escalation
**Description:** `ChangeRole` only validates admin-targeting special cases (`newRole == 'admin'` or `target.Role == 'admin'`). A request with `newRole == 'owner'` targeting a non-admin member skips those branches and falls through to the generic `IsAdminOrOwner(actor.Role)` check at line 500, which any admin passes. The store then persists 'owner' verbatim.
**Evidence:**
- `chat_service.go:495-500` â switch on `newRole == 'admin'` / `target.Role == 'admin'`; no case for `newRole == 'owner'`
- `chat_store.go:602` â `UpdateMemberRole` stores supplied role with no validation
- `chat_service.go:343` â `DeleteChat` restricted to `role == 'owner'` only
- `chat_service.go:453` â only owner may demote admins
**Verified:** Yes â code trace confirms no owner-promotion guard for non-owner actors
**Impact:** Any admin can mint co-owners or elevate arbitrary members to owner, gaining the right to delete the chat and demote other admins â permissions the service explicitly reserves for the original owner.
**Fix:** In `ChangeRole`, add: if `newRole == 'owner'`, require `actor.Role == 'owner'` (only the owner can transfer ownership).

---

### FINDING-03 [HIGH] Removed/left members retain full mutation rights over historical messages and polls

**File:line:** `services/messaging/internal/service/message_service.go:279`, `services/messaging/internal/store/message_store.go:331`, `services/messaging/internal/service/poll_service.go:311`
**Category:** Authorization â missing membership re-check on mutation
**Description:** Edit, delete, and poll-close operations authenticate by author identity (`msg.SenderID == userID`) only. There is no current-membership check. A user who has left or been removed retains full ability to edit/delete their old messages and close their old polls indefinitely.
**Evidence:**
- `message_service.go:279` â `EditMessage`: checks `msg.SenderID == userID`, no membership gate
- `message_store.go:331` â `SoftDeleteAuthorized`: `WHERE m.sender_id =  OR ...`, no membership predicate
- `poll_service.go:311` â `ClosePoll`: `msg.SenderID == userID` allows close without membership
- `message_service.go:37-60` â `checkChatAccess` (used for read/send) includes `IsMember`, but `EditMessage` calls `messages.GetByID` then applies sender check directly, bypassing `checkChatAccess`
**Verified:** Yes â confirmed no `IsMember`/`GetMemberByChatAndUser` call in the edit/delete/close-poll paths
**Impact:** Post-removal content alteration: a fired admin or banned user can silently rewrite or delete all their messages and close polls they created, manipulating active chat history after access was revoked.
**Fix:** Add `IsMember` check at the start of `EditMessage`, `DeleteMessage` (service layer), and `ClosePoll`. For delete, the admin-delete path should be gated on `checkChatAccess` with the `CanDeleteMessages` permission.

---

### FINDING-04 [MEDIUM] GIF Search and Trending endpoints require no authentication

**File:line:** `services/messaging/internal/handler/gif_handler.go:34`, `gif_handler.go:51`
**Category:** Missing authentication
**Description:** `Search()` (line 34) and `Trending()` (line 51) handlers never call `getUserID()`. Any caller with a valid `X-Internal-Token` â shared across all internal services â can hammer these endpoints and exhaust the Tenor API key quota without attribution.
**Evidence:**
- `gif_handler.go:34` â Search handler body: no `getUserID` call
- `gif_handler.go:51` â Trending handler body: no `getUserID` call
- Other handlers in the same file (`ListSaved`, `SaveGIF`, `RemoveGIF`) correctly call `getUserID(c)` and return 401 on failure
- `tenor/client.go:189` â global server-side rate limit is 100 requests/minute; no per-user limit, no attribution
**Verified:** Yes
**Impact:** Any internal service (or compromised service) can enumerate GIF results or flood Tenor API calls without attribution, burning API quota and potentially causing 429s for legitimate users.
**Fix:** Add `if _, err := getUserID(c); err != nil { return response.Error(c, apperror.Unauthorized('Authentication required')) }` at the top of both handlers.

---

### FINDING-05 [MEDIUM] Sticker Search endpoint requires no authentication

**File:line:** `services/messaging/internal/handler/sticker_handler.go:64`
**Category:** Missing authentication
**Description:** The `Search()` sticker handler does not call `getUserID()`. All other sticker endpoints (`ListInstalled`, `GetPack`, `ListRecent`, etc.) require auth. Only `Search` is open.
**Evidence:**
- `sticker_handler.go:64-78` â no `getUserID` call
- `sticker_handler.go:48` â `ListFeatured` requires `getUserID` (line 49)
- `sticker_handler.go:80` â `GetPack` requires `getUserID` (line 84)
**Verified:** Yes
**Impact:** Unauthenticated callers can enumerate all sticker packs via the search endpoint. Inconsistent with the rest of the sticker surface.
**Fix:** Add authentication check at line 64 before the query validation.

---

### FINDING-06 [MEDIUM] Link-preview rate limiter: INCR + EXPIRE are not atomic â key may persist without TTL

**File:line:** `services/messaging/internal/service/link_preview_service.go:88-97`
**Category:** Race condition in rate limiting
**Description:** The rate limiter does `INCR key` then, if `count == 1`, `EXPIRE key window`. The Expire error is silently swallowed. If the EXPIRE call fails or is lost, the key has no TTL and the window counter lives forever, permanently denying the user link previews.
**Evidence:**
- `link_preview_service.go:88` â `rdb.Incr(ctx, key)` (separate Redis command)
- `link_preview_service.go:90` â `if count == 1 {`
- `link_preview_service.go:92` â `rdb.Expire(ctx, key, rateLimitWindow)` â return error silently ignored
**Verified:** Yes
**Impact:** Under Redis instability, a user rate-limit counter could lose its TTL and permanently prevent link preview generation until the key is manually deleted.
**Fix:** Use `SET key 0 EX <window> NX` before `INCR`, or use a Lua script to atomically INCR and conditionally EXPIREAT.

---

### FINDING-07 [MEDIUM] Tenor rate limiter has the same non-atomic INCR + EXPIRE pattern

**File:line:** `services/messaging/internal/tenor/client.go:195-203`
**Category:** Race condition in rate limiting
**Description:** Same pattern as FINDING-06. Incr at line 195, then Expire only when count == 1 at line 201. If Expire fails, the global Tenor rate-limit counter has no TTL â permanently blocking all GIF functionality server-wide.
**Evidence:**
- `client.go:195` â `c.redis.Incr(ctx, key)`
- `client.go:200` â `if count == 1 { c.redis.Expire(...) }`
- `client.go:202` â Expire error only logged as Warn, not returned
**Verified:** Yes
**Impact:** Global Tenor rate-limit window could persist forever, blocking all GIF search/trending for all users.
**Fix:** Same as FINDING-06.

---

### FINDING-08 [MEDIUM] Sticker file download has no response body size limit â potential OOM

**File:line:** `services/messaging/internal/service/sticker_import.go:479`
**Category:** Resource exhaustion
**Description:** `DownloadFile()` calls `io.ReadAll(resp.Body)` with no `io.LimitReader` wrapper. Telegram sticker files are typically small (<1 MB), but this is not enforced. A large or malicious CDN response could exhaust heap memory and crash the service.
**Evidence:**
- `sticker_import.go:479` â `data, err := io.ReadAll(resp.Body)` â no size limit
- `sticker_import.go:504` â also uses `io.ReadAll` on media service response (error path)
**Verified:** Yes
**Impact:** Admin-triggered sticker pack import with a large Telegram file response could cause OOM crash. Risk is low (admin-only action, Telegram CDN trusted), but violates defense-in-depth.
**Fix:** Wrap: `io.LimitReader(resp.Body, 5*1024*1024)` (5 MB limit for sticker files).

---

### FINDING-09 [MEDIUM] SendNow races with cron DeliverPending â potential double delivery

**File:line:** `services/messaging/internal/service/scheduled_service.go:265-277`
**Category:** TOCTOU race condition
**Description:** `SendNow` reads `msg.IsSent` (line 265), calls `deliver()` (line 269), then calls `MarkSent()` (line 273). Between the read and the deliver call, the cron `DeliverPending` can atomically claim and deliver the same message. `SendNow` then delivers it a second time. `MarkSent` returns `pgx.ErrNoRows` (already marked), so `SendNow` surfaces an error â but double-delivery has already occurred.
**Evidence:**
- `scheduled_service.go:265` â `if msg.IsSent {` check (read, not atomic claim)
- `scheduled_service.go:269` â `s.deliver(ctx, *msg)` (action before claiming)
- `scheduled_service.go:273` â `s.scheduled.MarkSent(ctx, msgID)` â WHERE is_sent = false guard
- `scheduled_message_store.go:176-198` â `ClaimAndMarkPending` uses single atomic UPDATE with CTE + FOR UPDATE SKIP LOCKED
- `scheduled_message_store.go:160-174` â `MarkSent` returns ErrNoRows if already claimed; delivery already happened
**Verified:** Yes â race window confirmed between lines 265 and 269
**Impact:** Duplicate message delivery when a user calls SendNow near a cron tick for a message whose scheduled_at <= now. The cron runs every 10 seconds.
**Fix:** In `SendNow`, replace the read + conditional with an atomic claim: `UPDATE scheduled_messages SET is_sent=true, sent_at=NOW() WHERE id= AND sender_id= AND is_sent=false RETURNING *`. Only call `deliver()` if the UPDATE returns a row.

---

## Low Bucket

- **Test suite broken at build time**: Both `services/messaging/internal/service/mock_stores_test.go` and `services/messaging/internal/handler/mock_stores_test.go` are missing the `ListAllPaginated` method that was added to `store.ChatStore`. `go test ./...` in `services/messaging` fails at compilation.

- **Error string comparison** (`message_service.go:323`): `if err.Error() == 'forbidden'` instead of `errors.Is(err, store.ErrMessageForbidden)`. Functionally correct since the error is not wrapped, but fragile and inconsistent with the `errors.Is` calls on lines 116-120 in the same function.

- **Admin list endpoints: no handler-level upper bound on limit** (`admin_handler.go:42`, `admin_handler.go:64`): `c.QueryInt('limit', 50)` with no cap. Store enforces limit > 100 to 50, but the handler silently truncates with no indication to the caller.

- **Unpin requires owner/admin role but pin honors `CanPinMessages` bitmask**: `message_service.go:517` and `message_service.go:545` restrict unpin to owner/admin role checks, while `PinMessage` at `message_service.go:491` uses the `CanPinMessages` bitmask. A user granted `CanPinMessages` via bitmask can pin but not unpin â inconsistent UX.

- **Reaction validation only at handler layer**: `reaction_handler.go` validates emoji length and UUID-lookalike pattern, but `reaction_service.go:SetAvailableReactions` and the store accept arbitrary strings. Defense-in-depth requires validation in the service layer.

- **`GetUser` allows nil-UUID for unauthenticated viewer** (`user_handler.go:60`): `callerID, _ := getUserID(c)` silently passes nil UUID as viewer. Intentional for privacy-settings-based visibility, but undocumented.

- **Poll voters cursor is raw RFC3339Nano, not base64** (`poll_store.go`): Inconsistent with all other pagination cursors in the codebase which use base64-encoded values. Not a security issue but a consistency gap.

---

## Summary

| Severity | Count |
|----------|-------|
| HIGH     | 3     |
| MEDIUM   | 6     |
| LOW      | 7     |

The three HIGH findings share a root cause: `chat_members` permission gates use membership presence (`IsMember`) but not membership validity (banned status, current membership for mutations). The MEDIUM findings are independent: two rate-limiter atomicity gaps, one unbounded read, one delivery race, and two unauthenticated search/GIF endpoints.

## Status: COMPLETED
