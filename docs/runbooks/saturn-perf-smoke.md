# Saturn perf smoke — k6 WS load against prod gateway

> Created 2026-05-05. Existing k6 script (`tests/load/k6-150-ws.js`) +
> token minter (`mint-tokens.go`) work as-is against any URL. The only
> bits that change for Saturn-vs-local are env vars: `BASE_URL`,
> `DATABASE_URL`, `JWT_SECRET`.

---

## What this measures

150 concurrent WebSocket clients holding the gateway open + completing
auth handshake. Reports `ws_connect_duration`, `ws_auth_ack_duration`,
`ws_disconnects` percentiles. Pairs with the prior local run (p95 8 ms
connect, 0 disconnects) to confirm prod cold-start + Saturn network
hop don't degrade the steady state.

---

## Prerequisites

- Saturn URL for gateway. From `.saturn.yml` mapping +
  Saturn-assigned hostname:
  ```
  gateway → new-tg-w2bvpo.saturn.ac
  ```
  WS endpoint: `wss://new-tg-w2bvpo.saturn.ac/api/v1/ws`.
  Verify with `curl -s -o /dev/null -w "%{http_code}\n" https://new-tg-w2bvpo.saturn.ac/health`
  → expect `200`.

- Read-only access to prod `DATABASE_URL` and `JWT_SECRET` (Saturn
  → orbit-gateway → Environment variables).

- 150 load-test user rows in prod DB. **Do not run this against the
  prod database** unless you've seeded a `loadtest_*` schema or
  accept the rows in `users` table. Recommended path: spin up a
  staging Saturn project mirroring prod env, seed there, run k6 against
  the staging gateway.

---

## Step 1 — Seed users (if not already present)

```bash
psql "$DATABASE_URL" -f tests/load/seed-loadtest-users.sql
```

This inserts 150 users with emails `loadtest-001@orbit.test` …
`loadtest-150@orbit.test` and a deterministic password hash. Idempotent
on re-run.

---

## Step 2 — Mint tokens

```bash
cd tests/load
DATABASE_URL='postgres://...' \
JWT_SECRET='...' \
go run mint-tokens.go > tokens.json
```

Output: `tokens.json` with 150 `{email, token}` entries.

Why mint, not log in: gateway+auth share a 5/min/IP rate-limit on
`/auth/login`, so 150 sequential setup logins from one k6 container
would take 30 min and trip the limiter. We hold `JWT_SECRET` on the
test machine, so we sign locally and skip the hot path.

---

## Step 3 — Run k6 against Saturn

```bash
docker run --rm -i \
  -v "$(pwd)/tests/load:/scripts" \
  -e BASE_URL=wss://new-tg-w2bvpo.saturn.ac \
  -e USERS=150 \
  -e DURATION=60s \
  grafana/k6:0.50.0 run /scripts/k6-150-ws.js
```

Note the schema is `wss://` (TLS) for Saturn, vs `ws://` locally.

---

## Step 4 — Compare against local baseline

Baseline (local, 2026-05-04):

| metric | p95 |
|---|---|
| ws_connect_duration | ~8 ms |
| ws_auth_ack_duration | ~10 ms |
| ws_disconnects | 0 |

Saturn target (worst-case acceptance for pilot):

| metric | p95 max |
|---|---|
| ws_connect_duration | < 200 ms (TLS + transcontinental hop) |
| ws_auth_ack_duration | < 250 ms |
| ws_disconnects | 0 |

If Saturn is materially worse:
- > 500 ms p95 connect → likely gateway under-provisioned or coturn
  blocking; check Saturn → orbit-gateway → Observability → Resource usage
- Any non-zero disconnects → gateway logs for "ws hub closed connection"
  or "auth timeout"

---

## Artefact

After the run, save the k6 stdout + Saturn dashboard screenshots to:

```
audits/load-2026-05-DD-saturn.md
```

Sections: setup (DB rows / tokens count / k6 image), URL, env, full k6
summary table, gateway resource graphs from Saturn for the run window,
verdict + any follow-ups.
