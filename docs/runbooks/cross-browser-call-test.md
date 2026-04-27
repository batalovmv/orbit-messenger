# Cross-browser 1:1 call smoke test

> Manual procedure. Run before each pilot-targeted release.
> Estimated time: 10 min.

## Prereqs

- Two browser profiles on the SAME host, OR two devices on the same LAN.
- Two Orbit accounts that can DM each other. For local testing the seeded `test@orbit.local` / `user2@orbit.local` (password `LoadTest!2026`) work.
- `coturn` reachable. The local stack exposes it on `localhost:3478`. On Saturn it's behind a public hostname — verify `TURN_PUBLIC_URL` in the gateway env matches.
- Camera + mic permission granted (each browser will prompt the first time).

## What to verify

We test that the 7 surfaces touched by recent WebRTC and PWA work survive across browsers:

| # | What | Expected |
|---|---|---|
| 1 | Outgoing call — caller side | Ringing UI shows; remote side gets incoming-call ringtone within ~2s. |
| 2 | Pickup — callee side | Both sides show "Connected"; both videos visible (or at least audio). |
| 3 | ICE state | DevTools → console: no `iceConnectionState=failed`. |
| 4 | TURN relay fallback | Disable host candidates (offline LAN ICE) once between attempts and confirm a TURN-relayed connection still works. |
| 5 | Mute / camera toggle | UI state matches actual MediaStreamTrack `enabled` values. |
| 6 | Hangup | Both sides clear within 1s. WS message `call:end` appears in DevTools network panel. |
| 7 | Reconnect on tab refresh | Reload caller's tab mid-call → callee gets a clean `call:end` rather than a dangling state. |

## Browser matrix

| Browser | Channel | Notes |
|---|---|---|
| Chrome stable | Required | Largest install base; primary target for the WebRTC stack. |
| Firefox stable | Required | Different SDP munging quirks; surface bugs Chrome hides. |
| Safari (macOS or iOS) | Required for pilot | Most likely failure surface. iOS Safari needs a TLS gateway (R2 / Cloudflare) — local http://localhost works in Safari macOS only. |
| Edge | Optional | Same engine as Chrome; verify only if user count there matters. |
| Chromium-based mobile (Chrome Android) | Required | Different audio routing; confirms the `androidTapRecovery.ts` and the `pointercancel`-to-click logic from PWA hardening. |

## Steps

1. **Open the app in Browser A.** Login as `test@orbit.local` / `LoadTest!2026`.
2. **Open the app in Browser B.** Login as `user2@orbit.local` (or any second account).
3. From A, open the DM with B's user. Click the call icon.
4. On B, accept the call.
5. **Check #1, #2, #3 above.** Open DevTools → console; look for any red WebRTC errors.
6. From A, mute the mic → confirm the icon flips on both ends within 1s. Toggle camera off → same.
7. Hang up from A. Confirm B's UI clears.
8. Repeat the call. This time, on A's DevTools network tab, filter by `WS`. While ringing, throttle to "Slow 3G" for 5 s, then back to no throttling. The connection should ICE-restart rather than die — if it dies, log it.
9. Repeat with Browser pair (B, A) reversed (callee initiates).
10. Repeat once on a chromium-based mobile to exercise the new `androidTapRecovery` synthetic-click path.

Record any failure with: browser+OS+version, the specific step, console errors, and a HAR if you can capture one.

## Common failure modes and where to look

| Symptom | Likely cause | Where to look |
|---|---|---|
| Ringing on caller, callee never gets it | NATS `orbit.call.*.lifecycle` not delivered to gateway holding callee's WS | gateway logs for `nats: subscribed` lines on startup |
| Connected but no media | TURN unreachable; ICE only got host candidates that can't traverse | `chrome://webrtc-internals` → ICE candidates list. Confirm a `relay` candidate appeared. |
| Call connects but disconnects after ~30s | TURN credentials expired mid-call or NAT pinhole closed | `services/calls` logs for `TURN credential expired`; coturn logs for sessions ending |
| Mute icon out of sync | Message was published to NATS but lost between gateway instances | Same as #1 — gateway WS subscriber issue |
| iOS Safari: stays on "Connecting…" forever | iOS requires user-initiated audio context; check the playback button is part of the `click` handler, not async | `web/src/components/calls/*` |
| Android Chrome: tapping the call button does nothing on first attempt | `pointercancel` interrupted the tap before click fired (now mitigated by `androidTapRecovery.ts` shipped in 225e95a) | DevTools console → look for `[tap-recovery] synthetic click dispatched` log |

## When something fails

1. Capture a `chrome://webrtc-internals` dump and the WS frames from DevTools network panel.
2. File an issue with the matrix row that broke and the artefacts.
3. Do **not** retry without restarting both browsers — WebRTC state is sticky.
