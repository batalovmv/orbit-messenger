# Orbit Messenger Sprint P1/P2 — 2026-04-25
Swarm: default
Phase: 2 [PENDING] | Updated: 2026-04-26T00:00:00.000Z

---
## Phase 1: Security Review + Fixes (T6) [DONE]
- [x] 1.1: Security review services/auth — JWT, TOTP, rate limiting, sessions [MEDIUM]
- [x] 1.2: Security review services/gateway — WS auth, CORS, signaling IDOR, proxy headers [MEDIUM]
- [x] 1.3: Fix P1: SFU proxy callID UUID validation and read size limits [SMALL] — 7cfbe5f
- [x] 1.4: Fix P1: TOTP code replay prevention via Redis used-code tracking [SMALL] — 26efdc6
- [x] 1.5: Fix P1: AdminRevokeSession blacklist TTL use AccessTTL [SMALL] — 7577db7
- [x] 1.6: Fix P1: Service-level rate limiting on auth login/register/reset-admin [SMALL] — 1828d91 + a00e2de (regression test)
- [x] 1.7: Fix P2: WebRTC signaling IDOR — call membership check [SMALL] — 3e698cb
- [x] 1.8: Fix P2: Token revalidation — connection-scoped context [SMALL] — 38c5a99
- [x] 1.9: Fix P2: Rate limit identifier precedence [SMALL] — 2440feb
- [x] 1.10: Fix P2: Refresh cookie SameSite Strict [SMALL] — 96349eb
- [x] 1.11: Fix P2: Invalidate existing sessions on 2FA enable [SMALL] — 205627f
- [x] 1.12: docs/security-review-2026-04-26.md (13 findings: 5P1+5P2+3P3, 10 fixed, 3 deferred) [SMALL]

---
## Phase 2: Telegram Cleanup (T7) [PENDING]
- [ ] 2.1: Remove dead Stories code: EmbeddedStory, useEnsureStory, ApiStory, story assets [SMALL]
- [ ] 2.2: Remove Invoice and Giveaway message components, patch Message.tsx fallback [SMALL]
- [ ] 2.3: Remove payment infrastructure: web/src/components/payment/, paidMessage/, related files [MEDIUM]
- [ ] 2.4: Remove payment and gifts API types: gramjs methods, apiBuilders, api/types [SMALL]
- [ ] 2.5: Build + lint verification, measure bundle size diff, finalize cleanup commit [SMALL]

---
## Phase 3: Bot Inline Keyboards + HR Template (T8) [PENDING]
- [ ] 3.1: Backend: validate and persist reply_markup in bots sendMessage handler [SMALL]
- [ ] 3.2: Frontend: MessageInlineKeyboard component with url/callback_data button types [MEDIUM]
- [ ] 3.3: Frontend: integrate MessageInlineKeyboard into Message.tsx [SMALL]
- [ ] 3.4: Backend: HR bot template — migration 054, hr_bot.go, store, types, handler tests [LARGE]
- [ ] 3.5: Docs: bots-hr.md installation and usage guide [SMALL]
