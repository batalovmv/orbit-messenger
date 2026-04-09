# Audit Report — slot-04-media

**Service:** `services/media/` (media service, port 8083)
**Auditor:** Claude Sonnet 4.6
**Date:** 2026-04-09
**Pass:** Two-pass (discover then grep cross-reference)

---

## Scope

`services/media/` — R2/S3 upload/download pipeline, presigned URL generation, chunked upload, file type validation (MIME sniffing vs declared), EXIF stripping for photos, thumbnail generation (320/800/original, video frame extraction), SSRF, path traversal in filenames/R2 keys, size limits, storage quota enforcement, sentinel errors + mapError pattern, chunked upload TTL in Redis, video streaming via presigned URLs (range requests), content-disposition sanitization.

---

## Files Reviewed

- [x] `services/media/cmd/main.go`
- [x] `services/media/internal/handler/upload_handler.go`
- [x] `services/media/internal/handler/media_handler.go`
- [x] `services/media/internal/handler/media_handler_test.go`
- [x] `services/media/internal/service/media_service.go`
- [x] `services/media/internal/service/processor.go`
- [x] `services/media/internal/storage/r2.go`
- [x] `services/media/internal/store/media_store.go`
- [x] `services/media/internal/model/models.go`
- [x] `services/media/Dockerfile`

---

## Progress Log

1. Read CLAUDE.md - conventions confirmed (sentinel errors + mapError for media, getUserID pattern, X-Internal-Token trust model, R2 key structure).
2. Globbed all Go files under `services/media/` - 9 source files + Dockerfile.
3. Read all source files in full.
4. Cross-reference grep passes: MIME validation, presigned URL access control, quota enforcement, Content-Disposition, ffmpeg invocation, chunked upload Redis key patterns, rate limiting, HEIC handling, EnsureBucket policy, Lua script error matching.

---

## Findings

---

### FINDING-01 - HIGH - No Storage Quota Enforcement Per User

**File:** `services/media/internal/handler/upload_handler.go:57-99`, `services/media/internal/service/media_service.go:44-101`

**Category:** Missing Authorization / Resource Exhaustion

**Description:**
There is no per-user storage quota check at any point in the upload pipeline — neither simple uploads nor chunked uploads. A user can upload an unlimited number of files (each up to 2 GB for video/file types) and fill the R2 bucket without restriction.

**Evidence:**
- `Upload()` at `upload_handler.go:57` checks only `file.Size > model.SimpleUploadLimit` (50 MB ceiling for simple path) then calls `svc.Upload()`.
- `svc.Upload()` at `media_service.go:44` checks per-file type size limits via `model.SizeLimit()` but performs zero query against existing user storage usage.
- `InitChunkedUpload()` at `media_service.go:481` similarly validates total_size against per-type limit but never queries cumulative user usage.
- No `SUM(size_bytes) WHERE uploader_id = $1` query exists anywhere in `media_store.go`.
- The word "quota" does not appear in any Go file under `services/media/`.

**Verified:** Confirmed by full read of `media_service.go`, `media_store.go`, and `upload_handler.go`.

**Impact:** Any authenticated user can exhaust R2 storage capacity, causing denial of service for all other users and potentially unbounded cloud storage costs.

**Fix:** Add a `GetUserStorageUsed(ctx, userID) int64` store method summing `size_bytes`. Enforce a configurable per-user quota (e.g., `MAX_USER_STORAGE_BYTES` env var, default 5 GB) at the start of `svc.Upload()` and `svc.InitChunkedUpload()`. Reject with `apperror.BadRequest("Storage quota exceeded")`.

---

### FINDING-02 - HIGH - Presigned URL Functions Lack Access Control (IDOR)

**File:** `services/media/internal/service/media_service.go:384-424`

**Category:** Broken Access Control / IDOR

**Description:**
Three exported functions `GetPresignedURL`, `GetThumbnailURL`, and `GetMediumURL` generate presigned download URLs for arbitrary media IDs without calling `store.CanAccess()`. By contrast, `GetR2Key()` and `GetInfo()` both call `store.CanAccess()` before serving content. If any code path calls these presign functions with a caller-supplied media ID, any authenticated user can obtain a presigned URL for any other user's media.

**Evidence:**
- `media_service.go:384-394`: `GetPresignedURL` accepts only `id uuid.UUID` with no `userID` parameter and no `CanAccess` call.
- `GetThumbnailURL` (line 397) and `GetMediumURL` (line 412) have the same structural flaw.
- Compare with `GetR2Key()` at lines 352-358 which correctly calls `store.CanAccess(ctx, id, userID)` and returns `model.ErrAccessDenied` on failure.
- The presign functions are not currently wired to any HTTP handler (confirmed by grep), but they are exported and available to any importing package.

**Verified:** Confirmed by reading all three functions and all HTTP handler registrations.

**Impact:** Latent HIGH. If the messaging service or any future code calls these functions with user-supplied media IDs (e.g., for preview URL generation), any user can enumerate and download any other user's private files.

**Fix:** Add `userID uuid.UUID` parameter to all three functions, call `store.CanAccess(ctx, id, userID)` before generating the URL, return `model.ErrAccessDenied` on failure. This mirrors the existing pattern in `GetR2Key`.

---

### FINDING-03 - HIGH - Chunked Upload MIME Re-Validation Bypassed for `application/octet-stream`

**File:** `services/media/internal/service/media_service.go:692-710`

**Category:** MIME Type Bypass

**Description:**
For chunked uploads, `InitChunkedUpload` accepts the client-declared `mime_type` and validates it against `AllowedMIME`. At `CompleteChunkedUpload`, the assembled R2 object is re-fetched and content-sniffed. However, if `http.DetectContentType` returns `application/octet-stream` (its fallback for unrecognized formats), the re-validation is skipped entirely via:

    if detectedMIME != "application/octet-stream" && !model.AllowedMIME(meta.MediaType, detectedMIME) {

Any binary content whose magic bytes are not recognized by Go's `http.DetectContentType` (which only knows ~20 formats) will pass re-validation regardless of actual content, stored with the client-declared MIME type.

For `MediaTypeFile`, `AllowedMIME` always returns `true` (models.go:149), so the bypass applies unconditionally to all file-type uploads.

**Evidence:**
- `media_service.go:700-703`: the `application/octet-stream` exception.
- `model/models.go:149`: `case MediaTypeFile: return true`.

**Verified:** Confirmed by reading the complete `CompleteChunkedUpload` function.

**Impact:** Medium-High. For photo/video, `Content-Disposition: attachment` and `X-Content-Type-Options: nosniff` mitigate browser rendering. For file-type uploads the bypass is unconditional. Stored MIME in DB will mismatch actual content.

**Fix:** Remove the `application/octet-stream` exception. For unrecognized content, treat as validation failure for typed media (photo/video/voice). Alternatively integrate a library with broader format support (e.g., `github.com/h2non/filetype`) before falling back.

---

### FINDING-04 - MEDIUM - `EnsureBucket` Applies Public-Read Policy Unconditionally on Startup

**File:** `services/media/internal/storage/r2.go:232-251`, `services/media/cmd/main.go:109-111`

**Category:** Misconfiguration / Data Exposure

**Description:**
`EnsureBucket()` is called unconditionally on every service startup. When it creates the bucket, it immediately applies an `s3:GetObject` policy with `Principal: *` (world-readable). The comment says "for dev (MinIO)" but the code runs in production too. If the bucket does not already exist in production, the service creates it with a public-read policy. The `PutBucketPolicy` error is silently discarded (`_, _`).

**Evidence:**
- `r2.go:244`: `Principal: {"AWS": ["*"]}` policy applied unconditionally on bucket creation.
- `r2.go:245`: `_, _ = r.client.PutBucketPolicy(...)` - error silently discarded.
- `main.go:109`: `r2Client.EnsureBucket(ctx)` called unconditionally on startup.

**Verified:** Confirmed by reading both files.

**Impact:** On Cloudflare R2, `PutBucketPolicy` is likely a no-op (R2 does not fully support S3 bucket policies). On MinIO in staging, this makes all stored media world-readable. The silently-swallowed error hides any failure.

**Fix:** Guard the `PutBucketPolicy` call behind a `DEV_MODE=true` env flag. Log the error from `PutBucketPolicy` instead of discarding it.

---

### FINDING-05 - MEDIUM - Lua Script Error Matching via `strings.Contains` Is Fragile

**File:** `services/media/internal/service/media_service.go:638-675`

**Category:** Correctness / Error Handling

**Description:**
The abort and complete chunked upload flows parse Lua error replies by calling `strings.Contains(errMsg, "not_found")` and `strings.Contains(errMsg, "forbidden")`. If a Redis internal error message contains "not_found" as a substring (e.g., a NOSCRIPT error: "script not found in cache"), the wrong sentinel error is returned to the caller, mapping a Redis failure to a 404 instead of 500.

**Evidence:**
- `media_service.go:638-644`: abort path uses substring match.
- `media_service.go:669-675`: complete path uses substring match.

**Verified:** Both occurrences confirmed.

**Impact:** Medium. A Redis NOSCRIPT error (script evicted from Lua cache) would return "Upload not found" to the client instead of "Internal server error," masking the real failure.

**Fix:** Use exact string match or encode error type as a numeric return value from Lua (e.g., `return 1` for not_found, `return 2` for forbidden) and switch on the integer result.

---

### FINDING-06 - MEDIUM - `image/heic` in Allowlist But Undetectable by `http.DetectContentType`

**File:** `services/media/internal/model/models.go:139`, `services/media/internal/handler/upload_handler.go:88`

**Category:** MIME Validation Dead Code

**Description:**
`AllowedMIME("photo", "image/heic")` returns `true`. For simple uploads the handler always uses `http.DetectContentType(data)` to determine the MIME type. Go's standard library does not recognize HEIC magic bytes, so any HEIC file sniffs as `application/octet-stream`. `AllowedMIME("photo", "application/octet-stream")` returns `false`, so HEIC uploads are always rejected with "MIME type not allowed" despite being listed as supported.

**Verified:** `http.DetectContentType` implements the WHATWG MIME sniff spec; HEIC is not in that spec.

**Impact:** Medium. HEIC support is silently broken - advertised but non-functional.

**Fix:** Either remove `image/heic` from the photo allowlist, or add HEIC detection using a dedicated library (e.g., `github.com/h2non/filetype`) before falling back to `http.DetectContentType`.

---

### FINDING-07 - MEDIUM - No Rate Limiting on Media Service Upload Endpoints

**File:** `services/media/internal/handler/upload_handler.go:47-54`, `services/media/cmd/main.go:129-132`

**Category:** Missing Rate Limiting / DoS

**Description:**
No rate limiting exists anywhere in the media service. CLAUDE.md specifies "Rate limiting on each public endpoint, Redis-backed." The gateway provides rate limiting, but the media service has no defense-in-depth. A single authenticated user can initiate unlimited concurrent chunked upload sessions, filling R2 multipart slots and Redis with session metadata.

**Verified:** Grep for "ratelimit" and "limiter" returns zero results under `services/media/`.

**Impact:** Medium. Requires valid internal token to exploit.

**Fix:** Add Redis-backed rate limiting: max N chunked upload inits per minute per user (e.g., 5), max M chunk parts per minute per user (e.g., 300).

---

### FINDING-08 - MEDIUM - Original Filename Stored Without Sanitization

**File:** `services/media/internal/handler/upload_handler.go:63`, `services/media/internal/handler/upload_handler.go:122`

**Category:** Input Sanitization / Latent Content-Disposition Injection

**Description:**
The original filename is stored verbatim from the client: `file.Filename` for multipart uploads and `req.Filename` from the JSON body for chunked uploads. Neither is sanitized for null bytes, control characters, unicode bidirectional override characters, or path separators before storage in PostgreSQL.

Currently the download handler uses bare `Content-Disposition: attachment` with no filename directive, so the stored filename is never reflected in response headers. However, if `filename=` is ever added to the download header, the unsanitized filename would need RFC 5987 encoding.

**Verified:** `upload_handler.go:63` uses `file.Filename` directly. `upload_handler.go:122` uses `req.Filename` from JSON body without sanitization.

**Impact:** Currently low (filename not in response headers). Latent medium risk if Content-Disposition filename is added.

**Fix:** At upload time, sanitize filename: strip null bytes and control characters, strip path separators, limit length to 255 bytes. If filename goes into `Content-Disposition`, use `mime.FormatMediaType` or RFC 5987 encoding.

---

## Summary

| Severity | Count | Findings |
|----------|-------|---------|
| HIGH | 3 | FINDING-01 (no quota), FINDING-02 (presigned IDOR), FINDING-03 (chunked MIME bypass) |
| MEDIUM | 5 | FINDING-04 through FINDING-08 |
| LOW | 4 | FINDING-09 through FINDING-12 (Low Bucket section) |

**Most Critical:**

1. **FINDING-01** - Zero storage quota enforcement: any authenticated user can upload unlimited data, causing DoS and runaway cloud costs.
2. **FINDING-02** - `GetPresignedURL` / `GetThumbnailURL` / `GetMediumURL` have no `userID` parameter and no `CanAccess` call: exported methods that any importer can call to generate download URLs for any media without authorization.
3. **FINDING-03** - Chunked upload MIME re-validation at completion skips any file whose content sniffs as `application/octet-stream`, allowing mismatched content to be stored with client-declared MIME.

**What is well-implemented:**

- `X-Internal-Token` validation uses `subtle.ConstantTimeCompare` correctly in both handlers (`upload_handler.go:40`, `media_handler.go:40`).
- `getUserID` pattern follows CLAUDE.md convention exactly.
- Sentinel errors + `mapError` pattern correctly implemented (not `apperror` directly from service layer), matching the documented media service exception.
- EXIF stripping for photos: `ProcessImage` re-encodes through `image.Decode` then `jpeg.Encode`, which strips all EXIF by construction. Correct and complete.
- Atomic delete-with-ownership-check (`DeleteByUploader`) prevents TOCTOU on deletion using `DELETE ... WHERE id=$1 AND uploader_id=$2 RETURNING`.
- Lua scripts for chunked upload state updates prevent lost-update race conditions; chunk deduplication by part number prevents S3 multipart failures on retries.
- Path traversal in R2 keys is prevented: `mediaType` validated against `validMediaTypes` whitelist, `uploadID` is a UUID, and `extensionFromMIME` sanitizes extensions via character allowlist at `media_service.go:848-852`.
- No SSRF: no remote URL fetching occurs anywhere; ffmpeg/ffprobe are called on local temp files only.
- `Content-Disposition: attachment` and `X-Content-Type-Options: nosniff` on all download responses prevent browser rendering of served files.
- `processPhotoSync` uploads re-encoded JPEG (EXIF-stripped) then deletes the original unprocessed R2 key. Cleanup is correct.
- Background video/GIF processing passes only a file path to the goroutine (not the full byte slice), preventing OOM on large files.
- Redis fail-closed: Redis errors in chunked upload are treated as hard failures, not silently bypassed.
- `CompleteChunkedUpload` uses atomic Lua GET+verify+DEL to prevent a wrong user from destroying another user's upload metadata.

---

## Low Bucket

**FINDING-09** (Low) - Background goroutines for `processVideoAsync` (`media_service.go:204`) and `processGIFAsync` (`media_service.go:272`) have no `recover()`. A panic in ffmpeg output parsing would crash the entire process. Fix: add `defer func() { if r := recover(); r != nil { slog.Error("panic in media processor", "panic", r) } }()` at the top of each goroutine.

**FINDING-10** (Low) - `BuildMediaResponse` (`media_service.go:884`) generates presigned URLs without performing an access check itself. Access is enforced by callers (`GetInfo`, `GetR2Key`) before this function is invoked. The function has no documented precondition. Fix: add a comment noting the access-check precondition to prevent future callers from using it unsafely.

**FINDING-11** (Low) - Redis key prefix mismatch. Code uses `"chunked:"` (`media_service.go:27`) but CLAUDE.md documents `chunked_upload:{uploadId}`. Fix: align code to `"chunked_upload:"` or update CLAUDE.md.

**FINDING-12** (Low) - `GetObject` `Content-Type` from S3 is passed directly to the HTTP response (`media_handler.go:103,125`). If an R2 object has a crafted `Content-Type` (e.g., via direct S3 write), it would be served verbatim. Mitigated by `X-Content-Type-Options: nosniff` and `Content-Disposition: attachment`. Fix: override with DB-stored `m.MimeType` instead of trusting the S3 response header.

---

## Status: COMPLETED
