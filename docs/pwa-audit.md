# PWA Production-Readiness Audit — Orbit Messenger

> Cyclic collegial audit. Reviewers: Claude Opus 4.7 (primary), GPT-5.4 (cross-check), Gemini 3.1 Pro Preview (contrarian).
> Target: HTTPS rollout via saturn.ac subdomain (150 users). Out of scope: native apps, Tauri, custom domain.
> Started: 2026-04-25.

## Status legend
- ✅ confirmed by ≥2 reviewers
- 🟡 partial / disputed (reviewers disagree)
- ❌ disputed (likely false positive)
- 🆕 new finding (cycle N)
- ✔ verified against actual code

---

## Cycle 1 — Baseline + Initial Cross-Check

### Confirmed Must-Have (blocker / major for prod)

| ID | Severity | Finding | Effort | Evidence |
|----|----------|---------|--------|----------|
| M2 | major | **Maskable icons missing** — square icons exist but `"purpose": "maskable"` not declared. Android adaptive icons crop badly. | 0.25h | [site.webmanifest](../web/public/site.webmanifest) |
| M4 | major | **screenshots[] only one entry** — Chrome 117+ install prompt wants 2-3 with `form_factor` (mobile + wide). | 1h | [site.webmanifest:24](../web/public/site.webmanifest:24) |
| F1 🆕 | major | **Manifest lacks `id` and explicit `scope`** — install identity ambiguous, can produce duplicate installs after path migrations. | 0.5h | [site.webmanifest](../web/public/site.webmanifest) |
| F3 🆕 | major | **`/share/` branch in SW lacks `return`** — fragile, future regex addition causes double-`respondWith`. Currently safe but unsafe pattern. | 1h | [serviceWorker/index.ts:68-70](../web/src/serviceWorker/index.ts:68) |
| F4/NF2 🆕 | major | **No iOS install fallback** — `beforeinstallprompt` doesn't fire on Safari. iOS users see no install affordance. | 3h | [installPrompt.ts](../web/src/util/installPrompt.ts) |
| F6 🆕 | major | **Viewport disables zoom** (`user-scalable=no`, `maximum-scale=1.0`) — accessibility regression. | 0.25h | [index.html:14-15](../web/src/index.html:14) |
| F9 🆕 | major | **share_target accepts `*/*`** — no MIME validation, risk of unsupported payloads. | 1.5h | [site.webmanifest:30-43](../web/public/site.webmanifest:30) |
| NF1 🆕 | major | **No `launch_handler`** — clicking notifications can spawn duplicate windows. Need `client_mode: "focus-existing"`. | 1h | [site.webmanifest](../web/public/site.webmanifest) |
| NF3 🆕 | major | **Aggressive `skipWaiting` + cache clear** — open tabs can hit ChunkLoadError on lazy-load after deploy. Need user-prompted update flow. | 4h | [serviceWorker/index.ts:26](../web/src/serviceWorker/index.ts:26) |
| F2 🆕 | major | **No offline navigation fallback** — first launch flaky/offline fails entirely. | 4h | [serviceWorker/index.ts:73](../web/src/serviceWorker/index.ts:73) |

### Polish (minor / nice-to-have)

| ID | Severity | Finding | Effort |
|----|----------|---------|--------|
| M1 | minor | theme_color `#ffffff` — bad in dark mode | 0.25h |
| M3 | minor | shortcuts[] missing — Android long-press menu (disputed by Gemini as not-must) | 0.5h |
| M6 | minor | Lighthouse PWA audit not run on prod | 1h |
| F5 🆕 | minor | No `appinstalled` event listener — install state can desync | 1h |
| F7 🆕 | minor | Canonical URL hardcoded `https://orbit.local/` — leaks dev hostname | 0.25h |
| F8 🆕 | minor | Call notification strings hardcoded Russian, manifest English — mixed locale | 2h |
| F10 🆕 | minor | `updateWebmanifest.ts` mac-only swap is mistargeted (mac ≠ iOS) | 1h |
| F11 🆕 | minor | Navigation Preload not enabled — slower cold starts under SW | 2h |
| F12 🆕 | minor | Manifest hardcoded English, no `lang`/`dir`, no localized variants | 2h |
| N1 | minor | apple-touch-startup-image (splash) missing | 2h |
| N2 | minor | Legacy mac manifest swap should be removed | 0.5h |

### Disputed / Out of Scope

| ID | Reason |
|----|--------|
| N3 (protocol_handlers) | GPT: out of scope for internal launch. Gemini: nice-to-have. **Verdict:** defer post-pilot. |
| N4 (file_handlers) | Both: nice-to-have, poor cross-platform support. **Verdict:** defer. |

### Reviewer disagreements
- **shortcuts[] (M3):** GPT confirms must-have, Gemini disputes (not required for installability). Resolved: minor (visible polish, not blocker).
- **Lighthouse (M6):** GPT confirms gap, Gemini disputes (process not code). Resolved: process item, run after fixes.

### Cycle 1 verdict (both reviewers): `needs-fixes`

---

## Cycle 2 — re-verify + new probes (manifest 2025 fields, badging, security headers, share.ts deep-dive)

**GPT verdict:** diminishing-returns. **Gemini verdict:** still-finding-major.

### Reviewer-driven adjustments to cycle 1
- **M4 (screenshots) overstated** — one screenshot exists, just not multiple form_factors → still major but not "missing"
- **NF3 (skipWaiting) overstated** — assetCache.ts:51-58 actually detects 404 stale chunks and emits `staleChunkDetected` to clients → downgrade to **minor**
- **F12 (manifest English) overstated** — not a PWA defect unless localized install is required → drop or polish

### New cycle 2 findings

| ID | Severity | Finding | Effort | Evidence |
|----|----------|---------|--------|----------|
| NF-C2-1 ✔ | major | **`respondForShare` redirects to `.` from `/share/`** → resolves to `/share/` again, ugly URL bar. Fragile on Safari (resultingClientId may not match). | 1h | [share.ts:26](../web/src/serviceWorker/share.ts:26) |
| NF-C2-2 ✔ | major | **Missing `e.preventDefault()` in `beforeinstallprompt`** — Chrome shows default mini-infobar, defeating custom UX. | 0.25h | [installPrompt.ts:6](../web/src/util/installPrompt.ts:6) |
| NF-C2-3 ✔ | minor | **`respondWithCache` does unguarded `fetch(e.request)`** — offline + cache miss → uncaught reject → browser default error page. | 1h | [assetCache.ts:43](../web/src/serviceWorker/assetCache.ts:43) |
| NF-C2-4 ✔ | minor | **Notification `badge` is full-color icon** — Android shows white square in status bar (badge needs monochrome alpha). | 1h | [pushNotification.ts:231](../web/src/serviceWorker/pushNotification.ts:231) |
| C2-2 | major | **No `navigator.storage.persist()`** — IndexedDB chats can be evicted under storage pressure. Critical for offline-first messenger. | 2h | no usage in any provided file |
| C2-3 | major | **No Badging API** — `navigator.setAppBadge()` not called → installed-app icon never shows unread count. Standard messenger UX. | 3h | no usage anywhere |
| C2-4 | minor | **`READY_CLIENT_DEFERREDS` Map without cleanup** — entries never deleted, leaks per client lifecycle. | 1h | [share.ts:12](../web/src/serviceWorker/share.ts:12) |
| C2-5 | minor | **Permissions-Policy header not configured** — for camera/microphone/display-capture (calls). Server-side. | 2h | no header in HTML or known nginx config |
| C2-7 | minor | **SW silently swallows cache errors** — `withTimeout` catches all, `clearAssetCache().catch(() => {})` — no telemetry on failures. | 2h | [index.ts:38](../web/src/serviceWorker/index.ts:38), [assetCache.ts:64](../web/src/serviceWorker/assetCache.ts:64) |
| C2-8 | polish | **Manifest omits `categories`** — `["communication", "business", "productivity"]`. | 0.25h | [site.webmanifest](../web/public/site.webmanifest) |

### Cycle 2 disputed/dropped
- C2-1 (144x144 icon) — overengineering, modern browsers fine with 192+
- C2-6 (COOP/COEP) — only needed for SharedArrayBuffer, we don't use

---

## Cycle 3 — convergence check

**GPT verdict:** diminishing-returns. **Gemini verdict:** diminishing-returns.

Both reviewers independently surfaced **the same single new finding** — strong convergence signal.

| ID | Severity | Finding | Effort | Evidence |
|----|----------|---------|--------|----------|
| **C3-1** ⭐ | **major** | **No `pushsubscriptionchange` handler in SW** — when browser rotates/expires PushSubscription, push silently dies until manual re-subscribe. Both GPT and Gemini independently flagged this. | 3-4h | no listener in [index.ts](../web/src/serviceWorker/index.ts), [pushNotification.ts](../web/src/serviceWorker/pushNotification.ts) |

Other cycle 3 candidates were duplicates (maskable, share map cleanup) or unverifiable without server-side files.

### Stop condition triggered
- Cycle 3 surfaced 1 new finding, both reviewers signal diminishing-returns.
- Cycle 4 would degrade — terminating audit.

---

## Final Consolidated List

### Must-have before public PWA (~22h ≈ 3 days)

| ID | Title | Effort |
|----|-------|--------|
| M2 | Maskable icons (`purpose: "any maskable"`) | 0.25h |
| M4 | screenshots[] with `form_factor` (mobile + wide) | 1h |
| F1 | Manifest `id` and explicit `scope` | 0.5h |
| F2 | Offline navigation fallback (app shell) | 4h |
| F3 | Add `return` after `respondForShare` in SW | 0.5h |
| F4 | iOS manual-install instructions UI | 3h |
| F6 | Remove `user-scalable=no` from viewport (a11y) | 0.25h |
| F9 | Narrow `share_target` accept from `*/*` to MIME list | 1.5h |
| NF1 | `launch_handler: { client_mode: "focus-existing" }` | 1h |
| NF-C2-1 | Fix share redirect `.` → `/`, store data globally | 1h |
| NF-C2-2 | `e.preventDefault()` in beforeinstallprompt | 0.25h |
| C2-2 | `navigator.storage.persist()` after login | 2h |
| C2-3 | Badging API integration (`setAppBadge` on unread) | 3h |
| **C3-1** | **`pushsubscriptionchange` handler + re-subscribe flow** | 3-4h |

### Polish (~11h)

| ID | Title | Effort |
|----|-------|--------|
| M1 | theme_color → Orbit brand | 0.25h |
| M3 | shortcuts[] (Новый чат, Звонки, Настройки) | 0.5h |
| M6 | Lighthouse PWA audit on prod | 1h |
| F5 | `appinstalled` event listener | 0.5h |
| F7 | Parameterize canonical URL (drop orbit.local) | 0.25h |
| F8 | Localize call notification strings | 2h |
| F11 | Enable Navigation Preload in SW activate | 2h |
| N1 | apple-touch-startup-image (iOS splash) | 2h |
| N2/F10 | Remove legacy mac manifest swap | 0.5h |
| NF3 | User-prompted SW update flow | 4h |
| NF-C2-3 | try/catch around fetch in `respondWithCache` | 0.5h |
| NF-C2-4 | Monochrome `badge` PNG | 1h |
| C2-4 | Cleanup `READY_CLIENT_DEFERREDS` after share | 1h |
| C2-5 | `Permissions-Policy` header on saturn.ac | 1h (ops) |
| C2-7 | Telemetry on SW cache failures | 2h |
| C2-8 | Manifest `categories` field | 0.1h |

### Deferred (post-pilot)
- N3 protocol_handlers (`orbit://chat/{id}`)
- N4 file_handlers
- COOP/COEP (only if SharedArrayBuffer is added)
- Window Controls Overlay
- Localized manifests (only if RU-locale install becomes priority)

---

## Recommended execution order

**Phase A (1 day, easy wins) — these unblock everything else:**
1. Manifest fixes: M2 + M4 + F1 + M1 + M3 + NF1 + C2-8 = ~3.5h, all in [site.webmanifest](../web/public/site.webmanifest)
2. installPrompt fixes: NF-C2-2 + F5 = 0.75h, [installPrompt.ts](../web/src/util/installPrompt.ts)
3. Viewport a11y fix: F6 = 0.25h, [index.html:14](../web/src/index.html:14)
4. Canonical URL: F7 = 0.25h, [index.html:44](../web/src/index.html:44)

**Phase B (1 day, push reliability):**
5. C3-1 pushsubscriptionchange + re-subscribe = 4h
6. C2-3 Badging API + integrate with unread state = 3h
7. NF-C2-4 monochrome badge = 1h

**Phase C (1 day, share + offline robustness):**
8. F3 + NF-C2-1 + F9 + C2-4 = ~4h, [share.ts](../web/src/serviceWorker/share.ts) + manifest
9. C2-2 storage.persist after login = 2h
10. F2 offline shell fallback + NF-C2-3 cache catch = 4h

**Phase D (0.5 day, polish):**
11. F8 i18n call notifications = 2h
12. N1 apple splash images = 2h
13. M6 Lighthouse pass + iterate = 1-2h
14. F4 iOS install banner = 3h

**Total: ~4-5 days** for production-ready PWA on saturn.ac.

---

## Stop criteria summary

| Cycle | New major | New minor | Convergence |
|-------|-----------|-----------|-------------|
| 1 | 9 | 11 | initial |
| 2 | 4 | 6 | mixed (Gemini found a real share blocker) |
| 3 | 1 (independently confirmed by both reviewers) | 0 (duplicates only) | **converged** |

**Audit terminated** — diminishing returns + independent agreement on remaining gap. Cycle 4 would degrade.

Total cost: ~$0.008 across 6 LLM calls (gpt-5.4 ×3, gemini-3.1-pro ×3) on stepanovikov.uno flat-rate proxy.

---

## Implementation Status (2026-04-25)

Implemented across 6 commits on master:

### Phase A — Manifest + install + viewport (commits 57f0285, e57ef79, 25348ba)
- ✅ M1 theme_color → #212121, media-aware in HTML head
- ✅ M2 maskable icons via square variants in site.webmanifest
- ✅ M3 shortcuts[] (Новый чат, Звонки, Настройки)
- ✅ M4 screenshots form_factor=wide
- ✅ F1 manifest id and explicit scope
- ✅ F5 appinstalled event listener + standalone detection
- ✅ F6 viewport user-scalable=no removed (a11y fix)
- ✅ F7 hardcoded canonical orbit.local removed
- ✅ F9 share_target accept narrowed to MIME list
- ✅ NF1 launch_handler client_mode=focus-existing
- ✅ NF-C2-2 e.preventDefault() in beforeinstallprompt
- ✅ C2-8 manifest categories field

### Phase B — Push subscription rotation (commit 6f022db)
- ✅ C3-1 pushsubscriptionchange handler in SW + main thread re-subscribe
- ⚪ C2-3 Badging API — was already implemented (false-positive in audit, see Main.tsx:536 + util/appBadge.ts)

### Phase C — Share/offline/persist (commit e811a82)
- ✅ F2 offline navigation fallback to cached app shell
- ✅ F3 return after respondForShare (defensive)
- ✅ NF-C2-1 share redirect '.' → '/' (no more ugly /share/ URL)
- ✅ NF-C2-3 try/catch around fetch in respondWithCache
- ✅ C2-2 navigator.storage.persist() requested on app boot
- ✅ C2-4 cleanup of READY_CLIENT_DEFERREDS map after share

### Phase D — Polish + legacy cleanup (commit e39486e)
- ✅ F11 Navigation Preload enabled in SW activate, consumed in respondWithCacheNetworkFirst
- ✅ N2/F10 dropped util/updateWebmanifest.ts and apple-specific manifest variants

### Deferred (need design assets, manual testing, or larger scope)
- ⏸ F4 iOS install instructions UI — needs ru-localized banner + UX (3h)
- ⏸ F8 call notification i18n — by-design Russian for MST corp use, revisit if multi-language
- ⏸ M6 Lighthouse PWA audit — post-deploy on saturn.ac, manual Chrome DevTools
- ⏸ N1 apple-touch-startup-image — needs PNG splash assets per device
- ⏸ NF-C2-4 monochrome notification badge PNG — needs design asset
- ⏸ NF3 user-prompted SW update flow — partially mitigated by staleChunkDetected (assetCache.ts:51); explicit reload UI is a follow-up
- ⏸ C2-5 Permissions-Policy header — server-side (saturn.ac nginx)
- ⏸ C2-7 SW telemetry on cache failures — needs external logging endpoint

### Verification
- TypeScript baseline holds at 32 errors (none introduced by Phase A-D)
- All manifest JSON files valid
- Browser preview verification pending docker-compose up (orbit-web container)



