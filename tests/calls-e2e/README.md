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

**End-to-end passing** in 6-7 s on the local stack. Tested observation:

```
[bob] ICE state=connected after 535ms
[alice] ICE state=connected after 547ms
[bob] media flowing: sent=69B recv=801B after 10ms
[alice] media flowing: sent=801B recv=69B after 12ms
```

Stable selectors used:

| Surface | Selector |
|---|---|
| Email/password fields | `#sign-in-email` / `#sign-in-password` (with `pressSequentially` — fill() races Teact's controlled-state binding) |
| Submit button | `getByRole('button', { name: /next\|далее/i })` |
| LeftColumn ready | `#LeftColumn` |
| Direct chat row | `.Chat.chat-item-clickable.private` filtered by display name regex (Saved Messages and bots also match `.private`) |
| Call header button | `button[aria-label="Call"]` |
| Accept (incoming) | `button:has(.icon-phone-discard)` first — accept renders before hangup in PhoneCall.tsx |
| Hangup (active) | same first()-match — accept disappears after pickup, leaving only hangup |

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
