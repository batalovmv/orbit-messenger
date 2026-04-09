# Slot 14 Audit Report

## Metadata
- Slot: `14-messaging-state`
- Date: `2026-04-09`
- Commit: `82669bd35a1568f24eff710b1dd0074342f12dff`
- Scope: `web/src/global/reducers/messages.ts`, `web/src/global/actions/api/messages.ts`, `web/src/components/middle/MessageList*`, `web/src/components/middle/message/Message.tsx`
- Focus: `withGlobal` memoization, edit/delete flows, optimistic update rollback, scroll restoration, WS race conditions on fast message updates, pagination cursor handling, dedupe by message ID, scheduled messages UI
- Severity gate: individual only `HIGH` / `CRITICAL`; `MEDIUM` only with 90%+ confidence; `LOW` in bucket

## Status
- [x] Pass 1 complete
- [x] Pass 2 complete
- [x] Report finalized

## File Checklist
- [x] `web/src/global/reducers/messages.ts`
- [x] `web/src/global/actions/api/messages.ts`
- [x] `web/src/components/middle/MessageList.scss`
- [x] `web/src/components/middle/MessageList.tsx`
- [x] `web/src/components/middle/MessageListAccountInfo.module.scss`
- [x] `web/src/components/middle/MessageListAccountInfo.tsx`
- [x] `web/src/components/middle/MessageListBottomMarker.tsx`
- [x] `web/src/components/middle/MessageListContent.tsx`
- [x] `web/src/components/middle/MessageListHistoryHandler.tsx`
- [x] `web/src/components/middle/message/Message.tsx`

## Findings
No `HIGH` / `CRITICAL` issues confirmed in this scope.

### MEDIUM

1. Mixed delete path leaves local messages undeleted when selection contains both local and server messages
   - File: `web/src/global/actions/api/messages.ts:1026`
   - `messageIdsToDelete` filters local IDs out, and the local-delete helper runs only in the `!messageIdsToDelete.length` branch. In a mixed selection, only server-backed IDs are sent to `callApi('deleteMessages')`, while local unsent messages are ignored entirely.
   - Impact: selecting a pending/local bubble together with any persisted message produces a partial delete. The remote messages disappear after the server update, but the local ones stay in the list and keep their stale composer/upload state.

2. `editMessage` drops edit mode before the request succeeds and has no rollback on failure
   - File: `web/src/global/actions/api/messages.ts:654`
   - The handler clears `editingId` immediately, then fires `callApi('editMessage')` in a detached async closure. There is no success check before closing edit mode and no rollback path if the request is rejected or returns a failed result.
   - Impact: any network/validation/upload failure exits the user from edit mode and discards the in-progress edit context. For attachment edits, the upload-progress cleanup also sits only on the success path after `await`, so a thrown request can leave stale progress bookkeeping behind.

3. Older history fetches can overwrite newer message state, including recent edits/deletes
   - Files: `web/src/global/actions/api/messages.ts:1807`, `web/src/global/reducers/messages.ts:182`
   - `loadViewportMessages` merges async `fetchMessages` results back into state through `addChatMessagesById`. `mergeApiMessages` then blindly spreads `incomingMessage` over `existingMessage` with no freshness guard (`editDate`, revision, monotonic sequence, etc.).
   - Impact: if a WS update edits or deletes a message while an older history request is still in flight, the slower fetch can reapply stale content into `byId` and `listedIds`. In practice this can resurrect just-deleted messages or revert freshly edited text/reactions until another sync arrives.

## Low Bucket
- `web/src/components/middle/MessageList.tsx:605` treats any last-message ID change as `wasMessageAdded`, so delete/replace paths also go through the "new message" scroll/snap heuristic.
- `web/src/global/reducers/messages.ts:239` and `web/src/global/reducers/messages.ts:273` mutate incoming API message objects (`msg.content = {}`), which makes the reducer non-pure and increases aliasing risk across callers.
- `web/src/global/actions/api/messages.ts:1501` and `web/src/global/actions/api/messages.ts:1808` silently swallow scheduled/history fetch failures, leaving stale UI with no visible retry/error signal.

## Notes
- Explicit `withGlobal` memoization regressions were not confirmed in the reviewed mappers. The stronger issues in this slot are flow/race bugs in message state handling.
- `## Status: COMPLETED`
