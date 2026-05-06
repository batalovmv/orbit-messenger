# Local OIDC smoke test with Dex

Bring up an in-stack IdP and prove `orbit-auth` can complete the OAuth2
authorization-code + PKCE round-trip end to end. **Dev only — not for prod.**

## What you get

- A `dex` container on `127.0.0.1:5556` with one static OAuth client
  (`orbit-local`) and one static user (`alice@orbit.local` /
  `LoadTest!2026`).
- A redirect URI pointing at the gateway, so the OIDC callback exercises the
  same proxy path that production does.

## One-time setup

1. Make sure the rest of the stack is running:
   ```bash
   docker compose up -d
   ```
2. Bring up Dex via the `oidc-dev` profile:
   ```bash
   docker compose --profile oidc-dev up -d dex
   ```
3. Add OIDC env to `orbit-auth`. The simplest path is a `.env.oidc` file
   you source into your shell, then restart auth:
   ```bash
   OIDC_PROVIDER_KEY=dex
   OIDC_PROVIDER_DISPLAY_NAME=Dex (local)
   OIDC_ISSUER=http://dex:5556/dex
   OIDC_CLIENT_ID=orbit-local
   OIDC_CLIENT_SECRET=local-dev-secret
   OIDC_REDIRECT_URL=http://localhost:8080/api/v1/auth/oidc/dex/callback
   OIDC_FRONTEND_URL=http://localhost:3000/
   OIDC_ALLOWED_EMAIL_DOMAINS=orbit.local
   ```
   Inject those via `docker compose run --env-file .env.oidc auth …` or
   patch the `auth` service env section in compose for the duration of the
   smoke run, then `docker compose up -d --force-recreate auth`.
4. Confirm auth picked up the provider:
   ```bash
   docker compose logs auth | grep "oidc: provider ready"
   ```
   You should see `oidc: provider ready key=dex issuer=http://dex:5556/dex`.

## The smoke loop

1. Open `http://localhost:3000/` — the login screen should now show a full-
   width "Sign in with Dex (local)" button above the email/password form.
2. Click it. The browser navigates to Dex, prompts for credentials, and
   redirects back through the gateway.
3. After the callback you should land on `http://localhost:3000/` already
   logged in as alice (no login flash, address bar clean of `access_token`).
4. Check the auth log for the linking decision:
   ```bash
   docker compose logs auth | grep "oidc:"
   ```
   First successful run on a fresh DB shows `oidc: created new user`. A
   second run (or one where you've already invited `alice@orbit.local`)
   shows `oidc: linked existing user`.

## Manual deactivation test (preview of B4 sync worker)

Until the directory-sync worker (B4) lands you can simulate a deprovisioned
user by editing `deploy/dex/config.yaml`, removing alice's static-password
entry, and restarting Dex. The next sign-in attempt should hit Dex's "no
such user" path and bounce back to the login screen.

## Cleanup

```bash
docker compose --profile oidc-dev down dex
docker volume rm orbit_dex_data
```

Then revert the `OIDC_*` env on auth to disable SSO.
