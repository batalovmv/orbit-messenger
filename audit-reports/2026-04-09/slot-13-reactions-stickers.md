# Slot 13: Reactions / Stickers

## Status
COMPLETED

## Scope
- `web/src/components/middle/message/reactions/`
- `web/src/components/middle/composer/StickerPicker*`
- `web/src/components/middle/composer/ReactionPicker*`
- `web/src/global/actions/api/reactions.ts`

## Focus Areas
- pointer-events rules (`CLAUDE.md`)
- sendReaction diff logic
- URL rewriting (`FillPreviewURLs` -> `/media/{uuid}`)
- sticker emoji fallback from `richContent`
- Lottie animation lifecycles
- memory leaks in reaction TGS cache
- z-index layering (`ReactionPicker` 10200)

## File Checklist
- [x] `web/src/components/middle/message/reactions/ReactionButton.module.scss`
- [x] `web/src/components/middle/message/reactions/ReactionButton.tsx`
- [x] `web/src/components/middle/message/reactions/ReactionPicker.async.tsx`
- [x] `web/src/components/middle/message/reactions/ReactionPicker.module.scss`
- [x] `web/src/components/middle/message/reactions/ReactionPicker.tsx`
- [x] `web/src/components/middle/message/reactions/ReactionPickerLimited.module.scss`
- [x] `web/src/components/middle/message/reactions/ReactionPickerLimited.tsx`
- [x] `web/src/components/middle/message/reactions/Reactions.scss`
- [x] `web/src/components/middle/message/reactions/Reactions.tsx`
- [x] `web/src/components/middle/message/reactions/ReactionSelector.scss`
- [x] `web/src/components/middle/message/reactions/ReactionSelector.tsx`
- [x] `web/src/components/middle/message/reactions/ReactionSelectorCustomReaction.tsx`
- [x] `web/src/components/middle/message/reactions/ReactionSelectorReaction.module.scss`
- [x] `web/src/components/middle/message/reactions/ReactionSelectorReaction.tsx`
- [x] `web/src/components/middle/message/reactions/SavedTagButton.tsx`
- [x] `web/src/components/middle/composer/StickerPicker.module.scss`
- [x] `web/src/components/middle/composer/StickerPicker.tsx`
- [x] `web/src/global/actions/api/reactions.ts`
- [x] `web/src/components/middle/composer/ReactionPicker*` — no matches in scope

## Findings
High / Critical: none confirmed in scoped files.

### MEDIUM: stale rollback in `toggleReaction` can clobber a newer successful reaction state
- Confidence: high
- File: `web/src/global/actions/api/reactions.ts:181-245`
- `toggleReaction` snapshots `message` and `userReactions`, applies an optimistic update (`226-227`), then awaits `sendReactionApi` (`229-236`).
- On any failure it blindly restores the old snapshot with `addMessageReaction(global, message, userReactions)` (`241-244`).
- Because the action is async and not serialized per message, two quick toggles can overlap. If request A fails after request B already succeeded, request A's catch rewrites the message back to the stale pre-A state and wipes the newer local/server-confirmed selection.
- The Phase 5 note about post-send refresh reduces ordinary desync, but it does not protect the `older failure finishes last` ordering here because the stale catch runs after the later success path.
- User impact: reaction chips can show the wrong chosen set/counts until another refresh or WS update arrives, and the next toggle is then computed from incorrect local state.
- Minimal repro sketch:
  1. Toggle reaction A on a flaky connection.
  2. Before request A settles, toggle reaction B.
  3. Let request B succeed and request A fail later.
  4. The catch block at `241-244` restores the stale snapshot from before A/B and overwrites the newer state.
- Fix direction: guard rollbacks with an in-flight mutation token per message, or re-read the current message and only revert if the local state still matches the exact optimistic write produced by this request, or refetch on failure instead of replaying the stale snapshot.

## Low Severity Bucket
- `web/src/components/middle/composer/StickerPicker.tsx:237-240` unconditionally calls `addRecentSticker({ sticker })` even when the picker is used as an effect picker (`isForEffects`). If effect assets are supposed to stay separate from normal sticker recents, this will mix the two histories.
- `web/src/components/middle/message/reactions/ReactionPicker.tsx:116-129` still hard-depends on `sticker.emoji` at click time and has no local fallback path. The intended richContent backfill may exist upstream, but this scoped code would silently no-op if that normalization regresses.

## Coverage Notes
- Read `CLAUDE.md`, `web/CLAUDE.md`, and the Phase 5 reactions/stickers section in `PHASES.md`.
- Pointer-events checks inside scope look aligned with the Phase 5 fix: `ReactionSelectorReaction.module.scss` sets `pointer-events: none` on `.AnimatedSticker` and `.staticIcon`, and `ReactionPicker.module.scss` hides the portal root with `display: none` when closed.
- `web/src/components/middle/composer/ReactionPicker*` had no matching files in scope.
- `/media/{uuid}` rewriting, richContent-to-emoji backfill implementation, and TGS cache internals are referenced by the scoped components but implemented outside the allowed paths, so I only reviewed consumer-side usage here.

## Pass Log
- [x] Pass 1: scope mapping
- [x] Pass 2: verification
