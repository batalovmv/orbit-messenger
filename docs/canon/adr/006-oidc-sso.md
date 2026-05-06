# ADR 006 — OIDC SSO (single provider, env-config)

Date: 2026-05-05
Status: Accepted (MVP slice)

## Context

Pilot rollout (150 corporate users) is blocked on the absence of an SSO
flow. Issuing invite codes to every employee is operationally noisy and
de-provisioning a leaver requires a manual delete in the admin panel —
nothing watches the corporate directory.

Long-term we want: full OIDC against the customer's IdP (Google
Workspace / Azure AD / Okta), automatic deactivation when the employee
is removed there, and an admin UI to register additional providers.

## Decision (this slice)

Implement OIDC as a single provider configured via environment
variables. Deliberately **out of scope** for this slice:

- Admin UI for managing providers (one provider per deployment for now).
- `oidc_providers` DB table (env wins; revisit when we need per-tenant).
- Periodic SCIM/Directory sync (see "Deactivation" below).
- SAML, magic-link, social-login.
- FE login button (will be added in a follow-up once the BE flow is
  validated end-to-end with a mock provider).

Invite-code registration **stays** for external contractors and
loadtest fixtures.

## Architecture

### Routes (services/auth)

```
GET  /auth/oidc/{provider}/authorize  → 302 to provider authorize URL
GET  /auth/oidc/{provider}/callback   → exchange + cookie + 302 to FrontendURL
```

`{provider}` is matched against the env-configured provider key. Only
one is wired in this slice; the path segment exists so adding a second
in a follow-up doesn't break URL contracts.

### Configuration

```
OIDC_PROVIDER_KEY=google                 # path segment
OIDC_ISSUER=https://accounts.google.com  # discovery base
OIDC_CLIENT_ID=...
OIDC_CLIENT_SECRET=...                   # never logged, env-only
OIDC_REDIRECT_URL=https://app.example/api/v1/auth/oidc/google/callback
OIDC_ALLOWED_EMAIL_DOMAINS=example.com,subsidiary.example.com  # CSV; empty = any
```

If `OIDC_PROVIDER_KEY` is empty, the routes return 404 — feature is
off. Allows the same binary to ship with or without SSO.

### Library choice

`github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2`. Hand-rolling
JWKS rotation and id_token signature verification is a footgun we are
not paid to maintain. Both libs are stdlib-flavoured, no new transient
dependency surface beyond what they pull in.

### Flow

1. `/authorize` generates a random `state` (32 random bytes, base64-url)
   and a PKCE `verifier`/`challenge` (S256). Stashes
   `{verifier, nonce, return_to}` in Redis at key
   `oidc:state:{state}` with TTL 5 minutes. Redirects the browser to
   the provider's authorize URL with `state`, `code_challenge`,
   `nonce`, `scope=openid email profile`.
2. `/callback` reads `state` query param, fetches the Redis entry
   (single-use: `GETDEL`). If absent → 400. Otherwise exchanges `code`
   for tokens (sends `code_verifier`), validates the id_token (issuer,
   audience, nonce, expiry — done by go-oidc), extracts `sub` and
   `email`. Email domain is checked against `OIDC_ALLOWED_EMAIL_DOMAINS`
   if non-empty — mismatched email → 403 with audit log entry.
3. User resolution:
   - If a user with `oidc_subject = sub AND oidc_provider = key` exists
     → that's the user.
   - Else if a user with `email = id_token.email AND oidc_subject IS NULL`
     exists → bind the subject to that user (one-time email→OIDC link).
   - Else create a new user (role=member, password_hash blank,
     `is_active = true`) and call `joinDefaultChatsBestEffort` so the
     existing welcome flow runs.
4. Issue a normal access+refresh token pair via `createTokenPair`. Set
   refresh cookie with the same options used by `/auth/login`. Redirect
   to `OIDC_FRONTEND_URL || cfg.FrontendURL` (with `?return_to=` honoured
   from the original `/authorize` request, if it was a same-origin path).

### Schema (migration 070)

```sql
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS oidc_subject  TEXT,
    ADD COLUMN IF NOT EXISTS oidc_provider TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_oidc_identity
    ON users (oidc_provider, oidc_subject)
    WHERE oidc_subject IS NOT NULL;
```

Both columns nullable so password users coexist. Partial unique index
means a user can be linked to at most one provider but allows nulls
freely.

### Deactivation (deferred from this slice)

Approach selected for the next slice: a `services/auth` worker (one
goroutine, hourly tick) that, for each user with non-null
`oidc_provider`, calls the provider's directory API and flips
`is_active = false` + revokes all sessions when the upstream user is
gone or `suspended`. Provider-specific code lives behind a small
`DirectoryClient` interface (Google: Workspace Directory API; Azure:
Microsoft Graph; Okta: Users API). This slice ships **without** the
worker — manual `POST /admin/users/:id/deactivate` is the safety net.

## Security notes

- PKCE is mandatory (S256), even though we're a confidential client —
  closes the auth-code interception path.
- `state` is single-use via Redis `GETDEL`.
- `nonce` is checked by go-oidc against the id_token claim — replay
  protection across browser sessions.
- `OIDC_CLIENT_SECRET` is env-only; never logged. Saturn deployment
  reads it from the existing secret-injection mechanism (same as
  `INTERNAL_SECRET` and `JWT_SECRET`).
- `return_to` is sanitised: only same-origin paths with no `//` prefix
  are honoured; otherwise we redirect to the configured frontend root.

## Tests

Unit tests in `services/auth/internal/service/oidc_test.go`:

- Authorize: state stored in Redis, redirect URL contains
  `code_challenge_method=S256` and a 43-char `code_challenge`.
- Callback happy path: existing user (matched by `oidc_subject`).
- Callback link path: existing user (matched by email; subject bound).
- Callback create path: new user created + welcome-flow side effect
  invoked.
- Callback rejects: unknown state, bad nonce, expired id_token,
  email-domain not allowed.

go-oidc's discovery is mocked with an httptest server returning a
canned `/.well-known/openid-configuration` + JWKS. The id_token is
signed with a generated RSA key whose pubkey is served via the JWKS.

## Out of scope reminders

- No FE changes in this PR.
- No admin UI.
- No directory-sync worker.
- No multi-provider DB table.

These are tracked as separate items in `docs/NEXT-SESSION-PLAN.md`.
