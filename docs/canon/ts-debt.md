# Web TypeScript Debt — Phase 8D

20 `@ts-expect-error TODO(phase-8D-cleanup)` markers in 12 files.
Tracked here so they don't drift into "forgotten" status. Pre-commit hook
allows them only with the `TODO(...)` marker — see `.git/hooks/pre-commit`.

**Target:** zero `phase-8D-cleanup` markers before Phase 9.

## Categories

### i18n keys missing from `LangPair` (8)

Add to `src/assets/localization/fallback.strings` and run `npm run lang:ts`,
**or** rename to existing keys if the feature was renamed.

- `NotificationPriorityUpdateFailed` — `Chat.tsx:381`, `HeaderMenuContainer.tsx:352`
- `Languages` (signature mismatch) — `SettingsLanguage.tsx:174`
- `ShowTranslateButton` — `SettingsLanguage.tsx:227`
- `lng_translate_settings_about` — `SettingsLanguage.tsx:247`
- `ExactTextCopied` (signature mismatch) — `SettingsIntegrations.tsx:318`
- dynamic `Month${n}` keys — `BirthdaySetupModal.tsx:149,194`

### `ApiChat` shape (4)

Either add the fields to `src/api/types/chats.ts` (if features really exist)
or remove the call sites (if features were dropped).

- `botCommands` — `api/saturn/methods/chats.ts:685,691`
- `notificationPriorityOverride` — `Chat.tsx:617`, `HeaderActions.tsx:532`

### `IconName` union (3)

`'notifications'` icon not in the union. Add to `src/icons/` or rename.

- `HeaderMenuContainer.tsx:780`
- `useChatContextActions.ts:179, 232`

### Compliance `Blob` typing (2)

`Uint8Array<ArrayBufferLike>` is not assignable to `BlobPart[]` — TS 5.x
narrowing issue. Fix with `new Uint8Array(buffer.buffer)` or explicit cast.

- `CompliancePanel.tsx:85, 107`

### Misc (3)

- `tabId` not in `startBot` payload type — `actions/api/bots.ts:510`
- Mock spread type mismatch — `actions/api/settings.test.ts:62`
- Handler signature mismatch (undefined arg) — `useChatContextActions.ts:181`

## How to clear

1. Pick a category, fix all sites in one PR.
2. Run `npm run check:tsc` after each fix — must stay green.
3. Update this file (remove cleared category section).
4. When the file becomes empty, delete it and remove the link from `current-state.md`.

## Out of scope for Phase 8D

CSS/SCSS lint debt — 298 stylelint errors surfaced after `tsc` was fixed.
Run `npm run check:lint` to see them; mostly `order/properties-order` and
`selector-max-type`. Tracked separately, **non-blocking for deploy** (web
Dockerfile only gates on `check:tsc`).
