# Slot 11 Audit Report

## Status: COMPLETED

- Slot: `11`
- Name: `saturn-client`
- Scope: `web/src/api/saturn/`
- Commit: `82669bd35a1568f24eff710b1dd0074342f12dff`
- Focus: `fetch retry logic`, `auth refresh on 401`, `token storage`, `error normalization`, `cancellation`, `timeout handling`, `concurrent request dedupe`, `URL construction`, `response type safety`, `naming conventions`
- Severity gate: report `HIGH` / `CRITICAL` individually, `MEDIUM` only with high confidence, `LOW` in bucket only

## Checklist

- [x] `web/src/api/saturn/client.ts`
- [x] `web/src/api/saturn/index.ts`
- [x] `web/src/api/saturn/types.ts`
- [x] `web/src/api/saturn/apiBuilders/avatars.ts`
- [x] `web/src/api/saturn/apiBuilders/chats.ts`
- [x] `web/src/api/saturn/apiBuilders/messages.test.ts`
- [x] `web/src/api/saturn/apiBuilders/messages.ts`
- [x] `web/src/api/saturn/apiBuilders/reactions.ts`
- [x] `web/src/api/saturn/apiBuilders/symbols.test.ts`
- [x] `web/src/api/saturn/apiBuilders/symbols.ts`
- [x] `web/src/api/saturn/apiBuilders/users.ts`
- [x] `web/src/api/saturn/methods/auth.test.ts`
- [x] `web/src/api/saturn/methods/auth.ts`
- [x] `web/src/api/saturn/methods/calls.ts`
- [x] `web/src/api/saturn/methods/chats.test.ts`
- [x] `web/src/api/saturn/methods/chats.ts`
- [x] `web/src/api/saturn/methods/client.test.ts`
- [x] `web/src/api/saturn/methods/client.ts`
- [x] `web/src/api/saturn/methods/compat.test.ts`
- [x] `web/src/api/saturn/methods/compat.ts`
- [x] `web/src/api/saturn/methods/index.test.ts`
- [x] `web/src/api/saturn/methods/index.ts`
- [x] `web/src/api/saturn/methods/init.ts`
- [x] `web/src/api/saturn/methods/media.ts`
- [x] `web/src/api/saturn/methods/messages.test.ts`
- [x] `web/src/api/saturn/methods/messages.ts`
- [x] `web/src/api/saturn/methods/reactions.test.ts`
- [x] `web/src/api/saturn/methods/reactions.ts`
- [x] `web/src/api/saturn/methods/search.ts`
- [x] `web/src/api/saturn/methods/settings.ts`
- [x] `web/src/api/saturn/methods/settingsApi.ts`
- [x] `web/src/api/saturn/methods/symbols.test.ts`
- [x] `web/src/api/saturn/methods/symbols.ts`
- [x] `web/src/api/saturn/methods/sync.ts`
- [x] `web/src/api/saturn/methods/twoFaSettings.ts`
- [x] `web/src/api/saturn/methods/types.ts`
- [x] `web/src/api/saturn/methods/users.ts`
- [x] `web/src/api/saturn/updates/apiUpdateEmitter.ts`
- [x] `web/src/api/saturn/updates/wsHandler.ts`
- [x] `web/src/api/saturn/worker/connector.ts`
- [x] `web/src/api/saturn/worker/types.ts`

## Findings

### [HIGH] Invite hash path injection can retarget authenticated requests to arbitrary API endpoints

- Files: `web/src/api/saturn/methods/chats.ts:442-448`, `web/src/api/saturn/client.ts:143-149`
- `fetchChatInviteInfo()` and `joinChat()` splice raw `hash` into a path segment and pass it to `client.request()`, which concatenates `effectiveBase + path` and calls `fetch()` with `credentials: 'include'` and the bearer token header when present.
- Because the hash is not `encodeURIComponent()`-encoded, a crafted value like `../../auth/logout` rewrites the target URL after dot-segment normalization. Local verification:
  - `/chats/join/../../auth/logout -> https://orbit.local/api/v1/auth/logout`
  - `/chats/join/../../users/me/blocked/attacker-id -> https://orbit.local/api/v1/users/me/blocked/attacker-id`
- Impact: a malicious invite link can turn the invite flow into a same-origin request gadget. `fetchChatInviteInfo()` becomes an arbitrary authenticated `GET` under `/api/v1/*`, and `joinChat()` becomes an arbitrary authenticated `POST` under `/api/v1/*` once the victim clicks join. This is exploitable without XSS.
- Fix: reject `/`, `\`, `?`, `#`, and dot segments in invite hashes and always encode path params before interpolation. Safer option: send invite codes in query/body instead of raw path segments.

### [MEDIUM] The client never retries on response-side `401`, so valid refresh cookies do not recover failed requests or uploads

- Files: `web/src/api/saturn/client.ts:77-105`, `web/src/api/saturn/client.ts:123-159`, `web/src/api/saturn/methods/media.ts:82-123`, `web/src/api/saturn/methods/media.ts:144-164`
- `request()` only refreshes preflight when the locally cached expiry is near; if the backend returns `401` anyway, the error is surfaced immediately and the request is not retried after refresh. The upload paths bypass `request()` entirely, so they also skip centralized refresh handling.
- Real impact: clock skew, backend-side token revocation, cross-tab auth churn, or stale in-memory expiry state can make message sends/uploads fail even though the refresh cookie is still valid and the session could have been recovered transparently.
- Fix: on first authenticated `401`, serialize a refresh attempt and retry once. Route upload code through the same refresh/error path or preflight refresh immediately before XHR/chunk upload.

## Low Severity Bucket

- No default timeout exists for `refreshToken()`, `request()`, `uploadSimpleMedia()`, `uploadChunk()`, or binary downloads, so hung sockets can stall auth recovery, uploads, and reconnect flows indefinitely.
- Access tokens are persisted in `sessionStorage` (`web/src/api/saturn/client.ts:204-226`) instead of staying memory-only, which increases token exposure if any DOM XSS lands elsewhere in the app.
- Error normalization is inconsistent outside `request()`: raw XHR/fetch upload paths throw generic `Error` strings, so callers lose `ApiError.code/status` semantics.
- Fetch retry logic is effectively absent for transient network/`5xx` failures; the only dedupe present is request coalescing, not resilience.
- Type safety is weakened by many `request<any>` / `Promise<any>` call sites in `methods/chats.ts`, `methods/index.ts`, and related adapters, which makes response-shape regressions easier to miss.
- Dynamic path segments are broadly interpolated raw across the client. Most current IDs are UUID-like, but the helper should encode path params centrally instead of relying on caller discipline.
- Naming convention drift exists in a few public methods (`faveSticker`, `getDhConfig`, `repairFileReference`, `setChatMuted`) versus the stated `camelCase` + verb-first guideline.

## Pass 2 Verification

- Reviewed every file under `web/src/api/saturn/`, including all Saturn client tests and builders.
- Reproduced the invite-hash URL rewrite locally with PowerShell URI normalization:
  - `[uri]::new('https://orbit.local/api/v1/chats/join/../../auth/logout').AbsoluteUri`
  - Result: `https://orbit.local/api/v1/auth/logout`
- Ran targeted frontend tests:
  - `npm test -- --runTestsByPath src/api/saturn/methods/chats.test.ts src/api/saturn/methods/auth.test.ts src/api/saturn/methods/index.test.ts src/api/saturn/methods/messages.test.ts src/api/saturn/methods/reactions.test.ts src/api/saturn/methods/client.test.ts src/api/saturn/methods/compat.test.ts src/api/saturn/methods/symbols.test.ts src/api/saturn/apiBuilders/messages.test.ts src/api/saturn/apiBuilders/symbols.test.ts`
  - Result: `10` suites passed, `42` tests passed.
- No additional `HIGH` / `CRITICAL` issues survived pass 2 verification.
