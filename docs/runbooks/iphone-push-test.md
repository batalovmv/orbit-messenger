# iPhone push notification — manual smoke test

> Run after any change to `services/gateway/internal/push/`, the VAPID env, the service worker, or `web/src/serviceWorker/`.
> Estimated time: 5 min on a real iPhone.

## Prereqs

- A real iPhone running iOS 16.4+ (web push requires it). Earlier iOS will silently no-op.
- Orbit served over **HTTPS**. iOS push only works on a secure origin. The local `http://localhost:3000` will NOT work — you need either:
  - the Saturn pilot URL, OR
  - a tunnelled HTTPS endpoint like `cloudflared tunnel run` against your local stack.
- Two Orbit accounts: one to receive on the iPhone, another to send from a desktop browser.
- The recipient must have **added Orbit to home screen** at least once. iOS push only fires for "installed" PWAs — never for in-Safari tabs (this is an Apple limitation, not ours).

## What we are testing end-to-end

| Stage | Component | Failure surface |
|---|---|---|
| 1 | Service worker registration on iOS | iOS sometimes evicts SW on memory pressure |
| 2 | Permission grant | iOS shows a one-shot prompt; no second chance |
| 3 | `pushManager.subscribe()` | Apple pushes through APNs → web-push library translates from VAPID |
| 4 | Subscription POST → `services/gateway/.../push/subscribe` | Gateway stores in `push_subscriptions` |
| 5 | Sender posts message | NATS publishes → push dispatcher → APNs |
| 6 | Apple delivers → SW shows notification | Banner / lock-screen render |
| 7 | Tap → app opens to right chat | SW `notificationclick` handler |

## Setup

1. On the iPhone, open Safari, navigate to the pilot URL.
2. Log in as `iphone-test@<your-tenant>`. (Pre-create this account; iOS prompt-flow doesn't tolerate the registration interstitial.)
3. Tap the share icon → "Add to Home Screen". Confirm.
4. Close Safari. **Open Orbit from the home-screen icon** (this is critical — push only works in standalone PWA mode on iOS).
5. iOS will prompt to "Allow notifications?" the first time you visit a chat that triggers the prompt. Accept.
6. Backgrounding the app: lock the iPhone or swipe the app away. Push only fires when the page is hidden.

## Verify steps 1-3 from desktop (sanity)

Before sending the test message, sanity-check that the iPhone got past `pushManager.subscribe`:

```sql
-- On the prod / staging postgres
SELECT user_id, endpoint, created_at, last_used_at
FROM push_subscriptions
WHERE endpoint LIKE '%.push.apple.com%'
ORDER BY created_at DESC
LIMIT 5;
```

A row with the iPhone account's `user_id` and an `apple.com` endpoint must exist. If not — the SW didn't register or the subscribe call failed. Re-open the app on the iPhone with Safari → Develop → iPhone tab → Console open and watch for errors.

## Send the test push

From the desktop browser logged in as a *different* account that shares a chat with the iPhone account:

1. Open the DM with the iPhone account.
2. Send a message: "iPhone push smoke 2026-MM-DD".
3. Within ~5 s the iPhone should buzz / show a banner. Lock-screen banner is also acceptable.

## Verify

| Check | Where to look |
|---|---|
| Banner appears within 5 s | iPhone screen |
| Banner shows sender name + message preview | iPhone screen |
| Tapping the banner opens Orbit at that DM | iPhone, after tap |
| Counter clears once read | iPhone, after open |
| Gateway log shows `push: dispatched apns endpoint=…` | `docker logs orbit-gateway-1 \| grep push` (or Saturn equivalent) |
| `push_subscriptions.last_used_at` updated | psql query above |

## When push doesn't fire

iOS web-push has a notoriously narrow path. Walk this list before assuming a backend bug:

1. **Was Orbit opened from the home screen, not Safari?** If it was Safari-only the subscribe will succeed but Apple never delivers.
2. **Is the iPhone in Low Power Mode or Focus mode?** Both can suppress banners.
3. **Are the VAPID keys on the gateway the same ones the SW subscribed with?** Mismatched keys silently fail. Check `VAPID_PUBLIC_KEY` env on the gateway matches what the SW sees: in Safari → Develop → iPhone → Console → run `(await navigator.serviceWorker.ready).pushManager.getSubscription().then(s => s.options.applicationServerKey)`.
4. **Did the SW get evicted?** iOS evicts SW that haven't been visited in a while. Re-open the PWA from home screen and the SW re-registers.
5. **Apple rate-limits silent pushes.** If we send too many "data-only" pushes (no `notification:` payload) within a window, Apple stops delivering. The dispatcher in `services/gateway/internal/push/` always includes a `notification:` block — confirm it does for the failing message.

## What to record in a bug report

If reproducible: iOS version, device model, Safari version, the exact gateway log line for the dispatched push, the postgres row for the subscription, and the `applicationServerKey` from step 3. Without those the bug is not actionable.
