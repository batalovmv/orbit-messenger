# Slot 02: Auth

## Status

COMPLETED

## Scope

- `services/auth/`
- Focus: JWT access/refresh rotation, Redis blacklist fail-closed, bcrypt cost, 2FA TOTP, invite-only registration, session management, password reset, refresh token atomic `GetDel`, timing attacks on login

## File Checklist

- [x] `services/auth/cmd/main.go`
- [x] `services/auth/internal/handler/auth_handler.go`
- [x] `services/auth/internal/handler/auth_handler_test.go`
- [x] `services/auth/internal/model/models.go`
- [x] `services/auth/internal/service/auth_service.go`
- [x] `services/auth/internal/store/invite_store.go`
- [x] `services/auth/internal/store/session_store.go`
- [x] `services/auth/internal/store/user_store.go`
- [x] `services/auth/Dockerfile`
- [x] `services/auth/go.mod`
- [x] `services/auth/go.sum`

## Findings

### [CRITICAL] Public bootstrap endpoint lets the first remote caller mint a `superadmin`

- Evidence:
  - `POST /auth/bootstrap` is registered with no `requireAuth`, no internal-token gate, and no bootstrap secret or one-time proof in front of it: `services/auth/internal/handler/auth_handler.go:43-52`.
  - The handler accepts arbitrary email/password/display name from the request body and forwards them directly into `AuthService.Bootstrap()`: `services/auth/internal/handler/auth_handler.go:109-134`.
  - `Bootstrap()` hashes the supplied password and inserts a user with `Role: "superadmin"` whenever `CreateIfNoAdmins()` sees an empty admin set: `services/auth/internal/service/auth_service.go:48-68`, `services/auth/internal/store/user_store.go:100-116`.
- Impact:
  - On a fresh deployment, restored empty database, or any environment where the admin rows are missing, the first external request can seize permanent superadmin access and then mint invites / reset admins.
- Pass 2 verification:
  - Confirmed statically from the route registration and service flow above.
  - Existing tests exercise `/auth/bootstrap` anonymously and expect it to succeed, which matches the live behavior: `services/auth/internal/handler/auth_handler_test.go:341-349`.
- Remediation:
  - Move bootstrap behind an internal-only path or a one-time bootstrap secret, and hard-disable the route after initial provisioning.

### [MEDIUM] Revoking a session does not revoke that session's already-issued access JWT

- Evidence:
  - Access tokens contain only `sub`, `role`, `iat`, and `exp`; there is no `session_id` / `jti` claim that ties them back to a row in `sessions`: `services/auth/internal/service/auth_service.go:490-538`.
  - `ValidateAccessToken()` checks only per-token blacklist state and per-user invalid-before state in Redis. It never checks whether the backing session row still exists: `services/auth/internal/service/auth_service.go:442-485`.
  - `RevokeSession()` only deletes the session row, and `DeleteByID()` is just `DELETE FROM sessions ...`: `services/auth/internal/service/auth_service.go:305-315`, `services/auth/internal/store/session_store.go:81-91`.
  - `Logout()` blacklists only the single presented access token, so revoking another device/session from the sessions UI cannot invalidate that device's current access JWT: `services/auth/internal/service/auth_service.go:175-210`.
- Impact:
  - If a user revokes a stolen session, the attacker still keeps using the already-issued access token until it expires. With the current default config that window is up to 15 minutes: `services/auth/cmd/main.go:28-39`.
- Pass 2 verification:
  - Confirmed statically: there is no code path that can derive "this access JWT belonged to deleted session X", so the token remains valid until `exp`.
- Remediation:
  - Bind access tokens to a session identifier or revocation version and reject tokens whose session has been deleted or rotated.

## Low Bucket

- No auth-local rate limiting is visible in `services/auth/` for `/auth/login`, `/auth/register`, `/auth/refresh`, or `/auth/reset-admin`. If gateway protections are bypassed or misrouted, brute force and reset-key guessing hit the service directly.
- `go test ./...` in `services/auth/` is currently red for non-security reasons: the handler mocks never mark bootstrapped users as active, and bootstrap tests still expect `admin` while the service creates `superadmin`.
- Coverage gaps: no tests for refresh replay rejection, Redis fail-closed behavior on access-token validation, immediate access-token invalidation after session revoke, or the `reset-admin` happy path.

## Notes

- Pass 1: route map, token lifecycle, invite path, TOTP path, session path.
- Pass 2: re-verified candidate issues against the exact validation/revocation code paths and rejected weaker suspicions.
- Verification command run: `go test ./...` from `services/auth/` (fails in handler tests; captured in Low Bucket, not used as a primary security finding).
