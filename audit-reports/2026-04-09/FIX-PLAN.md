# Orbit Audit Fix Plan — Autonomous Execution Contract

**Target commit at start**: `73741d1` (current master HEAD as of writing)
**Source of truth for findings**: `audit-reports/2026-04-09/SUMMARY.md`
**Per-task reports**: `audit-reports/2026-04-09/slot-*.md` (read only when the task block here is insufficient)

## Principles you must internalise before starting

1. **Orbit Messenger is an internal corporate messenger for MST (~150 trusted employees).** Treat users as non-adversarial by default; threats are accidental misuse and compromised credentials, not anonymous DDoS. Per-user quotas, anti-abuse heuristics, and trust-minimising defaults should be configurable but default OFF unless the task block says otherwise.
2. **Logout-all-on-deploy is acceptable.** When a fix invalidates existing JWTs (e.g., adding `jti`/session binding), ship it directly — no dual-validation migration layer. Users will re-login on next request. Do not design backwards-compat shims.
3. **Reliable delivery > silent drop.** When a design choice is between "drop events" and "disconnect the slow client so it can reconnect and replay", always choose disconnect. Corporate compliance requires audit trails.
4. **Project conventions in `CLAUDE.md` are non-negotiable.** Re-read the "Правила разработки", "Код-конвенции Backend", "Database конвенции", and "Security правила" sections before starting. Violations are bugs.
5. **Respect the current phase.** The project is in Phase 5 (Rich Messaging) stabilisation + Phase 6 (Calls) active. Calls code is hot and fragile — be extra careful around `services/calls/` and `web/src/components/calls/`.

## Non-negotiable operational rules

You are executing this plan in an unsupervised long-running session. Follow these rules exactly.

### Workflow — one task at a time

For each task in the order listed below:

1. **Read the task block fully.** If anything is unclear, read the referenced slot report. Do NOT skip to the next task until you understand the current one.
2. **Verify preconditions.** Working tree must be clean (`git status` empty); if dirty, stash or commit anything unexpected before starting (log it in PROGRESS.md first).
3. **Implement the change.** Edit only the files listed in the task scope. Do not touch adjacent code for "bonus" cleanup.
4. **Run the test gate.** Execute the exact command from the task's `Test gate` section. If it passes, continue. If it fails, follow the `On failure` clause.
5. **Commit the change.** Stage ONLY the files you edited (never `git add -A`). Use the commit message template from the task.
6. **Append to progress log.** Add a line to `audit-reports/2026-04-09/PROGRESS.md` with: timestamp, task ID, status (DONE / SKIPPED / FAILED), commit hash, one-line note.
7. **Move to the next task.** Do not stop, do not summarise in chat, do not ask for confirmation.

### Hard rules — violating these will cause real damage

- **NEVER** use `git push`, `git push --force`, `git rebase`, `git commit --amend`, `git reset --hard`, `git checkout .`, `git clean -f`, or any destructive git operation. Local commits only. The operator handles push.
- **NEVER** skip the test gate. A commit without passing tests is a broken commit.
- **NEVER** use `git add -A` or `git add .`. Always `git add <specific files>`.
- **NEVER** edit files outside the task's declared scope. If the task says `services/calls/`, do not touch `services/gateway/` even if you notice a related issue. Log it in PROGRESS.md as "OBSERVED:" and continue.
- **NEVER** edit `CLAUDE.md`, `PHASES.md`, `audit-reports/2026-04-09/SUMMARY.md`, or any existing `slot-*.md` file. PROGRESS.md is yours; FIX-PLAN.md is read-only.
- **NEVER** use `AskUserQuestion` or stop to ask for clarification. When uncertain, pick a reasonable default consistent with the Principles above and log your decision in PROGRESS.md as "DECISION:".
- **NEVER** introduce new external dependencies (`go get`, `npm install <new>`) without explicit mention in the task block. If a fix seems to require one, log as "BLOCKED: needs external dep <name>" and skip.
- **NEVER** run migrations against any real database. Migration fixes are file-level only (edit `.sql` files under `migrations/`). Migrator code changes go in `pkg/migrator/`.
- **NEVER** commit secrets. No API keys, no passwords, no tokens in files.

### Soft rules — prefer but deviate with logged reason

- **Prefer** Edit over Write for existing files (smaller diffs).
- **Prefer** Grep/Glob over Agent for simple lookups (cheaper).
- **Prefer** minimal test runs (one package) over full-service runs when the change is localised. Task blocks specify the minimum; use that unless you suspect cross-package impact.
- **Prefer** one commit per task. If a task explicitly calls out two sub-changes, you may do two commits.

### Stop conditions — when you may halt instead of continuing

The user wants uninterrupted progress. Halt only on these conditions:

1. **Go toolchain broken** — e.g., `go build` returns "go: not found" or "cannot find main module" on every service. Log "HALT: toolchain" and stop.
2. **Git broken** — e.g., `git commit` returns "not a git repository" or "fatal: unable to lock ref". Log "HALT: git" and stop.
3. **Filesystem full / permission denied** on writes. Log "HALT: fs" and stop.
4. **PROGRESS.md shows 5 consecutive SKIPPED or FAILED tasks** — something systemic is wrong; log "HALT: cascade failure" and stop.

Everything else — individual task failures, missing files, compile errors in non-scope packages — is a SKIP, not a halt. Log the reason, move to the next task.

### On failure — per-task clause

Each task has an `On failure` line. Three possible values:

- **`rollback and skip`** (most common): `git checkout -- <scope files>` to discard the change, log "SKIPPED: <reason>" in PROGRESS.md, move to next task.
- **`rollback and retry once`** (rare): discard the change, re-read the task block, attempt once more. If it fails the second time, skip.
- **`keep partial and log`** (very rare, only when the task block says so): leave the partial change uncommitted, log "PARTIAL: <reason>", move on. The operator will resolve.

If a task has no `On failure` line, default to **rollback and skip**.

### Progress log format

`audit-reports/2026-04-09/PROGRESS.md` is yours to maintain. Append only. Format:

```
## 2026-04-09T21:30:00Z TASK-01 DONE a1b2c3d
Added AND role != 'banned' filter to IsMember and GetMemberIDs. Tests green.

## 2026-04-09T21:45:12Z TASK-02 SKIPPED
reason: chat_service.go:495 layout changed since audit — guard point no longer exists as single site, needs refactor. rollback clean.

## 2026-04-09T22:01:33Z TASK-03 DECISION
Task-03 asked to pick TTL for HMAC TURN creds. Picked 2h as compromise between UX (re-minting) and token lifetime. Aligned with JWT refresh cadence.
```

Keep entries terse (2-4 lines). No decorative markdown. No emojis.

## Task format reference

```
## TASK-NN — <short title>

**Source**: <slot ID>, <severity>, <file:line from SUMMARY.md>
**Scope**: <paths you may edit — strict boundary>
**Depends on**: <TASK-NN or none>

### Why
<1-2 sentence summary of the bug>

### Change
<precise instruction on what to edit and how>

### Acceptance
- <verifiable bullet>
- <verifiable bullet>

### Test gate
`<exact shell command from the service/package dir>`

### Commit message
`<first line of commit>`

### On failure
<optional; default is "rollback and skip">
```

---

# TASKS

Ordered by a mix of risk (low-risk warmup first), dependency (auth/authz before calls that rely on it), and scope (backend before frontend, schema migrations in one chunk).

Current total: **47 tasks** (17 HIGH + 30 MEDIUM, all originally in SUMMARY.md).

---

## Phase 1 — Warmup (trivial pkg/ fixes to validate the workflow)

## TASK-01 — validator.RequireString: trim whitespace

**Source**: slot-06, M-03, `pkg/validator/validator.go:40-52`
**Scope**: `pkg/validator/validator.go`, `pkg/validator/validator_test.go` (create if missing)

### Why
`RequireString` currently accepts whitespace-only and zero-width Unicode strings, allowing visually empty display names and chat titles.

### Change
In `RequireString`, call `strings.TrimSpace(value)` before the length check. Use the trimmed value only for validation; do not mutate the caller's input (validator is pure). Add unit tests covering: whitespace-only, zero-width space (`\u200B`), tab-only, normal string with surrounding spaces (should still pass if trimmed length is within bounds).

### Acceptance
- `RequireString(" ", "name", 1, 10)` returns an AppError (previously passed).
- `RequireString("\u200B\u200B", "name", 1, 10)` returns an AppError.
- `RequireString("  hello  ", "name", 1, 10)` still passes (trimmed len = 5).
- Existing callers in services still build.

### Test gate
`cd pkg && go test ./validator/... ./config/...`

### Commit message
`fix(validator): trim whitespace in RequireString to reject visually empty strings`

## TASK-02 — validator UUID regex: accept uppercase

**Source**: slot-06, M-04, `pkg/validator/validator.go:11`
**Scope**: `pkg/validator/validator.go`, `pkg/validator/validator_test.go`

### Why
Current UUID regex is lowercase-only; valid uppercase UUIDs from Windows/mobile clients get rejected with 400.

### Change
Change the regex to case-insensitive (prepend `(?i)` or use `[0-9a-fA-F]`). Add a test with uppercase UUID input.

### Acceptance
- `RequireUUID("550E8400-E29B-41D4-A716-446655440000", "id")` passes.
- `RequireUUID("550e8400-e29b-41d4-a716-446655440000", "id")` still passes.
- `RequireUUID("not-a-uuid", "id")` still fails.

### Test gate
`cd pkg && go test ./validator/...`

### Commit message
`fix(validator): accept uppercase UUIDs (RFC 4122 is case-insensitive)`

## TASK-03 — config.MustEnv and EnvOr reject whitespace-only values

**Source**: slot-06, M-05, `pkg/config/config.go:27-32`
**Scope**: `pkg/config/config.go`, `pkg/config/parse_test.go`

### Why
A whitespace-only `JWT_SECRET` or `INTERNAL_SECRET` currently passes silently and gets used as the actual secret.

### Change
In `MustEnv`, after reading the env var, call `strings.TrimSpace` and panic if the trimmed value is empty. Return the trimmed value. In `EnvOr`, trim the env value; if the trimmed result is empty, return the fallback. Add test cases.

### Acceptance
- `MustEnv("X")` with `X="   "` panics.
- `EnvOr("X", "default")` with `X="   "` returns `"default"`.
- `MustEnv("X")` with `X="real-secret"` returns `"real-secret"`.

### Test gate
`cd pkg && go test ./config/...`

### Commit message
`fix(config): treat whitespace-only env values as unset`

## TASK-04 — config.RedactURL handles keyword-value DSN strings

**Source**: slot-06, M-23, `pkg/config/config.go:15-24`
**Scope**: `pkg/config/config.go`, `pkg/config/parse_test.go`

### Why
`RedactURL` returns keyword-value DSN strings (`host=... password=... user=...`) unchanged because `url.Parse` treats them as valid opaque URLs. This silently leaks the password while looking redacted in logs.

### Change
Before parsing, detect keyword-value form: if the input contains `password=` or `sslpassword=` and does not start with a scheme (`://`), use a regex to replace `password=<value>` → `password=***` and `sslpassword=<value>` → `sslpassword=***` (value terminates at whitespace or end of string). Otherwise fall through to the current URL parse path. Add tests for both forms.

### Acceptance
- `RedactURL("host=db.example.com port=5432 user=orbit password=s3cret dbname=orbit")` returns a string containing `password=***` and NOT `s3cret`.
- `RedactURL("postgres://orbit:s3cret@db.example.com/orbit")` still redacts via URL parse.
- `RedactURL("")` returns `""`.
- `RedactURL("not a url at all")` returns `"***"` (safe default).

### Test gate
`cd pkg && go test ./config/...`

### Commit message
`fix(config): RedactURL now handles keyword-value PostgreSQL DSNs`

---

## Phase 2 — Messaging authorisation cluster (biggest blast radius)

## TASK-05 — Verify messaging X-Internal-Token enforcement (H-09)

**Source**: slot-09, H-09, `services/messaging/cmd/main.go:274`, 103 handler sites
**Scope**: read-only on `services/messaging/`; write to PROGRESS.md only.

### Why
The audit flagged that `X-User-ID` is trusted without `X-Internal-Token` validation across all 12 messaging handler files. However, `cmd/main.go:274` registers `api := app.Group("", handler.RequireInternalToken(internalSecret))` — so the middleware IS applied at the group level. This task is a VERIFICATION pass to confirm the protection is real and not bypassed by any route registered outside that group.

### Change
Read `services/messaging/cmd/main.go` fully. For every `*.Register(*)` call, verify the router argument is `api` (the guarded group), NOT `app` directly. Use Grep to find any direct `app.Get/Post/Put/Delete` calls in `services/messaging/cmd/main.go` that bypass the middleware. Read `RequireInternalToken` in `services/messaging/internal/handler/` and confirm it performs constant-time compare and rejects missing tokens.

If everything is correctly guarded, log in PROGRESS.md as `TASK-05 VERIFIED — messaging X-Internal-Token enforced at group level, N handlers registered under api group, 0 direct app bindings found` and move on with NO commit.

If you find a gap (e.g., a handler registered on `app` directly, or RequireInternalToken skips the check for some paths), convert this task into a real fix: move the binding under `api`, or patch `RequireInternalToken`. Then commit with message `fix(messaging): enforce internal token on <specific handler>`.

### Acceptance
- PROGRESS.md entry confirms verification result.
- If a real gap was found, it is fixed, tests still green, committed.

### Test gate
`cd services/messaging && go build ./... && go test ./internal/handler`

### Commit message (only if gap found)
`fix(messaging): enforce X-Internal-Token on <handler>`

## TASK-06 — Banned members bypass IsMember (H-04)

**Source**: slot-03, H-04, `services/messaging/internal/store/chat_store.go:405` (IsMember), `chat_store.go:376` (GetMemberIDs)
**Scope**: `services/messaging/internal/store/chat_store.go`, `services/messaging/internal/store/chat_store_test.go` if exists, plus any existing test that exercises IsMember/GetMemberIDs.

### Why
`IsMember` returns true for a row with `role='banned'`, so banned users still pass authorisation gates in message, reaction, poll, and fanout code paths. `GetMemberIDs` similarly includes banned users in fanout recipient lists.

### Change
In `chat_store.go`, locate the `IsMember` SQL query and add `AND role != 'banned'` to the WHERE clause. Do the same for the `GetMemberIDs` query. Do NOT touch `GetMember` (it may be legitimately used to look up ban state) — only the membership-check queries. Read both functions fully to confirm the edit. If tests exist, add a case: insert a banned member, assert `IsMember` returns false and `GetMemberIDs` excludes them.

### Acceptance
- `IsMember` returns false for a row with `role='banned'`.
- `GetMemberIDs` excludes banned users.
- Existing tests still pass.

### Test gate
`cd services/messaging && go build ./... && go test ./internal/handler ./internal/service`

### Commit message
`fix(messaging): exclude banned members from IsMember and GetMemberIDs`

## TASK-07 — Admin cannot promote anyone to owner (H-05)

**Source**: slot-03, H-05, `services/messaging/internal/service/chat_service.go:495-500`
**Scope**: `services/messaging/internal/service/chat_service.go`, `services/messaging/internal/service/chat_service_test.go`

### Why
`ChangeRole` (or `UpdateMemberRole`, whichever the actual function name is around line 495) lets an `admin` caller set `newRole = "owner"`, effectively minting co-owners or transferring ownership. Only the current owner should be allowed to do this.

### Change
Find the role-update function around `chat_service.go:495`. Before the store call, add: if `newRole == "owner"` and `actor.Role != "owner"`, return `apperror.Forbidden("Only the chat owner can assign the owner role")`. Add a test: admin tries to promote member → 403; owner tries to promote member → success (mock returns nil).

### Acceptance
- Admin → owner promotion attempt returns 403.
- Owner → owner promotion still works (ownership transfer).
- Admin → admin and admin → member still work.
- Existing tests still pass.

### Test gate
`cd services/messaging && go test ./internal/service`

### Commit message
`fix(messaging): only owner can promote members to owner role`

## TASK-08 — Removed/left members cannot edit or delete their old content (H-06)

**Source**: slot-03, H-06, `services/messaging/internal/service/message_service.go:279` (EditMessage), `services/messaging/internal/store/message_store.go:331` (soft delete), `services/messaging/internal/service/poll_service.go:311` (ClosePoll)
**Scope**: `services/messaging/internal/service/message_service.go`, `services/messaging/internal/service/poll_service.go`, `services/messaging/internal/service/message_service_test.go`, `services/messaging/internal/service/poll_service_test.go`

### Why
`EditMessage`, `DeleteMessage` (service layer), and `ClosePoll` only verify that the caller is the original author. A fired admin or removed user retains the ability to rewrite or delete their old messages and close their polls indefinitely after access was revoked.

### Change
At the top of `EditMessage`, `DeleteMessage`, and `ClosePoll` (service layer, NOT store layer), add an `IsMember(ctx, chatID, callerID)` check BEFORE the author check. If not a member, return `apperror.Forbidden("Not a member of this chat")`. Combine with the existing `role != 'banned'` filter from TASK-06 — banned users also fail this check. Add tests: non-member tries to edit → 403; non-member tries to delete → 403; non-member tries to close poll → 403; normal member still works.

### Acceptance
- Ex-member cannot edit their old messages (403).
- Ex-member cannot delete their old messages (403).
- Ex-member cannot close polls they created (403).
- Current members can still edit/delete/close as before.

### Test gate
`cd services/messaging && go test ./internal/service`

### Commit message
`fix(messaging): require current chat membership for edit/delete/close-poll`

## TASK-09 — Unauthenticated GIF search and trending endpoints (M-10)

**Source**: slot-03, M-10, `services/messaging/internal/handler/gif_handler.go:34,51`
**Scope**: `services/messaging/internal/handler/gif_handler.go`, `services/messaging/internal/handler/gif_handler_test.go`

### Why
GIF Search and Trending handlers never call `getUserID(c)`, so any caller with `X-Internal-Token` can exhaust the Tenor API key quota without attribution. Inconsistent with the rest of the messaging API surface.

### Change
Add `if _, err := getUserID(c); err != nil { return response.Error(c, err) }` as the first statement of both handlers (Search around line 34, Trending around line 51). Follow the existing pattern from other handlers in the file. Add a test for each: missing `X-User-ID` → 401.

### Acceptance
- GIF Search without `X-User-ID` returns 401.
- GIF Trending without `X-User-ID` returns 401.
- Authenticated calls still work.

### Test gate
`cd services/messaging && go test ./internal/handler`

### Commit message
`fix(messaging): require user authentication on GIF search and trending`

## TASK-10 — Unauthenticated sticker search endpoint (M-11)

**Source**: slot-03, M-11, `services/messaging/internal/handler/sticker_handler.go:64`
**Scope**: `services/messaging/internal/handler/sticker_handler.go`, `services/messaging/internal/handler/sticker_handler_test.go`

### Why
Sticker Search handler does not call `getUserID(c)`, inconsistent with all other sticker endpoints in the same file.

### Change
Add the `getUserID` guard at the top of `Search`. Add a test for missing user context → 401.

### Acceptance
- Sticker Search without `X-User-ID` returns 401.
- Other sticker endpoints still work.

### Test gate
`cd services/messaging && go test ./internal/handler`

### Commit message
`fix(messaging): require user authentication on sticker search`

---

## Phase 3 — Gateway security

## TASK-11 — Gateway dot-segment path breakout (H-10)

**Source**: slot-01, H-10, `services/gateway/internal/handler/proxy.go:20-27`, `proxy.go:225-280`
**Scope**: `services/gateway/internal/handler/proxy.go`, `services/gateway/internal/handler/proxy_test.go`

### Why
`sanitizeProxyPath()` does not normalise dot segments before matching the deny rule for `/internal/*`. An external authenticated user can craft `/api/v1/chats/../../internal/chats/<id>/member-ids`, which passes the deny-rule check and forwards to service-internal endpoints with `X-Internal-Token`.

### Change
In `sanitizeProxyPath()`, use `path.Clean(decodedPath)` from the stdlib `path` package (NOT `filepath` — different separator semantics on Windows vs Linux) to normalise dot segments before the deny-rule check. After cleaning, reject any path containing the substring `/internal` (with a leading slash to avoid matching legitimate tokens like `marketing-internal` in legit paths). Add tests: `/api/v1/chats/../../internal/foo` → 404; `/api/v1/chats/legit` → forwards; `/api/v1/internal/foo` → 404.

### Acceptance
- Dot-segment breakout paths return 404 before being proxied.
- Legitimate paths still proxy correctly.
- No path crossing to internal endpoints is possible from external clients.

### Test gate
`cd services/gateway && go test ./internal/handler`

### Commit message
`fix(gateway): normalise dot segments in proxy path to block /internal breakout`

## TASK-12 — Auth session rate limiter bypass via bogus bearer (H-13)

**Source**: slot-01, H-13, `services/gateway/cmd/main.go:128-133`, `services/gateway/internal/middleware/ratelimit.go:33-45`
**Scope**: `services/gateway/cmd/main.go`, `services/gateway/internal/middleware/ratelimit.go`, `services/gateway/internal/middleware/ratelimit_test.go` if exists.

### Why
`authSessionRateLimitIdentifier` derives the rate-limit key from the caller-supplied `Authorization` header hash. An attacker sends a fresh bogus bearer per request, getting a new Redis key each time and bypassing the 60 req/min ceiling.

### Change
Change `authSessionRateLimitIdentifier` so that for pre-auth routes (login, register, refresh), it returns `"ip:" + c.IP()` only. For post-auth routes where the bearer is actually validated, the existing hash-based key can stay — but ONLY AFTER validation. Simplest: change the function to always return IP-based key for this bucket; rename to `authRateLimitIdentifierByIP` for clarity. Add a test: two requests from the same IP with different bogus bearer tokens must share the same rate-limit bucket.

### Acceptance
- Two requests with different bogus bearers from same IP count as two hits against one bucket.
- Legitimate authenticated users don't see worse rate-limit behaviour.
- Test covers the bypass scenario.

### Test gate
`cd services/gateway && go test ./internal/middleware ./internal/handler`

### Commit message
`fix(gateway): key auth session rate limiter on IP only to prevent bogus-bearer bypass`

---

## Phase 4 — Calls service authorisation

## TASK-13 — Call lifecycle authorisation (H-07)

**Source**: slot-05, H-07, `services/calls/internal/service/call_service.go:311-387` (DeclineCall, EndCall), `services/calls/internal/handler/call_handler.go:236-251` (RemoveParticipant)
**Scope**: `services/calls/internal/service/call_service.go`, `services/calls/internal/handler/call_handler.go`, matching `_test.go` files.

### Why
`DeclineCall`, `EndCall`, and `RemoveParticipant` do not verify the caller's identity against the call's invited set or active participants. Any authenticated user who learns a call UUID can disrupt unrelated calls.

### Change
Mirror the pattern from `AcceptCall` in the same file. For `DeclineCall`: ensure caller is in the call's `invited_users` set, return 403 if not. For `EndCall`: ensure caller is a participant (or the initiator), return 403 if not. For `RemoveParticipant`: ensure caller is a participant with removal rights (initiator or admin role in the chat). Add tests for each: stranger → 403; legit participant → success.

### Acceptance
- Stranger declining someone's incoming call returns 403.
- Stranger ending an active call returns 403.
- Stranger removing a participant returns 403.
- Legitimate actors still succeed.
- Existing tests pass.

### Test gate
`cd services/calls && go test ./internal/handler ./internal/service`

### Commit message
`fix(calls): authorise DeclineCall, EndCall, and RemoveParticipant by caller identity`

## TASK-14 — GetCall roster leak (H-08)

**Source**: slot-05, H-08, `services/calls/internal/handler/call_handler.go:175-186`, `services/calls/internal/service/call_service.go:391-405`
**Scope**: same as TASK-13 plus any new tests.

### Why
`GET /calls/:id` returns the full call roster including display names and avatars of participants, without checking that the caller has membership in the associated chat. Any authenticated user who guesses a call UUID can harvest participant metadata.

### Change
In `GetCall` (handler or service), read the caller's user ID, fetch the call, then check that the caller is a member of `call.ChatID` via the messaging service's `IsMember` check (or the local cache if available). If not a member, return 404 (not 403 — avoid confirming the call exists). Add a test: stranger → 404; member of the chat → full roster.

### Acceptance
- Stranger calling GetCall for a call in a chat they're not in → 404.
- Chat member → full call details as before.

### Test gate
`cd services/calls && go test ./internal/handler ./internal/service`

### Commit message
`fix(calls): hide call roster from non-members of the associated chat`

## TASK-15 — Short-lived HMAC TURN credentials (H-14)

**Source**: slot-05, H-14, `services/calls/internal/handler/call_handler.go:315-317`, `services/calls/internal/service/call_service.go:589-602`
**Scope**: `services/calls/internal/service/call_service.go`, `services/calls/internal/handler/call_handler.go`, `services/calls/cmd/main.go` (env read), `.env.example` (document new var), matching `_test.go`.

### Why
`/calls/:id/ice-servers` returns the global static `TURN_USER:TURN_PASSWORD` pair. Any authenticated caller can harvest these long-lived credentials and reuse the relay outside the original call.

### Change
Implement the standard coturn short-term credential pattern (REST API time-limited credentials, RFC 7635):
1. Add a new env var `TURN_SHARED_SECRET` read in `services/calls/cmd/main.go` (optional — if empty, fall back to static creds with a warning log for backwards compat in local dev).
2. In the service method that builds the ice-servers response, generate a username as `<unix_timestamp_2h_from_now>:<userID>` and a password as base64(HMAC-SHA1(turn_shared_secret, username)). Use Go stdlib `crypto/hmac` and `crypto/sha1`.
3. Return the time-limited creds in place of the static ones when `TURN_SHARED_SECRET` is set.
4. Update `.env.example` to document `TURN_SHARED_SECRET` and mark the fallback behaviour.
5. Update coturn config notes in comments (operator must set `use-auth-secret` and matching `static-auth-secret` in turnserver.conf — add as a comment near the env read in main.go).

Add a test that generates a credential and verifies the HMAC round-trip.

### Acceptance
- When `TURN_SHARED_SECRET` is set, `/calls/:id/ice-servers` returns time-limited username/password pair (username is a unix timestamp followed by userID).
- When unset, existing static behaviour is preserved (but a warning is logged on startup).
- Unit test verifies HMAC correctness.
- `.env.example` documents the new variable.

### Test gate
`cd services/calls && go test ./internal/service ./internal/handler`

### Commit message
`fix(calls): mint short-lived HMAC TURN credentials per RFC 7635`

---

## Phase 5 — Media service

## TASK-16 — Media presigned URL IDOR (H-11)

**Source**: slot-04, H-11, `services/media/internal/service/media_service.go:384-424` (GetPresignedURL, GetThumbnailURL, GetMediumURL)
**Scope**: `services/media/internal/service/media_service.go`, any caller in `services/media/` and `services/gateway/` that invokes these functions, matching `_test.go`.

### Why
`GetPresignedURL`, `GetThumbnailURL`, and `GetMediumURL` accept only a media ID and generate a downloadable URL without an access check. `GetR2Key` in the same file already uses the correct `store.CanAccess(ctx, id, userID)` pattern — these three functions must be brought in line.

### Change
Add a `userID uuid.UUID` parameter to all three functions. Before generating the URL, call `store.CanAccess(ctx, id, userID)` and return `model.ErrMediaNotFound` (the existing sentinel) if it returns false. Update all call sites to pass the caller's user ID — use Grep to find them. If any caller does not have a user ID in scope, log it in PROGRESS.md as `OBSERVED:` and fail that specific call site with an error (do not silently pass `uuid.Nil` — that would preserve the IDOR).

Add tests: owner gets URL; non-owner gets ErrMediaNotFound; nil user ID gets error.

### Acceptance
- All three functions require userID.
- Non-owners cannot generate URLs for other users' media.
- All existing call sites pass a real user ID.
- Tests cover the IDOR scenarios.

### Test gate
`cd services/media && go test ./... && cd ../gateway && go build ./...`

### Commit message
`fix(media): enforce CanAccess on GetPresignedURL/GetThumbnailURL/GetMediumURL`

## TASK-17 — Configurable per-user storage quota skeleton (H-16, corporate-safe default)

**Source**: slot-04, H-16, `services/media/internal/handler/upload_handler.go:57-99`, `services/media/internal/service/media_service.go:44-101`
**Scope**: `services/media/internal/store/media_store.go` (or wherever storage queries live), `services/media/internal/service/media_service.go`, `services/media/internal/handler/upload_handler.go`, `services/media/cmd/main.go` (env read), `.env.example`, matching `_test.go`.

### Why
No per-user quota enforcement exists. For a ~150-employee corporate messenger this is low priority, but a configurable skeleton with a default-off setting gives the operator a safety valve for the day R2 costs spike.

### Change
1. Add a store method `GetUserStorageBytes(ctx, userID) (int64, error)` that sums `size_bytes` from the media table for that user (with `is_deleted=false`).
2. Add env var `MAX_USER_STORAGE_BYTES` read in `services/media/cmd/main.go`. Default: `0` meaning "unlimited, no check". Log at startup `"storage quota disabled"` when 0, or `"storage quota: <N> bytes/user"` when set.
3. In `Upload` and `InitChunkedUpload` service methods, if `maxUserStorage > 0`, call `GetUserStorageBytes` and compare `current + incoming_size > maxUserStorage`. If over, return `apperror.TooManyRequests("Storage quota exceeded")`.
4. `.env.example` documents the variable with a comment: `# Leave empty or 0 to disable (recommended for corporate deployments).`
5. Add a test: with quota set low, second upload fails; with quota unset, unlimited uploads work.

Important: do NOT make the quota mandatory. The corporate messenger use case explicitly accepts unlimited storage as the default. This task is about adding a HOOK, not enforcing a policy.

### Acceptance
- With `MAX_USER_STORAGE_BYTES` unset or 0, upload pipeline behaves exactly as before.
- With `MAX_USER_STORAGE_BYTES=1048576` (1 MB), a 2 MB upload is rejected.
- `.env.example` documents the var and the default-off behaviour.
- Tests cover both paths.

### Test gate
`cd services/media && go test ./...`

### Commit message
`feat(media): add configurable per-user storage quota (default disabled)`

## TASK-18 — Chunked upload MIME bypass (M-12)

**Source**: slot-04, M-12, `services/media/internal/service/media_service.go:692-710`
**Scope**: `services/media/internal/service/media_service.go`, matching `_test.go`.

### Why
After assembling a chunked upload, the MIME re-validation condition is `if detectedMIME != "application/octet-stream" && ...`. Any file whose magic bytes Go's `http.DetectContentType` does not recognise falls through as `octet-stream` and bypasses validation — stored with the client-declared MIME. Additionally, for `MediaTypeFile` the bypass is unconditional.

### Change
1. Remove the `application/octet-stream` short-circuit from the MIME re-validation condition. The fallback for unrecognised binaries should be: strict compare against the declared MIME. If declared MIME is `application/octet-stream` and detected is also `application/octet-stream`, accept. Otherwise reject.
2. For `MediaTypeFile`, still enforce the re-validation but with a relaxed allowlist (accept `application/octet-stream` + any MIME in the existing file allowlist). Do not skip entirely.
3. Add a test: upload a chunked file with magic bytes that don't match any known format, declared as `image/jpeg` → should reject.

### Acceptance
- Unknown-magic files declared as images are rejected after chunked upload.
- Legitimate binary files (MediaTypeFile) still accepted.
- Tests cover the bypass closure.

### Test gate
`cd services/media && go test ./internal/service`

### Commit message
`fix(media): close MIME sniff bypass for unrecognised magic bytes in chunked uploads`

## TASK-19 — EnsureBucket swallowed policy error (M-13)

**Source**: slot-04, M-13, `services/media/internal/storage/r2.go:232-251`
**Scope**: `services/media/internal/storage/r2.go`.

### Why
`EnsureBucket` applies a world-readable bucket policy on every startup and silently discards the `PutBucketPolicy` error. On staging MinIO where permissions may differ, this creates a surprise public bucket.

### Change
1. Do not swallow the `PutBucketPolicy` error. Log it at ERROR level with the bucket name and the error.
2. Make the policy application conditional on an env var `R2_APPLY_PUBLIC_POLICY=true` (default: false). If false, skip policy application entirely — assume the operator pre-configured the bucket.
3. Log at startup which path was taken.

Do NOT change the actual policy (operator may depend on it in production). This is about removing the silent-failure surprise.

### Acceptance
- With default env, EnsureBucket does not touch the bucket policy.
- With `R2_APPLY_PUBLIC_POLICY=true`, it applies the policy and logs success or error (not swallowed).
- `.env.example` documents the new var.

### Test gate
`cd services/media && go build ./...` (no new unit test needed for this io-heavy path).

### Commit message
`fix(media): stop silently swallowing bucket policy errors; gate behind env`

## TASK-20 — Media upload rate limiting (M-14)

**Source**: slot-04, M-14, `services/media/internal/handler/upload_handler.go:47-54`
**Scope**: `services/media/internal/handler/upload_handler.go`, `services/media/cmd/main.go`.

### Why
No rate limiting in the media service. A single authenticated user can initiate unlimited concurrent chunked upload sessions. Corporate context is trusted, but a sensible ceiling prevents accidental runaway clients (e.g., buggy desktop app in a retry loop).

### Change
Use the same rate-limit middleware pattern from the gateway (check `services/gateway/internal/middleware/ratelimit.go`). Add a per-user rate limit on `Upload` and `InitChunkedUpload` routes: `60 req/min/user` (generous). Read `REDIS_URL` in media `cmd/main.go` if not already; register the middleware. Key on `user:<userID>`.

If the media service does not already depend on a shared rate-limit package, copy the minimum implementation from gateway into `services/media/internal/middleware/ratelimit.go` — do not add a new external package. Keep the copy minimal.

### Acceptance
- 61st request in one minute from the same user returns 429.
- 60 req/min from different users all succeed.
- Logged rate-limit hits do not include sensitive data.

### Test gate
`cd services/media && go test ./internal/handler`

### Commit message
`fix(media): add per-user rate limit on upload endpoints`

---

## Phase 6 — Search / performance

## TASK-21 — Search ACL N+10 query amplification (H-17)

**Source**: slot-18, H-17, `services/messaging/internal/service/search_service.go:61-70`, `services/messaging/internal/store/chat_store.go:70-104`
**Scope**: `services/messaging/internal/store/chat_store.go`, `services/messaging/internal/service/search_service.go`, matching `_test.go`.

### Why
The search flow calls `ListByUser` (a heavyweight query with correlated COUNT subqueries + LATERAL joins) in a loop up to 10 times per search request to collect chat IDs for the Meilisearch ACL filter. A user in many chats triggers a DB amplification path.

### Change
1. Add a new store method `GetUserChatIDs(ctx, userID uuid.UUID) ([]uuid.UUID, error)` that runs a single lightweight query: `SELECT chat_id FROM chat_members WHERE user_id = $1 AND role != 'banned'`. No JOINs, no COUNT, no LATERAL.
2. In `search_service.go` around line 61, replace the `ListByUser` loop with a single `GetUserChatIDs` call.
3. Add a test: mock returns N chat IDs, verify the service calls `GetUserChatIDs` exactly once and passes the IDs to Meilisearch.
4. Do NOT delete the `ListByUser` loop usage elsewhere — it's still the right call for chat list rendering.

### Acceptance
- Search with a user in 50 chats triggers 1 chat_members query, not 10 ListByUser calls.
- Banned-status filter is respected (banned users don't appear in results for chats they were banned from).
- Existing search tests still pass.

### Test gate
`cd services/messaging && go test ./internal/service`

### Commit message
`fix(messaging): use lightweight GetUserChatIDs for search ACL filter`

---

## Phase 7 — WebSocket session lifecycle

## TASK-22 — WS periodic token re-validation (H-01)

**Source**: slot-01, H-01, `services/gateway/internal/ws/handler.go:68-158`, `services/gateway/internal/handler/sfu_proxy.go:73-168`
**Scope**: `services/gateway/internal/ws/handler.go`, `services/gateway/internal/ws/hub.go`, `services/gateway/internal/handler/sfu_proxy.go`, matching `_test.go`.

### Why
After the initial auth frame, WS and SFU connections stay open indefinitely. A stolen token can keep the session alive via ping/pong with zero re-validation.

### Change
For each authenticated WS connection:
1. Store the token's expiry and a pointer to the token hash on the connection struct.
2. Start a per-connection goroutine that ticks every 30 seconds. On each tick: (a) if `time.Now() > token_exp` → close with code 1008, message `"token expired"`; (b) call `authService.ValidateAccessToken` (or the equivalent blacklist check) — if it returns an error, close with code 1008, message `"token revoked"`.
3. Stop the ticker on connection close (defer). Make sure the goroutine exits cleanly.
4. Mirror for SFU proxy.
5. Add a test that sets up a WS connection, ages out its token (mock the auth service), and asserts the connection closes within 35 seconds (or use a test-injectable ticker).

Pick 30 seconds as the interval — matches the existing heartbeat cadence in the ТЗ (CLAUDE.md: ping/pong every 30 seconds). Do not make it shorter — too much auth-service load.

### Acceptance
- Expired token closes WS within ~30s.
- Revoked token closes WS within ~30s.
- Live valid token keeps the connection open.
- No goroutine leaks (ticker stops on close).
- Test covers at least the expiry scenario.

### Test gate
`cd services/gateway && go test ./internal/ws ./internal/handler`

### Commit message
`fix(gateway): periodically re-validate JWT on WS and SFU connections`

## TASK-23 — User deactivated event NATS subscription (H-03)

**Source**: slot-08, H-03, `services/messaging/internal/service/admin_service.go:101-107` (publishes), `services/gateway/internal/ws/nats_subscriber.go:55-73` (no subscription)
**Scope**: `services/gateway/internal/ws/nats_subscriber.go`, `services/gateway/internal/ws/hub.go`, matching `_test.go`.

### Why
When an admin deactivates a user account, the messaging service publishes a NATS event on `orbit.user.*.deactivated`, but the gateway never subscribes to this subject. Open WS sessions continue receiving events until the TCP connection drops naturally.

### Change
1. In `nats_subscriber.go`, add a subscription to `orbit.user.*.deactivated`. Extract the user ID from the subject or event payload.
2. On message receipt, call `hub.CloseUserConnections(userID)` (add this method to Hub if it doesn't exist — it should iterate connections for the user and send a close frame with code 1008, message `"account deactivated"`).
3. The Hub method should be safe to call concurrently with ongoing sends — use the same locking pattern as existing hub methods.
4. Add a test: publish a fake deactivation event, verify the matching connection closes.

### Acceptance
- Deactivating a user immediately closes all their open WS connections.
- Other users' connections are unaffected.
- Test verifies the close.

### Test gate
`cd services/gateway && go test ./internal/ws`

### Commit message
`fix(gateway): close WS connections on user deactivation NATS event`

## TASK-24 — WS fanout: disconnect slow subscribers instead of blocking (H-15)

**Source**: slot-08, H-15, `services/gateway/internal/ws/hub.go:91-114`, `services/gateway/internal/ws/nats_subscriber.go:492-503`
**Scope**: `services/gateway/internal/ws/hub.go`, `services/gateway/internal/ws/nats_subscriber.go`, matching `_test.go`.

### Why
The fanout path writes synchronously to each WebSocket connection with a 10-second blocking write. One slow subscriber stalls NATS delivery for the whole chat. Per the corporate context, dropping events is unacceptable (audit trail). The correct fix: disconnect the slow client, let it reconnect and replay via JetStream (TASK-46).

### Change
1. Give each connection a per-connection send queue — a buffered channel of, say, 256 messages.
2. Replace the synchronous write in the fanout path with a non-blocking `select { case conn.send <- msg: default: go conn.Close(1008, "slow consumer") }`. The default branch disconnects the slow client.
3. Add a per-connection goroutine that reads from the send queue and performs the actual write with a 5-second deadline. On write timeout → close with code 1008, message `"write timeout"`.
4. Ensure goroutine lifecycle: start on accept, stop on close, drain the queue cleanly.
5. Add a test: mock a slow client that blocks on read, verify it gets disconnected within the queue capacity instead of stalling the hub.

Important: do NOT silently drop events. Disconnect means the client will reconnect and replay from JetStream once TASK-46 lands. Without TASK-46, there is a temporary gap — acknowledge this in a code comment.

### Acceptance
- Slow subscriber gets disconnected, not silently dropped events.
- Hub fanout latency does not grow linearly with slow-client count.
- Other subscribers unaffected.
- Test demonstrates disconnect-on-slow.

### Test gate
`cd services/gateway && go test ./internal/ws`

### Commit message
`fix(gateway): disconnect slow WS subscribers instead of blocking fanout`

## TASK-25 — JWT session binding via jti claim (H-02, M-01)

**Source**: slot-02, H-02, `services/auth/internal/service/auth_service.go:305-315`, `services/auth/internal/service/auth_service.go:442-485`, `services/auth/internal/service/auth_service.go:490-538`
**Scope**: `services/auth/internal/service/auth_service.go`, `services/auth/internal/model/models.go` (if session model needs a version field), `services/auth/internal/store/session_store.go`, migration file (new, `migrations/NNN_session_jti.sql` — pick next number), matching `_test.go`.

### Why
Revoking a session does not invalidate that session's already-issued access JWT. Access tokens carry no `jti` and no session reference. After a revoke, the attacker retains a valid access token for up to 15 minutes.

### Change
1. Add a `jti` claim to access tokens containing the session ID (UUID) that issued them. Modify the token creation path in `auth_service.go` around line 490.
2. In `ValidateAccessToken` around line 442, extract the `jti`, look up the session by ID via `sessionStore.GetByID`, and return `apperror.Unauthorized("Session revoked")` if not found.
3. `RevokeSession` (around line 305) already deletes the session row. After TASK-25, that delete IS the revocation — no separate blacklist needed for this path. But keep the blacklist logic for the Logout flow (blacklisting the specific access token presented).
4. Create a new migration file if any schema change is needed. (Likely not — `sessions` table should already have an `id` column.) Verify by reading the latest migration number.
5. Add tests:
   - Login → get access token → call GetMe → 200.
   - Login → revoke session → call GetMe with old access token → 401 "Session revoked".
   - Old tests still pass.

Corporate context: losing all existing sessions on deploy is explicitly acceptable (user confirmed). No dual-validation layer needed.

### Acceptance
- Access tokens contain `jti` (session ID).
- Revoking a session invalidates its access token immediately (next API call).
- Logout still works via existing blacklist path.
- New tests pass.

### Test gate
`cd services/auth && go test ./...`

### Commit message
`fix(auth): bind access tokens to session via jti claim for immediate revocation`

---

## Phase 8 — Infrastructure / deploy

## TASK-26 — Coturn default credentials hardening (H-12)

**Source**: slot-17, H-12, `docker-compose.yml:83-96`, `docker-compose.yml:197-212`
**Scope**: `docker-compose.yml`, `.env.example`.

### Why
If `TURN_PASSWORD` is omitted from the env file, coturn starts with `orbit:orbit` credentials publicly reachable. In corporate deployment this is a relay open to the world.

### Change
1. In `docker-compose.yml`, replace `${TURN_USER:-orbit}` and `${TURN_PASSWORD:-orbit}` with `${TURN_USER:?TURN_USER is required}` and `${TURN_PASSWORD:?TURN_PASSWORD is required}`. The `:?` syntax makes docker-compose fail fast if the var is unset.
2. Update `.env.example` with a prominent comment warning: `# REQUIRED: compose will fail without these values. Never use 'orbit:orbit'.`
3. Verify docker-compose still parses (`docker compose config` would be ideal but may not be available in the agent environment — if not, just visually verify the file still looks valid YAML after edit).

### Acceptance
- `docker-compose.yml` uses `:?` for TURN_USER and TURN_PASSWORD.
- `.env.example` has a warning comment.
- File is still valid YAML.

### Test gate
`python -c "import yaml; yaml.safe_load(open('docker-compose.yml'))" || echo "yaml parse failed"` (if python not available, skip test gate and log verified-by-inspection)

### Commit message
`fix(infra): require TURN credentials in compose, no silent default fallback`

---

## Phase 9 — Race conditions and atomicity

## TASK-27 — Scheduled SendNow double-delivery race (M-06)

**Source**: slot-03, M-06, `services/messaging/internal/service/scheduled_service.go:265-277`
**Scope**: `services/messaging/internal/service/scheduled_service.go`, `services/messaging/internal/store/scheduled_store.go` (or equivalent), matching `_test.go`.

### Why
`SendNow` reads `IsSent`, then calls `deliver()`, then `MarkSent()`. The cron job `DeliverPending` can claim the same message in the window, causing double delivery.

### Change
1. Add an atomic store method `ClaimScheduled(ctx, id) (bool, error)` that runs `UPDATE scheduled_messages SET is_sent = true WHERE id = $1 AND is_sent = false RETURNING id`. Returns `true` if a row was affected (claim successful), `false` otherwise (already sent).
2. In `SendNow`, replace the read-then-mark pattern with: call `ClaimScheduled` first; if false, return `nil` silently (already delivered — idempotent); if true, proceed with delivery.
3. Update the cron `DeliverPending` path similarly — use `ClaimScheduled` per message before delivering.
4. Add a test simulating concurrent `SendNow` and `DeliverPending`: only one should deliver.

### Acceptance
- Double `SendNow` delivers exactly once.
- Concurrent `SendNow` + cron delivers exactly once.
- Tests cover both.

### Test gate
`cd services/messaging && go test ./internal/service`

### Commit message
`fix(messaging): atomic claim of scheduled messages to prevent double delivery`

## TASK-28 — Call state TOCTOU + unique constraint (M-07)

**Source**: slot-05, M-07, `services/calls/internal/service/call_service.go:45-76`, `migrations/034_phase6_calls.sql:28-29`
**Scope**: `services/calls/internal/service/call_service.go`, migration file (new or extend 034 — prefer a new one like `migrations/NNN_calls_unique_active.sql`), matching `_test.go`.

### Why
All call state transitions are TOCTOU: read state, compare, update. The DB has no unique constraint enforcing a single active call per chat — two clients can race and create two calls for the same chat.

### Change
1. Add a new migration file `migrations/NNN_calls_unique_active.sql` (pick the next available number — check current max). Content: `CREATE UNIQUE INDEX IF NOT EXISTS idx_calls_chat_active ON calls(chat_id) WHERE status IN ('ringing', 'active');`. This is a partial unique index that allows multiple historical calls but only one active.
2. In `requestCall` service method, handle the unique violation error from the store: if the insert fails with a unique constraint error (pgx error code `23505`), fetch the existing active call and return it instead of creating a new one. This makes the operation idempotent per chat.
3. Add a test: two goroutines race `requestCall` → both return the same call object, one via insert, one via conflict fallback.

Note: the fix in cb0646c ("dedupe concurrent requestCall to prevent self-discard") added app-level dedupe. The partial unique index is defence-in-depth at the DB layer.

### Acceptance
- New migration file exists and is idempotent (`IF NOT EXISTS`).
- Concurrent requestCall returns the same call, no duplicate rows.
- Test covers the race.

### Test gate
`cd services/calls && go test ./internal/service`

### Commit message
`fix(calls): add partial unique index on active calls per chat + handle conflict in requestCall`

## TASK-29 — link_preview_service non-atomic INCR+EXPIRE (M-08)

**Source**: slot-03, M-08, `services/messaging/internal/service/link_preview_service.go:88-97`
**Scope**: `services/messaging/internal/service/link_preview_service.go`, matching `_test.go`.

### Why
Rate limiter does `INCR` then `EXPIRE`. If `EXPIRE` fails (Redis hiccup), the key has no TTL and permanently blocks that user's link previews.

### Change
Replace the two-step pattern with an atomic Redis operation. Two acceptable approaches:
1. **SET NX EX then INCR**: `SET key 0 NX EX <ttl>` (only sets if missing, with TTL), then `INCR key`. TTL is established on first hit.
2. **Lua script**: a single EVAL that does `local v = redis.call('INCR', KEYS[1]); if v == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]); end; return v`.

Prefer option 1 — simpler, no Lua. Add a test using miniredis: verify TTL is set on the first hit.

### Acceptance
- First hit sets TTL atomically.
- TTL does not reset on subsequent hits (we want it to decay).
- If Redis fails mid-operation, the key is either absent or has a TTL (no forever-stuck state).

### Test gate
`cd services/messaging && go test ./internal/service`

### Commit message
`fix(messaging): atomic rate limit for link preview (SET NX EX + INCR)`

## TASK-30 — Tenor client non-atomic INCR+EXPIRE (M-09)

**Source**: slot-03, M-09, `services/messaging/internal/tenor/client.go:195-203`
**Scope**: `services/messaging/internal/tenor/client.go`, `services/messaging/internal/tenor/client_test.go` if exists.

### Why
Same anti-pattern as TASK-29, but at the global Tenor rate-limit key. If EXPIRE fails, GIF search breaks forever for everyone.

### Change
Apply the same fix as TASK-29: `SET NX EX` + `INCR`. Extract a helper in the tenor package if the pattern is duplicated anywhere else, or keep it inline — the important thing is atomicity.

### Acceptance
- First hit sets TTL atomically.
- No forever-stuck state on Redis flake.

### Test gate
`cd services/messaging && go test ./internal/tenor` (if package has tests) or `go build ./...` otherwise.

### Commit message
`fix(messaging): atomic rate limit for tenor GIF client`

---

## Phase 10 — Resource bounds

## TASK-31 — Sticker import unbounded ReadAll (M-15)

**Source**: slot-03, M-15, `services/messaging/internal/service/sticker_import.go:479`
**Scope**: `services/messaging/internal/service/sticker_import.go`.

### Why
`io.ReadAll(resp.Body)` with no limit. A large CDN response during sticker import can exhaust heap.

### Change
Replace `io.ReadAll(resp.Body)` with `io.ReadAll(io.LimitReader(resp.Body, maxStickerBytes))` where `maxStickerBytes = 10 * 1024 * 1024` (10 MB — generous for a sticker file, Telegram's own limit is 512KB for static and 256KB for animated). Check that the read result is smaller than the limit; if equal, log and return an error (likely truncated).

### Acceptance
- Reading a response larger than 10 MB returns an error, not a half-loaded sticker.
- Normal-sized stickers still import.

### Test gate
`cd services/messaging && go build ./...` (behavioural; no easy unit test without mocking HTTP body)

### Commit message
`fix(messaging): bound sticker import reads with io.LimitReader`

---

## Phase 11 — Missing NATS fanout and self-leave event

## TASK-32 — Self-leave member_removed fanout missing (M-17)

**Source**: slot-08, M-17, `services/messaging/internal/service/chat_service.go:415-437`
**Scope**: `services/messaging/internal/service/chat_service.go`, matching `_test.go`.

### Why
In the self-leave path, the departing user is removed BEFORE `GetMemberIDs` is called. The `chat_member_removed` NATS event is not fanned out to the leaving user's other tabs/devices, so their other clients still show the chat.

### Change
Reorder the operations: call `GetMemberIDs` BEFORE the remove. Include the leaving user's ID in the `MemberIDs` field of the NATS event payload (or explicitly always append `userID` to the recipient list when the removed user is the caller). Verify the event actually reaches the leaver's other connections.

Add a test: self-leave publishes an event whose `MemberIDs` includes the leaver.

### Acceptance
- Leaver's other tabs/devices receive `chat_member_removed` and refresh UI.
- Existing tests still pass.

### Test gate
`cd services/messaging && go test ./internal/service`

### Commit message
`fix(messaging): include self in member_removed fanout on self-leave`

---

## Phase 12 — Schema integrity migrations

## TASK-33 — poll_votes composite FK (M-18)

**Source**: slot-07, M-18, `migrations/022_phase5_rich_messaging.sql:119-136`
**Scope**: new migration file `migrations/NNN_poll_votes_composite_fk.sql` (pick next number).

### Why
`poll_votes` has separate FKs on `polls(id)` and `poll_options(id)` but no composite FK. A vote can reference an option from a different poll, corrupting poll counts.

### Change
Create a new migration file that:
1. Adds a composite FK via `ALTER TABLE poll_votes ADD CONSTRAINT poll_votes_poll_option_fk FOREIGN KEY (poll_id, option_id) REFERENCES poll_options(poll_id, id) ON DELETE CASCADE;` (assuming `poll_options` has `poll_id` column — verify by reading migration 022; if not, the constraint should be built differently).
2. If `poll_options` does not already have a composite `UNIQUE(poll_id, id)`, the FK cannot exist — the migration must add it first: `ALTER TABLE poll_options ADD CONSTRAINT poll_options_poll_id_id_key UNIQUE (poll_id, id);`.
3. Wrap in `DO $$ BEGIN ... EXCEPTION WHEN duplicate_object THEN NULL; END $$;` blocks for idempotency.
4. Include a `DELETE FROM poll_votes WHERE NOT EXISTS (SELECT 1 FROM poll_options WHERE poll_options.id = poll_votes.option_id AND poll_options.poll_id = poll_votes.poll_id);` cleanup step before adding the FK, to remove any existing orphan votes.

Do NOT run the migration. Edit the file only.

### Acceptance
- Migration file exists, has the next sequential number.
- Content is idempotent (can be re-run safely).
- Includes cleanup of orphans before FK creation.

### Test gate
`ls migrations/ | tail -5` (visual check that the file was created in the right place). No runtime test.

### Commit message
`fix(db): add composite FK on poll_votes to prevent cross-poll option references`

## TASK-34 — saved_messages_lookup UNIQUE (chat_id) (M-19)

**Source**: slot-07, M-19, `migrations/033_saved_messages.sql:2-4`
**Scope**: new migration file.

### Why
`saved_messages_lookup` primary key is `(user_id)` only. No unique constraint on `chat_id` — a race can link the same saved-messages chat to multiple users.

### Change
New migration file: `ALTER TABLE saved_messages_lookup ADD CONSTRAINT saved_messages_lookup_chat_id_key UNIQUE (chat_id);` wrapped in DO/EXCEPTION block for idempotency.

Note: if the table already has duplicate chat_ids in production, this migration will fail. Add a DELETE-oldest cleanup step that keeps the row with the earliest `created_at` per chat_id (if such column exists). Verify by reading 033 first.

### Acceptance
- Migration file exists, is idempotent.
- Includes any necessary cleanup.

### Test gate
File-level only.

### Commit message
`fix(db): add UNIQUE constraint on saved_messages_lookup.chat_id`

## TASK-35 — Idempotent DDL in migrations 022/034/036/037 (M-20)

**Source**: slot-07, M-20
**Scope**: `migrations/022_phase5_rich_messaging.sql`, `migrations/034_phase6_calls.sql`, `migrations/036_*.sql`, `migrations/037_*.sql`.

### Why
Latest migrations contain bare `CREATE TABLE/INDEX/TRIGGER` without `IF NOT EXISTS`. Concurrent multi-service startup on a fresh DB can race and fail with duplicate-object errors. Migration 037 already has this fix (d6b25e3 commit). Others need the same treatment.

### Change
For each of 022, 034, 036 (and 037 if not already done):
1. Read the file fully.
2. Replace `CREATE TABLE` → `CREATE TABLE IF NOT EXISTS`.
3. Replace `CREATE INDEX` → `CREATE INDEX IF NOT EXISTS` (PostgreSQL supports this since 9.5).
4. Wrap `CREATE TRIGGER` in `DO $$ BEGIN ... EXCEPTION WHEN duplicate_object THEN NULL; END $$;` (PostgreSQL has no `CREATE TRIGGER IF NOT EXISTS`).
5. Wrap `ALTER TABLE ... ADD CONSTRAINT` similarly (no IF NOT EXISTS for constraints).

Do NOT change any semantics — only make each statement idempotent.

### Acceptance
- All four files are idempotent (re-runnable).
- No semantic drift.

### Test gate
Visual verification only. Log in PROGRESS.md which statements you wrapped in each file.

### Commit message
`fix(db): make migrations 022, 034, 036, 037 idempotent`

---

## Phase 13 — Migrator hardening

## TASK-36 — migrator checksum verification (M-21)

**Source**: slot-06, M-21, `pkg/migrator/migrator.go:83-86`
**Scope**: `pkg/migrator/migrator.go`.

### Why
Stored migration checksums are loaded from the DB but never compared against current file content. A migration edited after initial application is silently skipped — leaving the DB in an inconsistent state.

### Change
In the migrator's main loop, after loading `applied[filename] = checksum` from the DB, for each on-disk migration file, compute its current checksum (SHA-256 of file bytes) and compare against the stored one. If they differ: log ERROR with filename, current checksum, stored checksum, and **return an error** — halt migration. Include the filename in the error message so the operator knows which file to investigate.

Do NOT silently proceed. Migrations are not safe to auto-reconcile. Operator must manually resolve (either revert the file or create a new migration).

### Acceptance
- If a migration file is edited after application, migrator halts with a clear error.
- If checksums match, migrator proceeds normally.
- Fresh DBs (no applied records) proceed normally.

### Test gate
`cd pkg && go test ./migrator/... 2>&1 || echo "no test (migrator requires DB)"` — if no tests exist, just build: `cd pkg && go build ./migrator/...`.

### Commit message
`fix(migrator): halt on checksum mismatch to prevent silent schema drift`

## TASK-37 — migrator legacy DB heuristic replacement (M-22)

**Source**: slot-06, M-22, `pkg/migrator/migrator.go:61-75`
**Scope**: `pkg/migrator/migrator.go`.

### Why
The legacy DB bootstrap heuristic checks if a `users` table exists and skips ALL migrations if yes. A shared or misconfigured DB with any `users` table silently has zero migrations applied.

### Change
Replace the heuristic with: check if `schema_migrations` table exists. If yes → standard flow. If no → it's truly fresh, create `schema_migrations` and apply all files. Do NOT skip migrations based on the presence of unrelated tables.

If the codebase has some historical deployment where `users` existed before `schema_migrations` (the reason the heuristic was added), add a one-time bootstrap: if `users` exists but `schema_migrations` does not, create `schema_migrations` and seed it with ALL migration files marked as applied (trusting the operator). Log loudly: `"legacy bootstrap: seeding schema_migrations from existing users table"`. This preserves compatibility but makes the behaviour explicit.

### Acceptance
- Truly fresh DB → all migrations apply.
- Legacy DB with `users` but no `schema_migrations` → seeds and logs loudly.
- DB with `schema_migrations` → standard flow.

### Test gate
`cd pkg && go build ./migrator/...`

### Commit message
`fix(migrator): replace legacy users-table heuristic with explicit schema_migrations check`

---

## Phase 14 — Frontend

## TASK-38 — Invite hash URL path traversal (M-24)

**Source**: slot-11, M-24, `web/src/api/saturn/methods/chats.ts:442-448`
**Scope**: `web/src/api/saturn/methods/chats.ts`, nearby callers if the raw hash is passed through.

### Why
The raw invite hash is spliced into a URL path without encoding. A value like `../../auth/logout` rewrites the target URL, turning the invite flow into an authenticated same-origin request gadget.

### Change
Wrap the invite hash in `encodeURIComponent()` before path interpolation. Verify no other `/methods/*.ts` file has the same anti-pattern — Grep for path interpolation of user-controlled strings and fix any other occurrences (log each in PROGRESS.md as `OBSERVED:` if found, but only fix them if the task block allows scope expansion — it does not, so log and move on).

### Acceptance
- Invite hash is URL-encoded in the outgoing request path.
- Malicious hash values do not rewrite the target URL.

### Test gate
`cd web && npx tsc --noEmit --project tsconfig.json 2>&1 | head -50` (type-check only; full build is slow)

### Commit message
`fix(web): encode invite hash before path interpolation to prevent URL rewrite`

## TASK-39 — Auth registration form deadlock (M-25)

**Source**: slot-16, M-25, `web/src/components/auth/AuthSaturnRegister.tsx:27,29,61`
**Scope**: `web/src/components/auth/AuthSaturnRegister.tsx`.

### Why
The form sets `isLoading = true` on submit but never clears it on any failure path. Form is permanently disabled until page reload.

### Change
Wrap the submit handler in `try { ... } finally { setIsLoading(false); }`. Ensure the finally runs after both the success redirect and any error display.

### Acceptance
- Failed registration attempt re-enables the submit button.
- Successful registration still redirects correctly.

### Test gate
`cd web && npx tsc --noEmit --project tsconfig.json 2>&1 | head -20`

### Commit message
`fix(web): clear isLoading on registration failure to unblock the form`

## TASK-40 — QR auth screen wiring (M-26)

**Source**: slot-16, M-26, `web/src/components/auth/Auth.tsx:55`
**Scope**: `web/src/components/auth/Auth.tsx`.

### Why
`authorizationStateWaitQrCode` is mapped to `AuthEmailLogin` instead of the QR/passkey login component. The QR screen is never rendered.

### Change
Check whether a `AuthQrCode.tsx` or similar component exists (Grep for `QrCode` in `web/src/components/auth/`). If yes, wire it up in the switch. If no, this is an incomplete feature — log as `SKIPPED: QR login component not implemented in the codebase; wiring would show a blank screen` and continue. Do NOT invent a new component in scope.

### Acceptance
- Either: QR component wired, or: task skipped with logged rationale.

### Test gate
`cd web && npx tsc --noEmit --project tsconfig.json 2>&1 | head -20`

### Commit message (only if wired)
`fix(web): wire AuthQrCode screen to authorizationStateWaitQrCode`

### On failure
rollback and skip (may be genuinely incomplete)

## TASK-41 — toggleReaction stale rollback (M-27)

**Source**: slot-13, M-27, `web/src/global/actions/api/reactions.ts:181-245`
**Scope**: `web/src/global/actions/api/reactions.ts`.

### Why
Concurrent `toggleReaction` requests can overwrite a newer successful state with an older failed snapshot when the older request's rollback path executes after the newer state has been applied.

### Change
Before applying the rollback snapshot, compare against the current global state. If the current reaction state for the message has moved past the snapshot (e.g., different version counter or different reactions set), skip the rollback — the newer state is authoritative. Use a per-message version number or compare the reactions array by value.

If the current code uses optimistic updates via a dedicated slice, consult the existing pattern in other action files (e.g., messages.ts) and match it.

### Acceptance
- Stale rollback is skipped when a newer successful state exists.
- Fresh rollback (no newer state) still applies.

### Test gate
`cd web && npx tsc --noEmit --project tsconfig.json 2>&1 | head -20`

### Commit message
`fix(web): skip stale rollback in toggleReaction if newer state wins`

## TASK-42 — Message history stale-overwrite (M-28)

**Source**: slot-14, M-28, `web/src/global/actions/api/messages.ts:1807`, `web/src/global/reducers/messages.ts:182`
**Scope**: `web/src/global/reducers/messages.ts`, `web/src/global/actions/api/messages.ts`.

### Why
`mergeApiMessages` has no freshness guard. An older in-flight history fetch can overwrite newer message state (edits, deletes) when it resolves after a newer fetch.

### Change
In `mergeApiMessages`, for each incoming message, compare against the current state: if `current.editTimestamp > incoming.editTimestamp` or `current.isDeleted && !incoming.isDeleted`, keep current. Only merge fields when incoming is demonstrably newer.

Verify the existing reducer does not already handle this — Grep for `editTimestamp` usage in the reducer.

### Acceptance
- Older history fetch does not resurrect deleted messages.
- Older history fetch does not revert message edits.
- Newer fetches still merge as expected.

### Test gate
`cd web && npx tsc --noEmit --project tsconfig.json 2>&1 | head -20`

### Commit message
`fix(web): add freshness guard to mergeApiMessages`

## TASK-43 — Directory drag-drop readEntries loop (M-29)

**Source**: slot-15, M-29, `web/src/components/middle/composer/helpers/getFilesFromDataTransferItems.ts:24-32`
**Scope**: `web/src/components/middle/composer/helpers/getFilesFromDataTransferItems.ts`.

### Why
`readEntries()` is called exactly once. Browsers return file lists in OS-chunked batches; calling once drops everything beyond the first batch on directory drops.

### Change
Wrap `readEntries` in a loop: call it repeatedly until it returns an empty array. This is the standard `FileSystemDirectoryReader` pattern (documented on MDN). Preserve the existing async handling.

### Acceptance
- Dropping a directory with 50 files yields all 50, not just the first batch.
- Single-file drops still work.

### Test gate
`cd web && npx tsc --noEmit --project tsconfig.json 2>&1 | head -20`

### Commit message
`fix(web): loop readEntries until exhausted to capture all dropped files`

## TASK-44 — Blob URL revocation in mediaLoader (M-30)

**Source**: slot-15, M-30, `web/src/util/mediaLoader.ts:145-147,180,207-210`
**Scope**: `web/src/util/mediaLoader.ts`.

### Why
Blob URLs for photos and videos are never revoked via `URL.revokeObjectURL()`. Repeated media browsing steadily grows renderer memory (one URL per image/video viewed).

### Change
Maintain a small LRU map of blob URLs (keyed by media ID) with a cap of, say, 128 entries. On insertion past the cap, `URL.revokeObjectURL()` the evicted entry. Also revoke on explicit unload / route change if a hook is available.

If the file already has a caching structure, extend it with the revocation logic. If not, add a minimal LRU (Map + size check, evict oldest on overflow).

### Acceptance
- After browsing 200 images in a chat, fewer than ~130 blob URLs exist in the map.
- Revoked blob URLs are not loaded again (cache miss re-creates them).

### Test gate
`cd web && npx tsc --noEmit --project tsconfig.json 2>&1 | head -20`

### Commit message
`fix(web): revoke blob URLs on LRU eviction in mediaLoader`

---

## Phase 15 — Permissions test noise

## TASK-45 — Permissions test mismatch (M-02)

**Source**: slot-06, M-02, `pkg/permissions/permissions_test.go:16-21`
**Scope**: read-only on `pkg/permissions/permissions_test.go`.

### Why
Already resolved in commit 73741d1 (my work). The original M-02 finding about the test passing `memberPerms=0` expecting defaults is fixed — tests now use `PermissionsUnset`. This task is a VERIFICATION pass to confirm the test file is in the expected state and log DONE.

### Change
Read `pkg/permissions/permissions_test.go` lines 16-50. Verify:
1. `TestEffectivePermissions_Admin_DefaultPerms` uses `PermissionsUnset` for memberPerms.
2. A `TestEffectivePermissions_Admin_ExplicitlyZero` test exists.
3. `TestEffectivePermissions_MemberGroup_DefaultPerms` uses `PermissionsUnset`.

If all three hold, log `TASK-45 VERIFIED — M-02 resolved in 73741d1` and commit nothing. If any is missing, this would indicate rollback or drift — log as FAILED and halt (stop condition).

### Acceptance
- Verification log in PROGRESS.md.
- No code change, no commit.

### Test gate
`cd pkg && go test ./permissions/...`

### Commit message
(no commit)

---

## Phase 16 — NATS durability (largest refactor, intentionally last)

## TASK-46 — JetStream durable subscribers with message dedup (M-16)

**Source**: slot-08, M-16, `services/gateway/internal/ws/nats_subscriber.go:75-76`
**Scope**: `services/gateway/internal/ws/nats_subscriber.go`, all publisher sites that publish on `orbit.*` subjects (check `services/messaging/`, `services/calls/`, `services/media/` for `publisher.Publish` calls), possibly `services/gateway/cmd/main.go` for stream setup.

### Why
Core NATS `nc.Subscribe` is at-most-once. Gateway restart silently drops all in-flight events. For corporate audit compliance, switch to JetStream durable consumers with idempotency (dedup by message ID on the WS hub to prevent dupes).

### Change
This is the most complex task. Do it in THREE sub-commits within this task:

**Sub-commit 1 — stream setup**:
1. On gateway startup, ensure a JetStream stream exists covering the `orbit.>` subject hierarchy: `js.AddStream(&nats.StreamConfig{Name: "ORBIT", Subjects: []string{"orbit.>"}, Retention: nats.LimitsPolicy, MaxAge: 24*time.Hour})`. Idempotent — call `AddStream` and handle "already exists" error gracefully.
2. Commit: `feat(gateway): create JetStream ORBIT stream on startup`.

**Sub-commit 2 — durable consumer**:
1. Replace `nc.Subscribe` calls in `nats_subscriber.go` with `js.PullSubscribe` or `js.Subscribe` with a durable name like `gateway-ws`. Each subject gets its own durable.
2. On receive, call `msg.Ack()` after successful fanout to the WS hub. On fanout error, `msg.Nak()` to retry.
3. Add a dedup cache on the hub: a small LRU (say 1024 entries) keyed by `eventID` (which the publisher should already include in the event envelope). On duplicate delivery (which JetStream may do on retry), skip the fanout silently.
4. Add a test covering dedup: same event ID delivered twice → fanned out once.
5. Commit: `feat(gateway): durable JetStream consumer with message dedup`.

**Sub-commit 3 — publisher side idempotency marker**:
1. Verify all publish sites set a unique `Nats.MsgId` header in the publish options. The JetStream server will also dedup on this header if within the dedup window (default 2 minutes). Add the header wherever missing.
2. Commit: `feat(gateway): set NATS MsgId header on publish for JetStream dedup`.

**Important**: if any of the three sub-commits fails the test gate, rollback that sub-commit and halt the entire task (do not leave the system in a half-migrated state). Log `TASK-46 PARTIAL: stopped after sub-commit N` and move to the next task.

### Acceptance
- ORBIT stream exists on gateway startup.
- Durable consumer receives events, acks on success.
- Dedup prevents duplicate fanout.
- Existing flows (new_message, reaction_added, etc.) still work end-to-end.
- All services still build.

### Test gate
`cd services/gateway && go test ./internal/ws ./internal/handler` and `cd services/messaging && go test ./...` and `cd services/calls && go build ./...` and `cd services/media && go build ./...`

### Commit messages
(three commits as described above)

### On failure
keep partial and log (this task is allowed partial completion per the sub-commit structure)

---

## TASK-47 — (reserved) Final smoke test

**Source**: housekeeping
**Scope**: all five services.

### Why
After 46 task attempts, run a final cross-service test pass and log the result. This is the last step before the session can end.

### Change
1. Run `go test ./...` in each of `services/{gateway, auth, messaging, media, calls}` and log the result per service.
2. Run `cd pkg && go test ./...`.
3. Summarise in PROGRESS.md: how many tasks DONE, SKIPPED, FAILED, PARTIAL. How many services currently green.
4. No code changes. No commits.

### Acceptance
- PROGRESS.md has a final summary block `## FINAL SUMMARY` with the statistics.

### Test gate
(the task IS the test gate)

### Commit message
(no commit)

---

# APPENDIX — What to do when the plan runs out

After TASK-47, your job is done. Do NOT:
- Invent new tasks
- Continue "cleaning up" nearby code
- Start auditing anything
- Write summaries in chat

Do:
- Stop, silently
- Leave PROGRESS.md in its final state
- Leave the repo in its final committed state (clean working tree)

The operator will read PROGRESS.md, review the commits, and decide next steps.
