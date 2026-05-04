# Calls E2E — Playwright harness for live WebRTC P2P call smoke

Standalone Playwright suite that drives **real Chromium** against the
**live local Orbit stack** (gateway + NATS + coturn), authenticates two
seeded users in isolated browser contexts, and verifies a full P2P
call flow: ICE handshake → bytes flowing in both directions → hangup
clearing state.

This is **separate** from `web/tests/playwright/`, which runs against a
mocked frontend with stubbed WS — calls cannot be validated that way.

## Scope

- ✅ Chromium with fake media stream (`--use-fake-device-for-media-stream`)
- ⏳ Firefox / WebKit — would need separate fake-media flag handling
- ⏳ TURN-relay fallback — requires disabling host candidates mid-test
- ❌ Group / SFU calls (3+ participants) — out of scope

## Status (2026-05-04)

Scaffold lands but the test does **not yet pass end-to-end**. The
remaining gap is selector tuning for two surfaces:

1. **Chat selection** — `.ListItem` first-match clicks the wrong chat
   on stacks with seed group chats. Need to either filter by the known
   direct-chat ID `35268255-199f-43a7-b469-c65950bb9641` (queried at
   test setup) or use the search-then-pick path with a deterministic
   peer name.
2. **Call/Accept/Hangup buttons** — current selectors are best-effort
   regex over aria-label/title; the actual app uses icon-class
   selectors that don't expose accessible names. Concrete path: read
   `web/src/components/calls/*` for the real selectors.

What works so far:
- Login (email + password fields, Enter to submit)
- Both browser contexts launch with fake media + permissions
- RTCPeerConnection instrumentation (window.__orbitPCs registry)
- Wait helpers for ICE state and media flow

## Pre-requisites

1. Full local stack up: `docker compose up -d`
2. Web dev server: `cd web && npm run dev` (or use the docker `web`
   container). Default port: `3000`.
3. Seeded users with known passwords. The test uses `test@orbit.local`
   and `user2@orbit.local`. Both need password `LoadTest!2026` — reset
   via SQL if drifted:
   ```sql
   UPDATE users SET password_hash = (
     SELECT password_hash FROM users WHERE email = 'loadtest_0@orbit.local'
   ) WHERE email IN ('test@orbit.local', 'user2@orbit.local');
   ```
4. Direct chat between the two users. Verify via:
   ```sql
   SELECT c.id FROM chats c
   JOIN chat_members a ON a.chat_id=c.id AND a.user_id=(SELECT id FROM users WHERE email='test@orbit.local')
   JOIN chat_members b ON b.chat_id=c.id AND b.user_id=(SELECT id FROM users WHERE email='user2@orbit.local')
   WHERE c.type='direct';
   ```

## Run

```bash
cd tests/calls-e2e
npm install
npx playwright install chromium
npx playwright test --project=chromium-fake-media
# headed (watch the actual browsers):
npx playwright test --headed --project=chromium-fake-media
```

Override target URL via `ORBIT_URL=http://localhost:3000` if the dev
server is on a different port.
