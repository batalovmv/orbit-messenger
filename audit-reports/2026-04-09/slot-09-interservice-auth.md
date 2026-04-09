# Slot 09 — Inter-service Auth

## Status: COMPLETED

## Scope

- `services/gateway/internal/middleware/`
- all `*_handler.go` files under `services/*/internal/handler/`
- audit focus limited to `getUserID` helpers and reads of `X-User-ID` / `X-Internal-Token`

## Pass Plan

- [x] Pass 1: inventory files and auth-entry points
- [x] Pass 2: verify exploitability and severity
- [x] Finalize report

## File Checklist

- [x] `services/gateway/internal/middleware/cors.go`
- [x] `services/gateway/internal/middleware/jwt.go`
- [x] `services/gateway/internal/middleware/logging.go`
- [x] `services/gateway/internal/middleware/ratelimit.go`
- [x] `services/gateway/internal/middleware/ratelimit_test.go`
- [x] `services/gateway/internal/middleware/security_headers.go`
- [x] `services/auth/internal/handler/auth_handler.go`
- [x] `services/calls/internal/handler/call_handler.go`
- [x] `services/calls/internal/handler/sfu_handler.go`
- [x] `services/media/internal/handler/media_handler.go`
- [x] `services/media/internal/handler/upload_handler.go`
- [x] `services/messaging/internal/handler/admin_handler.go`
- [x] `services/messaging/internal/handler/chat_handler.go`
- [x] `services/messaging/internal/handler/gif_handler.go`
- [x] `services/messaging/internal/handler/invite_handler.go`
- [x] `services/messaging/internal/handler/message_handler.go`
- [x] `services/messaging/internal/handler/poll_handler.go`
- [x] `services/messaging/internal/handler/reaction_handler.go`
- [x] `services/messaging/internal/handler/scheduled_handler.go`
- [x] `services/messaging/internal/handler/search_handler.go`
- [x] `services/messaging/internal/handler/settings_handler.go`
- [x] `services/messaging/internal/handler/sticker_handler.go`
- [x] `services/messaging/internal/handler/user_handler.go`

## Findings

### HIGH: `messaging` trust boundary collapses to caller-controlled `X-User-ID`

- Evidence:
  - `services/messaging/internal/handler/chat_handler.go:640-649` defines the shared `getUserID` helper for the whole package and it authorizes exclusively from `c.Get("X-User-ID")`.
  - `services/messaging/internal/handler/invite_handler.go:35-39` explicitly registers `JoinByInvite` on a "public" route set while `services/messaging/internal/handler/invite_handler.go:150-166` still authenticates with the same header-only `getUserID`.
  - Pass 2 verification found **103** `getUserID(c)` call sites across 12 messaging handler files and **0** `X-Internal-Token` checks anywhere under `services/messaging/internal/handler/`.
- Why this is exploitable:
  - Any actor that can reach the messaging service directly on its service port, or any internal service that can be turned into an SSRF pivot, can send an arbitrary `X-User-ID` and act as that user.
  - That bypasses the intended JWT -> gateway -> internal-header trust transition. In practice the gateway becomes a convention, not an enforced security boundary, because the backend service accepts the identity header without proving the request was gateway-originated.
- Impact:
  - Full user impersonation across messaging flows that reuse `getUserID`: chat CRUD, messages, invites, admin actions, reactions, polls, scheduled messages, search, settings, stickers, and user endpoints.
  - The `RegisterPublic` invite flow raises the risk further because one of the routes is already documented as bypassing gateway JWT checks while still consuming the same identity header.
- Severity rationale:
  - I am keeping this at **HIGH** rather than CRITICAL because the exploit requires direct reachability to `messaging` or an east-west/SSRF foothold, not guaranteed public internet access from this scope alone.

## Low Bucket

- `services/auth/internal/handler/auth_handler.go`, `services/calls/internal/handler/call_handler.go`, `services/media/internal/handler/media_handler.go`, and `services/media/internal/handler/upload_handler.go` compare `X-Internal-Token` with `subtle.ConstantTimeCompare`, which is correct as a constant-time primitive, but the contract is still a replayable shared bearer secret rather than a per-request HMAC/authenticated envelope. Any service compromise that exposes `INTERNAL_SECRET` can mint arbitrary cross-service identity headers.
- `services/calls/internal/handler/call_handler.go:68-76` defines `RequireInternalToken`, and `services/calls/internal/handler/sfu_handler.go:33-35` says route wiring applies it in `main.go`, but the handlers that read `X-User-ID` do not enforce that locally. This is brittle and should be locked down with explicit handler-level tests so a future route-registration change does not silently expose the same bug class as `messaging`.
- `services/gateway/internal/middleware/ratelimit_test.go:30-34` intentionally seeds `Locals("userID")` from a raw `X-User-ID` request header. It is test-only, but it normalizes the exact spoofing pattern the production code is supposed to reject.
