# Slot 15 Audit Report

## Scope
- `web/src/components/mediaViewer/`
- `web/src/components/middle/composer/helpers/buildAttachment.ts`
- `web/src/components/middle/composer/helpers/getFilesFromDataTransferItems.ts`
- `web/src/global/actions/api/media.ts` (missing at commit `82669bd35a1568f24eff710b1dd0074342f12dff`)
- `web/src/util/mediaLoader.ts`
- `web/src/util/mediaSession.ts`

## Focus Areas
- upload progress tracking
- chunked upload error recovery
- MediaViewer memory and blob URL revocation
- thumbnail loading fallbacks
- presigned URL refresh on expiry
- video streaming and range-request assumptions
- image EXIF privacy on client
- file size limits UX

## Files Checklist
- [x] `web/src/components/mediaViewer/MediaViewer.async.tsx`
- [x] `web/src/components/mediaViewer/MediaViewer.tsx`
- [x] `web/src/components/mediaViewer/MediaViewerActions.tsx`
- [x] `web/src/components/mediaViewer/MediaViewerContent.tsx`
- [x] `web/src/components/mediaViewer/MediaViewerFooter.tsx`
- [x] `web/src/components/mediaViewer/MediaViewerSlides.tsx`
- [x] `web/src/components/mediaViewer/SenderInfo.tsx`
- [x] `web/src/components/mediaViewer/VideoPlayer.tsx`
- [x] `web/src/components/mediaViewer/helpers/getViewableMedia.ts`
- [x] `web/src/components/mediaViewer/helpers/ghostAnimation.ts`
- [x] `web/src/components/mediaViewer/hooks/useMediaProps.ts`
- [x] `web/src/components/middle/composer/helpers/buildAttachment.ts`
- [x] `web/src/components/middle/composer/helpers/getFilesFromDataTransferItems.ts`
- [x] `web/src/global/actions/api/media.ts` (missing)
- [x] `web/src/util/mediaLoader.ts`
- [x] `web/src/util/mediaSession.ts`

## Pass 1
- [x] Map upload, viewer, thumbnail, and media URL flows
- [x] Identify candidate issues in scope

## Pass 2
- [x] Re-read only candidate areas
- [x] Verify each reported issue against real call paths
- [x] Drop anything below severity gate or confidence bar

## Findings
No HIGH or CRITICAL findings confirmed in the scoped frontend media paths.

### MEDIUM: Directory drag-and-drop truncates uploads after the first `readEntries()` batch
- Evidence: `web/src/components/middle/composer/helpers/getFilesFromDataTransferItems.ts:24-32` creates a `FileSystemDirectoryReader`, calls `readEntries()` exactly once, and resolves immediately with that first batch.
- Why this is real: `FileSystemDirectoryReader.readEntries()` is chunked; callers must keep reading until it returns an empty array. In the current helper, nested folder drops stop after the first batch instead of exhausting the directory.
- Impact: dragging folders with many files silently loses the tail of the selection before validation/upload. For media-heavy chats this means partial sends with no user-visible indication that files were skipped.
- Pass 2 verification: there is no second read loop or follow-up drain call anywhere in this helper; all recursion hangs off that single callback result.

### MEDIUM: Blob URLs are leaked on normal media unload/cancel paths
- Evidence: `web/src/util/mediaLoader.ts:145-147`, `web/src/util/mediaLoader.ts:180`, and `web/src/util/mediaLoader.ts:207-210` convert fetched blobs into object URLs and keep them in `memoryCache`. Cleanup paths at `web/src/util/mediaLoader.ts:185-191` and `web/src/util/mediaLoader.ts:193-200` only delete cache entries; they never call `URL.revokeObjectURL()`.
- Why this is real: the only object-URL revocation in this file is the OGG conversion temporary at `web/src/util/mediaLoader.ts:173-175`. Normal photo/video blob URLs survive both `unload()` and canceled-progress cleanup.
- Impact: each full-size photo/video viewed or downloaded can pin its backing blob until tab reload. Repeated browsing through large shared media steadily grows renderer memory and can freeze or crash the tab on long sessions.
- Pass 2 verification: no scoped `MediaViewer*` file adds a compensating revoke path; the leak originates in `mediaLoader` itself.

## Low Bucket
- No explicit `onError` fallback exists in scoped `MediaViewerContent` / `VideoPlayer` image-video render paths, so broken preview/full-media loads degrade into a spinner or blank preview rather than a thumbnail fallback or retry UI.
- `web/src/global/actions/api/media.ts` does not exist at frozen commit `82669bd35a1568f24eff710b1dd0074342f12dff`, so the requested chunked-upload recovery / presigned-refresh review could not be completed from the user-specified action path.
- `web/src/components/middle/composer/helpers/buildAttachment.ts` creates multiple object URLs (`blobUrl`, `previewBlobUrl`, `compressedBlobUrl`) but no cleanup owner is visible in this scoped slice; attachment-discard/send cleanup should be checked in the actual upload action path.

## Notes
- Commit audited: `82669bd35a1568f24eff710b1dd0074342f12dff`
- Severity gate: HIGH / CRITICAL individually; MEDIUM only if confidence >= 0.9
- Scope note: user-provided `web/src/components/common/MediaViewer*` maps to existing `web/src/components/mediaViewer/` on this commit

## Status
- COMPLETED
