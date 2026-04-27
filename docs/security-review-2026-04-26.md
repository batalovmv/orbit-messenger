# Security Review — 2026-04-26

**Scope**: services/auth + services/gateway  
**Method**: Two-pass audit via opus-code-reviewer  
**Date**: 2026-04-26

## Summary

| Severity | Count | Status |
|----------|-------|--------|
| P0 | 0 | — |
| P1 | 5 | All fixed |
| P2 | 6 | All fixed |
| P3 | 3 | Deferred to backlog |

**Total findings**: 14 (5 P1, 6 P2, 3 P3)  
**Fixed**: 11  
**Deferred**: 3

---

## P1 Findings (Critical — Fixed)

### P1-1: Gateway SFU proxy — no read size limit
**File**: `services/gateway/internal/handler/sfu_proxy.go:105`  
**Issue**: Bidirectional pump has no `SetReadLimit` on upstream or client connections. Malicious calls-service could send arbitrarily large frame.  
**Fix**: Add `SetReadLimit(256 * 1024)` on both connections after dial.  
**Status**: ✅ Fixed in commit `fix(gateway): add read limits to SFU proxy connections`

### P1-2: Gateway SFU proxy — callID not validated as UUID
**File**: `services/gateway/internal/handler/sfu_proxy.go:111`  
**Issue**: `callID` from URL params concatenated into upstream URL without UUID validation. Path traversal possible.  
**Fix**: Add `uuid.Parse(callID)` validation before use.  
**Status**: ✅ Fixed in commit `fix(gateway): validate callID as UUID in SFU proxy`

### P1-3: Auth — AdminRevokeSession blacklist TTL hardcoded
**File**: `services/auth/internal/service/auth_service.go:624`  
**Issue**: User blacklist TTL hardcoded to 24h instead of using `AccessTTL` (15min default). Locks user out too long or too short depending on config.  
**Fix**: Use `s.cfg.AccessTTL` as TTL.  
**Status**: ✅ Fixed in commit `fix(auth): use AccessTTL for user blacklist expiry`

### P1-4: Auth — TOTP codes not single-use (replay attack)
**File**: `services/auth/internal/service/auth_service.go:106, :371`  
**Issue**: TOTP codes can be replayed within 30s window. No used-code tracking.  
**Fix**: Store used codes in Redis with 90s TTL (covers ±1 window drift).  
**Status**: ✅ Fixed in commit `fix(auth): prevent TOTP code replay attacks`

### P1-5: Auth — no rate limiting at service level
**File**: `services/auth/cmd/main.go:139`  
**Issue**: Auth service has zero rate limiting. Only gateway has limits. Direct access to port 8081 allows unlimited brute-force.  
**Fix**: Add Redis-backed rate limiting on `/login` (5/min), `/register` (10/min), `/reset-admin` (5/min).  
**Status**: ✅ Fixed in commit `fix(auth): add service-level rate limiting`

---

## P2 Findings (High — Fixed)

### P2-1: Gateway WebRTC signaling — no call membership check (IDOR)
**File**: `services/gateway/internal/ws/handler.go:527-569`  
**Issue**: `handleSignalingRelay` delivers WebRTC frames to any user without verifying both sender and recipient are in the call. User A can spam user B with signaling for arbitrary calls.  
**Fix**: Check Redis set `call_members:<callID>` before relay (populated by calls service on join/leave).  
**Status**: ✅ Fixed in commit `fix(gateway): add call membership check to WebRTC signaling`

### P2-2: Gateway token revalidation — orphaned HTTP requests on disconnect
**File**: `services/gateway/internal/ws/token_revalidation.go:44`  
**Issue**: Revalidation goroutine uses `context.Background()`. If connection closes mid-HTTP-call, request runs to completion (10s). Reconnect storms create burst of orphaned requests.  
**Fix**: Derive revalidation context from connection-scoped context cancelled on `conn.done`.  
**Status**: ✅ Fixed in commit `fix(gateway): cancel revalidation on connection close`

### P2-3: Gateway rate limit — userID local overrides custom Identifier
**File**: `services/gateway/internal/middleware/ratelimit.go:55-57`  
**Issue**: `userID` local always wins over custom `Identifier` function. Future routes combining custom identifier + JWT middleware will silently break.  
**Fix**: Invert precedence — only fall back to `userID` when no custom `Identifier` configured.  
**Status**: ✅ Fixed in commit `fix(gateway): fix rate limit identifier precedence`

### P2-4: Auth — refresh cookie SameSite should be Strict
**File**: `services/auth/internal/handler/auth_handler.go:30`  
**Issue**: Refresh token cookie uses `SameSite: "Lax"`. Allows cookie on top-level cross-site navigations. Should be `"Strict"` for refresh tokens.  
**Fix**: Change to `SameSite: "Strict"`.  
**Status**: ✅ Fixed in commit `fix(auth): set refresh cookie SameSite to Strict`

### P2-5: Auth — enabling 2FA does not invalidate existing sessions
**File**: `services/auth/internal/service/auth_service.go:375`  
**Issue**: After user enables 2FA, pre-existing sessions remain valid. Attacker with stolen refresh token retains access.  
**Fix**: Call `DeleteAllByUser` and set `user_tokens_invalid_before` after enabling 2FA.  
**Status**: ✅ Fixed in commit `fix(auth): invalidate sessions when enabling 2FA`

### P2-6: Auth — disabling 2FA does not invalidate existing sessions
**File**: `services/auth/internal/service/auth_service.go:408`  
**Issue**: `Disable2FA` only called `UpdateTOTP` — no session revocation. Attacker who triggered 2FA disable retains all active sessions.  
**Fix**: Call `DeleteAllByUser` after `UpdateTOTP` succeeds (non-fatal on error, sessions expire via AccessTTL).  
**Status**: ✅ Fixed in commit `fix(auth): revoke all sessions on 2FA disable to prevent session fixation`

---

## P3 Findings (Low — Deferred to Backlog)

### P3-1: Gateway proxy — internal headers read from request not locals
**File**: `services/gateway/internal/handler/proxy.go:81-85`  
**Issue**: After stripping client-supplied internal headers, proxy re-adds them via `c.Get()` (reads request headers) instead of `c.Locals()`. Works today but fragile.  
**Recommendation**: Read from `c.Locals("userID")` for re-add.  
**Status**: ⏸️ Deferred — works correctly in practice, low risk

### P3-2: Gateway SFU proxy — InsecureSkipVerify on upstream TLS
**File**: `services/gateway/internal/handler/sfu_proxy.go:105`  
**Issue**: `InsecureSkipVerify: true` on upstream dialer. Acceptable for docker-internal but no env flag to enable verification if topology changes.  
**Recommendation**: Make conditional on `CALLS_INTERNAL=true` env flag.  
**Status**: ⏸️ Deferred — acceptable for current deployment

### P3-3: Auth — bootstrap secret not auto-disabled
**File**: `services/auth/internal/handler/auth_handler.go:153-158`  
**Issue**: `BOOTSTRAP_SECRET` env var remains valid indefinitely. DB guard prevents second admin but secret should be cleared after first use.  
**Recommendation**: Log loud warning on startup if secret set AND admin exists.  
**Status**: ⏸️ Deferred — DB-level guard sufficient

---

## Confirmed Clean Areas

| Area | Verdict |
|------|---------|
| WS auth token in URL | ✅ Token in first WS frame, never in URL |
| CORS `*` + credentials | ✅ Reflects only allowed origins |
| Token revalidation on expiry | ✅ Checks expiry every tick, closes with 1008 |
| Internal header stripping | ✅ Strips x-user-id, x-internal-token, x-trusted-client-ip |
| WS backpressure | ✅ 256-message queue, slow consumers disconnected |
| WS message size limit | ✅ 64KB limit on main WS (SFU fixed separately) |
| NATS subject injection | ✅ uuid.Parse guards all user-supplied IDs |
| JWT signing | ✅ RS256, proper expiry, complete claims |
| Password hashing | ✅ argon2id with secure params |
| Refresh token rotation | ✅ Atomic get-then-delete pattern |

---

## Lessons Learned

1. **Defense in depth**: Gateway rate limiting is not enough — services must have their own limits
2. **TOTP replay**: Standard TOTP libraries don't track used codes — must implement manually
3. **Context cancellation**: Long-lived goroutines need connection-scoped contexts
4. **UUID validation**: Every user-supplied ID must be validated before interpolation
5. **Session invalidation**: Security upgrades (2FA enable, password reset) must invalidate old sessions

---

## Next Steps

- [x] All P1 findings fixed with handler tests
- [x] All P2 findings fixed with handler tests
- [ ] P3 findings tracked in `docs/security-backlog.md`
- [x] Knowledge base updated with lessons
