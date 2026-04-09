# Slot 01 Audit Report: Gateway

## Status: COMPLETED

## Scope

- `services/gateway/`
- Focus: routing, middleware chain, WebSocket hub, proxy timeouts/retries, CORS/CSP headers, rate limiting, `X-Internal-Token` signing, header forwarding (`X-User-ID` trust), push dispatcher, SSE/WS fan-out

## Findings

### [HIGH] Auth session rate limiting can be bypassed with attacker-controlled `Authorization` / `refresh_token` values

- Files: `services/gateway/cmd/main.go:128-133`, `services/gateway/cmd/main.go:238-247`, `services/gateway/internal/middleware/ratelimit.go:33-45`
- The `/api/v1/auth/*` session bucket (`/refresh`, `/me`, `/logout`, `/sessions`, `/2fa/*`, `/invites`) is keyed by `authSessionRateLimitIdentifier`, which prefers any bearer token or `refresh_token` cookie before falling back to IP. These routes run before JWT middleware, so the identifier is fully attacker-controlled.
- An unauthenticated client can send a fresh bogus `Authorization: Bearer <random>` header on every request and get a new Redis key each time, bypassing the intended `60 req/min` ceiling and forcing unbounded proxy traffic into the auth service.
- Impact: practical DoS/brute-force amplification against the auth service on the exact endpoints that were supposed to be behind the session limiter.
- Pass 2 verification: re-checked the route registration in `RegisterAuthProxyRoutes`, the limiter wiring in `cmd/main.go`, and the precedence rules in `RateLimitMiddleware`; there is no trusted identity source on these routes before the custom identifier runs.

### [HIGH] WebSocket and SFU sessions stay authorized after JWT expiry or revocation

- Files: `services/gateway/internal/ws/handler.go:68-158`, `services/gateway/internal/ws/handler.go:161-180`, `services/gateway/internal/handler/sfu_proxy.go:73-168`
- Both `/api/v1/ws` and `/api/v1/calls/:id/sfu-ws` validate the access token exactly once during the first auth frame, then keep only `userID` in connection state. The subsequent read/ping loops never re-check blacklist state or token expiry.
- That means anyone holding a briefly valid access token can keep a socket alive indefinitely with ping/pong, continue receiving chat or call events after logout, and outlive the nominal JWT lifetime entirely.
- Impact: revoked/expired tokens still retain live real-time access until the TCP/WebSocket session drops, which is significant data exposure for stolen-token scenarios.
- Pass 2 verification: re-read both connection lifecycles end-to-end; there is no expiry metadata stored on `Conn`, no periodic `ValidateToken` call, and no revocation-triggered disconnect path.

### [HIGH] `sanitizeProxyPath()` lets external requests break out into upstream `/internal/*` endpoints

- Files: `services/gateway/internal/handler/proxy.go:20-27`, `services/gateway/internal/handler/proxy.go:225-280`
- The gateway matches a public route first and only then runs `sanitizeProxyPath`, which trims `/api/v1` and calls `path.Clean` on the full path. A request such as `/api/v1/chats/../../internal/chats/<id>/member-ids` reaches the generic messaging proxy route, gets cleaned to `/internal/chats/<id>/member-ids`, and is forwarded upstream with `X-Internal-Token`.
- The explicit `/api/v1/internal/*` deny route only blocks paths that already start with `/internal` before sanitization; it does not protect against dot-segment breakout through `/media/*`, `/calls*`, or the generic messaging proxy.
- Impact: authenticated external users can cross the public/internal boundary and hit service-to-service endpoints as if the request originated from the gateway.
- Pass 2 verification: re-checked the route order in `SetupProxy` against the upstream URL construction in each proxy handler; the cleaned path is produced after route selection, so the namespace check is bypassed by design.

## Low Bucket

- `services/gateway/internal/handler/sfu_proxy.go:64-69` disables TLS verification for upstream `wss://` dials via `InsecureSkipVerify: true`.
- `services/gateway/internal/handler/proxy.go:59-69` forwards client-supplied `X-Forwarded-For` / `X-Real-IP` / `Forwarded` headers unchanged to upstream services, which can poison downstream IP-based logging or controls if any service trusts them.
- No regression tests cover dot-segment breakout in `sanitizeProxyPath()` or the session-limiter bucket abuse (`services/gateway/internal/handler/proxy_test.go`, `services/gateway/internal/middleware/ratelimit_test.go`).
- `services/gateway/cmd.exe` is a committed binary artifact inside the service tree.

## Verification

- `go test ./...` in `services/gateway` — passed

## Pass Log

- [x] Pass 1: surface scan completed
- [x] Pass 2: findings re-verified

## File Checklist

- [x] `services/gateway/cmd.exe` (binary artifact only)
- [x] `services/gateway/Dockerfile`
- [x] `services/gateway/go.mod`
- [x] `services/gateway/go.sum`
- [x] `services/gateway/cmd/main.go`
- [x] `services/gateway/cmd/generate-vapid/main.go`
- [x] `services/gateway/internal/handler/health.go`
- [x] `services/gateway/internal/handler/proxy.go`
- [x] `services/gateway/internal/handler/proxy_test.go`
- [x] `services/gateway/internal/handler/sfu_proxy.go`
- [x] `services/gateway/internal/middleware/cors.go`
- [x] `services/gateway/internal/middleware/jwt.go`
- [x] `services/gateway/internal/middleware/logging.go`
- [x] `services/gateway/internal/middleware/ratelimit.go`
- [x] `services/gateway/internal/middleware/ratelimit_test.go`
- [x] `services/gateway/internal/middleware/security_headers.go`
- [x] `services/gateway/internal/push/dispatcher.go`
- [x] `services/gateway/internal/push/dispatcher_test.go`
- [x] `services/gateway/internal/ws/events.go`
- [x] `services/gateway/internal/ws/handler.go`
- [x] `services/gateway/internal/ws/hub.go`
- [x] `services/gateway/internal/ws/hub_test.go`
- [x] `services/gateway/internal/ws/nats_subscriber.go`
- [x] `services/gateway/internal/ws/nats_subscriber_test.go`
