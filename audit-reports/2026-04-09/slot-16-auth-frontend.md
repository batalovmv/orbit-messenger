# Slot 16 Audit Report

## Scope
- `web/src/components/auth/`
- `web/src/global/actions/api/auth.ts`
- `web/src/global/reducers/auth.ts`

## Focus Areas
- login/register flows
- 2FA TOTP UI
- invite landing page
- session persistence
- logout cleanup
- CSP-friendly code
- XSS surface in invite codes
- redirect loops

## Files Checklist
- [x] `web/src/components/auth/`
- [ ] `web/src/global/actions/api/auth.ts` — file is absent in this commit; inspected adjacent Saturn auth wiring in `web/src/global/actions/api/saturnAuth.ts`
- [x] `web/src/global/actions/api/saturnAuth.ts`
- [x] `web/src/global/reducers/auth.ts`

## Findings
### MEDIUM: Registration form deadlocks after the first failed submit
- Files: `web/src/components/auth/AuthSaturnRegister.tsx:27`, `web/src/components/auth/AuthSaturnRegister.tsx:29`, `web/src/components/auth/AuthSaturnRegister.tsx:61`, `web/src/global/actions/api/saturnAuth.ts:58`
- Confidence: high
- Why it matters: `AuthSaturnRegister` keeps its own local `isLoading` flag and sets it on submit, but never clears it on any failure path. `canSubmit` is gated by `!isLoading`, so a single rejected invite, validation error, or transient backend failure leaves the form permanently disabled until full page reload. This breaks invite-only onboarding and traps the user in a non-recoverable auth state.
- Pass 2 verification: confirmed that the component only calls `markIsLoading()` locally and has no `unmarkIsLoading()` or effect tied to `auth.errorKey`/request completion, while `saturnRegister` also does not reset component-local state on failure.

### MEDIUM: `authorizationStateWaitQrCode` never renders the QR login screen
- Files: `web/src/components/auth/Auth.tsx:55`
- Confidence: high
- Why it matters: the auth shell maps both `authorizationStateWaitPhoneNumber` and `authorizationStateWaitQrCode` to `AuthEmailLogin`. As a result, the dedicated QR branch is never rendered from this entrypoint even though the auth state machine still distinguishes it. On desktop this makes the QR/passkey branch effectively unreachable from the visible UI and creates hidden state transitions that the user cannot understand or recover from cleanly.
- Pass 2 verification: re-checked the switch in `getScreen()` and there is no branch that returns `AuthQrCode`; the `authorizationStateWaitQrCode` case explicitly returns `AuthEmailLogin`.

## Low Bucket
- No confirmed HIGH / CRITICAL issues in this scope after pass 2.
- Requested file `web/src/global/actions/api/auth.ts` does not exist at commit `82669bd`; current Saturn auth wiring lives in `web/src/global/actions/api/saturnAuth.ts`.
- Invite pre-validation looks unfinished in scope: `saturnValidateInvite` exists, but no component under `web/src/components/auth/` calls it, so there is no invite landing/validation step before the full registration form.
- No logout cleanup logic for IndexedDB / Cache Storage is present in the audited scope, so that focus area could not be validated here.
- No in-scope evidence of refresh-token persistence strategy (`httpOnly` cookie vs `localStorage`/`sessionStorage`); storage handling appears to live outside the requested files.

## Notes
- Commit audited: `82669bd35a1568f24eff710b1dd0074342f12dff`
- Severity gate: HIGH / CRITICAL individually; MEDIUM only if confidence >= 0.9

## Status
- COMPLETED
