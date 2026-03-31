package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/services/media/internal/model"
	"github.com/mst-corp/orbit/services/media/internal/storage"
	"github.com/mst-corp/orbit/services/media/internal/store"
)

const (
	presignTTL       = 1 * time.Hour
	chunkedUploadTTL = 24 * time.Hour
	chunkedKeyPrefix = "chunked:"
)

// MediaService orchestrates upload, processing, download, and deletion.
type MediaService struct {
	store *store.MediaStore
	r2    *storage.R2Client
	rdb   *redis.Client
	nc    *nats.Conn
}

// NewMediaService creates the service.
func NewMediaService(store *store.MediaStore, r2 *storage.R2Client, rdb *redis.Client, nc *nats.Conn) *MediaService {
	return &MediaService{store: store, r2: r2, rdb: rdb, nc: nc}
}

// Upload handles a simple (non-chunked) file upload.
func (s *MediaService) Upload(ctx context.Context, uploaderID uuid.UUID, fileData []byte, filename, mimeType, mediaType string, isOneTime bool) (*model.Media, error) {
	if mediaType == "" {
		mediaType = model.DetectMediaType(mimeType)
	}
	if !model.AllowedMIME(mediaType, mimeType) {
		return nil, model.ErrMIMENotAllowed
	}
	if int64(len(fileData)) > model.SizeLimit(mediaType) {
		return nil, model.ErrFileTooLarge
	}

	mediaID := uuid.New()

	// For async-processed types: write temp file FIRST, then start goroutine with path only.
	// This avoids holding fileData in goroutine memory during ffmpeg processing (#7).
	switch mediaType {
	case model.MediaTypeVideo, model.MediaTypeVideoNote:
		return s.uploadWithAsyncProcessing(ctx, mediaID, uploaderID, fileData, filename, mimeType, mediaType, isOneTime, s.processVideoAsync)
	case model.MediaTypeGIF:
		return s.uploadWithAsyncProcessing(ctx, mediaID, uploaderID, fileData, filename, mimeType, mediaType, isOneTime, s.processGIFAsync)
	}

	ext := extensionFromMIME(mimeType)
	r2Key := fmt.Sprintf("%s/%s/original%s", mediaType, mediaID.String(), ext)

	// Upload original to R2
	if err := s.r2.Upload(ctx, r2Key, bytes.NewReader(fileData), mimeType, int64(len(fileData))); err != nil {
		return nil, fmt.Errorf("upload to R2: %w", err)
	}

	m := &model.Media{
		ID:               mediaID,
		UploaderID:       uploaderID,
		Type:             mediaType,
		MimeType:         mimeType,
		OriginalFilename: strPtr(filename),
		SizeBytes:        int64(len(fileData)),
		R2Key:            r2Key,
		IsOneTime:        isOneTime,
		ProcessingStatus: model.ProcessingPending,
	}

	switch mediaType {
	case model.MediaTypePhoto:
		s.processPhotoSync(ctx, m, fileData)
	case model.MediaTypeVoice:
		s.processVoiceSync(ctx, m, fileData)
	default:
		m.ProcessingStatus = model.ProcessingReady
	}

	if err := s.store.Create(ctx, m); err != nil {
		// Cleanup R2 keys on DB failure (#3)
		s.cleanupR2Keys(ctx, m)
		return nil, fmt.Errorf("create media record: %w", err)
	}
	return m, nil
}

// uploadWithAsyncProcessing handles video/GIF: save temp file, insert DB, then process in goroutine.
func (s *MediaService) uploadWithAsyncProcessing(ctx context.Context, mediaID, uploaderID uuid.UUID,
	fileData []byte, filename, mimeType, mediaType string, isOneTime bool,
	processFn func(mediaID uuid.UUID, tmpPath string)) (*model.Media, error) {

	ext := extensionFromMIME(mimeType)
	r2Key := fmt.Sprintf("%s/%s/original%s", mediaType, mediaID.String(), ext)

	// Upload original to R2
	if err := s.r2.Upload(ctx, r2Key, bytes.NewReader(fileData), mimeType, int64(len(fileData))); err != nil {
		return nil, fmt.Errorf("upload to R2: %w", err)
	}

	// Write temp file BEFORE goroutine so fileData can be GC'd (#7)
	prefix := mediaType
	tmpPath, err := SaveToTemp(fileData, prefix)
	if err != nil {
		return nil, fmt.Errorf("save temp file: %w", err)
	}
	// fileData is no longer needed after this point

	m := &model.Media{
		ID:               mediaID,
		UploaderID:       uploaderID,
		Type:             mediaType,
		MimeType:         mimeType,
		OriginalFilename: strPtr(filename),
		SizeBytes:        int64(len(fileData)),
		R2Key:            r2Key,
		IsOneTime:        isOneTime,
		ProcessingStatus: model.ProcessingProcessing,
	}

	if err := s.store.Create(ctx, m); err != nil {
		os.Remove(tmpPath)
		s.r2.Delete(ctx, r2Key)
		return nil, fmt.Errorf("create media record: %w", err)
	}

	// Goroutine receives only tmpPath, not fileData
	go func() {
		defer os.Remove(tmpPath)
		processFn(mediaID, tmpPath)
	}()

	return m, nil
}

// processPhotoSync resizes and uploads photo variants.
func (s *MediaService) processPhotoSync(ctx context.Context, m *model.Media, data []byte) {
	result, err := ProcessImage(data)
	if err != nil {
		slog.Error("process image failed", "media_id", m.ID, "error", err)
		m.ProcessingStatus = model.ProcessingReady // still usable with original
		return
	}

	m.Width = &result.Width
	m.Height = &result.Height
	m.ProcessingStatus = model.ProcessingReady

	baseKey := fmt.Sprintf("photos/%s", m.ID.String())
	oldR2Key := m.R2Key // track original key for cleanup (#2)

	// Upload thumb
	thumbKey := baseKey + "/thumb_320.jpg"
	if err := s.r2.Upload(ctx, thumbKey, bytes.NewReader(result.Thumb320), "image/jpeg", int64(len(result.Thumb320))); err != nil {
		slog.Error("upload thumb failed", "error", err)
	} else {
		m.ThumbnailR2Key = &thumbKey
	}

	// Upload medium
	medKey := baseKey + "/medium_800.jpg"
	if err := s.r2.Upload(ctx, medKey, bytes.NewReader(result.Medium800), "image/jpeg", int64(len(result.Medium800))); err != nil {
		slog.Error("upload medium failed", "error", err)
	} else {
		m.MediumR2Key = &medKey
	}

	// Re-upload original as JPEG (EXIF stripped)
	origKey := baseKey + "/original.jpg"
	if err := s.r2.Upload(ctx, origKey, bytes.NewReader(result.Original), "image/jpeg", int64(len(result.Original))); err != nil {
		slog.Error("upload original re-encoded failed", "error", err)
		// Keep the original unprocessed key
	} else {
		m.R2Key = origKey
		m.SizeBytes = int64(len(result.Original))
		// Delete the old unprocessed original (#2)
		if oldR2Key != origKey {
			if err := s.r2.Delete(ctx, oldR2Key); err != nil {
				slog.Warn("delete old original R2 key failed", "key", oldR2Key, "error", err)
			}
		}
	}
}

// processVideoAsync extracts thumbnail and metadata. Runs in goroutine.
// tmpPath is cleaned up by the caller goroutine.
func (s *MediaService) processVideoAsync(mediaID uuid.UUID, tmpPath string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	duration, width, height, err := GetVideoMetadata(tmpPath)
	if err != nil {
		slog.Warn("video metadata extraction failed", "error", err)
	}

	var thumbKey *string
	thumb, err := ExtractVideoThumbnail(tmpPath)
	if err != nil {
		slog.Warn("video thumbnail extraction failed", "error", err)
	} else {
		key := fmt.Sprintf("videos/%s/thumb.jpg", mediaID.String())
		if err := s.r2.Upload(ctx, key, bytes.NewReader(thumb), "image/jpeg", int64(len(thumb))); err != nil {
			slog.Error("upload video thumb failed", "error", err)
		} else {
			thumbKey = &key
		}
	}

	var w, h *int
	var dur *float64
	if width > 0 {
		w = &width
	}
	if height > 0 {
		h = &height
	}
	if duration > 0 {
		dur = &duration
	}

	if err := s.store.UpdateProcessingResult(ctx, mediaID, thumbKey, nil, w, h, dur, nil); err != nil {
		slog.Error("update processing result failed", "error", err)
		if err2 := s.store.UpdateProcessingStatus(ctx, mediaID, model.ProcessingFailed); err2 != nil {
			slog.Error("update processing status failed", "error", err2)
		}
		return
	}

	s.publishMediaReady(mediaID)
}

// processVoiceSync extracts waveform and duration.
func (s *MediaService) processVoiceSync(ctx context.Context, m *model.Media, data []byte) {
	tmpFile, err := SaveToTemp(data, "voice")
	if err != nil {
		slog.Error("save temp voice failed", "error", err)
		m.ProcessingStatus = model.ProcessingReady
		return
	}
	defer os.Remove(tmpFile)

	result, err := ExtractWaveform(tmpFile)
	if err != nil {
		slog.Warn("waveform extraction failed", "error", err)
		m.ProcessingStatus = model.ProcessingReady
		return
	}

	m.WaveformData = result.WaveformPeaks
	m.DurationSeconds = &result.Duration
	m.ProcessingStatus = model.ProcessingReady
}

// processGIFAsync converts GIF to MP4. Runs in goroutine.
func (s *MediaService) processGIFAsync(mediaID uuid.UUID, tmpPath string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tmpMP4 := tmpPath + ".mp4"
	defer os.Remove(tmpMP4)

	if err := ConvertGIFToMP4(tmpPath, tmpMP4); err != nil {
		slog.Warn("GIF to MP4 conversion failed", "error", err)
		if err2 := s.store.UpdateProcessingStatus(ctx, mediaID, model.ProcessingReady); err2 != nil {
			slog.Error("update processing status failed", "error", err2)
		}
		s.publishMediaReady(mediaID)
		return
	}

	mp4Data, err := os.ReadFile(tmpMP4)
	if err == nil {
		key := fmt.Sprintf("gif/%s/video.mp4", mediaID.String())
		if err := s.r2.Upload(ctx, key, bytes.NewReader(mp4Data), "video/mp4", int64(len(mp4Data))); err != nil {
			slog.Error("upload gif mp4 failed", "error", err)
		}
	}

	var thumbKey *string
	thumb, err := ExtractVideoThumbnail(tmpMP4)
	if err == nil {
		key := fmt.Sprintf("gif/%s/thumb.jpg", mediaID.String())
		if err := s.r2.Upload(ctx, key, bytes.NewReader(thumb), "image/jpeg", int64(len(thumb))); err == nil {
			thumbKey = &key
		}
	}

	if err := s.store.UpdateProcessingResult(ctx, mediaID, thumbKey, nil, nil, nil, nil, nil); err != nil {
		slog.Error("update gif processing result failed", "error", err)
	}
	s.publishMediaReady(mediaID)
}

// cleanupR2Keys deletes all R2 objects associated with a media record (for rollback).
func (s *MediaService) cleanupR2Keys(ctx context.Context, m *model.Media) {
	if err := s.r2.Delete(ctx, m.R2Key); err != nil {
		slog.Warn("cleanup R2 key failed", "key", m.R2Key, "error", err)
	}
	if m.ThumbnailR2Key != nil {
		if err := s.r2.Delete(ctx, *m.ThumbnailR2Key); err != nil {
			slog.Warn("cleanup thumbnail R2 key failed", "key", *m.ThumbnailR2Key, "error", err)
		}
	}
	if m.MediumR2Key != nil {
		if err := s.r2.Delete(ctx, *m.MediumR2Key); err != nil {
			slog.Warn("cleanup medium R2 key failed", "key", *m.MediumR2Key, "error", err)
		}
	}
}

// --- Download / Info / Delete (sentinel errors: #8) ---

// StreamFile returns the file body and content type for a given media ID and R2 key.
func (s *MediaService) StreamFile(ctx context.Context, r2Key string) (io.ReadCloser, string, error) {
	return s.r2.GetObject(ctx, r2Key)
}

// GetR2Key returns the R2 key for the given media and variant.
func (s *MediaService) GetR2Key(ctx context.Context, id uuid.UUID, variant string) (string, error) {
	m, err := s.store.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	if m == nil {
		return "", model.ErrMediaNotFound
	}
	switch variant {
	case "thumbnail":
		if m.ThumbnailR2Key == nil {
			return "", model.ErrNoThumbnail
		}
		return *m.ThumbnailR2Key, nil
	case "medium":
		if m.MediumR2Key == nil {
			return "", model.ErrNoMedium
		}
		return *m.MediumR2Key, nil
	default:
		return m.R2Key, nil
	}
}

// GetPresignedURL returns a presigned download URL for a media file.
func (s *MediaService) GetPresignedURL(ctx context.Context, id uuid.UUID) (string, error) {
	m, err := s.store.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	if m == nil {
		return "", model.ErrMediaNotFound
	}
	return s.r2.PresignedGetURL(ctx, m.R2Key, presignTTL)
}

// GetThumbnailURL returns a presigned URL for the thumbnail.
func (s *MediaService) GetThumbnailURL(ctx context.Context, id uuid.UUID) (string, error) {
	m, err := s.store.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	if m == nil {
		return "", model.ErrMediaNotFound
	}
	if m.ThumbnailR2Key == nil {
		return "", model.ErrNoThumbnail
	}
	return s.r2.PresignedGetURL(ctx, *m.ThumbnailR2Key, presignTTL)
}

// GetMediumURL returns a presigned URL for the medium-resolution variant.
func (s *MediaService) GetMediumURL(ctx context.Context, id uuid.UUID) (string, error) {
	m, err := s.store.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	if m == nil {
		return "", model.ErrMediaNotFound
	}
	if m.MediumR2Key == nil {
		return "", model.ErrNoMedium
	}
	return s.r2.PresignedGetURL(ctx, *m.MediumR2Key, presignTTL)
}

// GetInfo returns media metadata.
func (s *MediaService) GetInfo(ctx context.Context, id uuid.UUID) (*model.Media, error) {
	m, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, model.ErrMediaNotFound
	}
	return m, nil
}

// Delete removes a media file (only uploader can delete).
func (s *MediaService) Delete(ctx context.Context, id, userID uuid.UUID) error {
	m, err := s.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if m == nil {
		return model.ErrMediaNotFound
	}
	if m.UploaderID != userID {
		return model.ErrNotUploader
	}

	// Delete from R2 (best effort)
	s.cleanupR2Keys(ctx, m)
	return s.store.Delete(ctx, id)
}

// --- Chunked Upload (fixes #1 ownership, #4 Redis errors) ---

// InitChunkedUpload starts a multipart upload.
// validMediaTypes is the set of allowed media_type values for R2 key prefixes.
var validMediaTypes = map[string]bool{
	"photo": true, "video": true, "file": true,
	"voice": true, "videonote": true, "gif": true,
}

func (s *MediaService) InitChunkedUpload(ctx context.Context, uploaderID uuid.UUID, filename, mimeType, mediaType string, totalSize int64) (*model.ChunkedUploadMeta, error) {
	if mediaType == "" {
		mediaType = model.DetectMediaType(mimeType)
	}
	// Validate mediaType against enum to prevent R2 key injection (e.g. "../../admin")
	if !validMediaTypes[mediaType] {
		return nil, model.ErrMIMENotAllowed
	}
	if !model.AllowedMIME(mediaType, mimeType) {
		return nil, model.ErrMIMENotAllowed
	}
	if totalSize > model.SizeLimit(mediaType) {
		return nil, model.ErrFileTooLarge
	}

	uploadID := uuid.New().String()
	ext := extensionFromMIME(mimeType)
	r2Key := fmt.Sprintf("%s/%s/original%s", mediaType, uploadID, ext)

	r2UploadID, err := s.r2.InitMultipartUpload(ctx, r2Key, mimeType)
	if err != nil {
		return nil, fmt.Errorf("init multipart: %w", err)
	}

	chunkSize := model.ChunkSize
	totalChunks := int(totalSize / int64(chunkSize))
	if totalSize%int64(chunkSize) > 0 {
		totalChunks++
	}

	meta := &model.ChunkedUploadMeta{
		ID:          uploadID,
		UploaderID:  uploaderID.String(),
		Filename:    filename,
		MimeType:    mimeType,
		MediaType:   mediaType,
		TotalSize:   totalSize,
		ChunkSize:   chunkSize,
		TotalChunks: totalChunks,
		R2Key:       r2Key,
		R2UploadID:  r2UploadID,
		Parts:       []model.Part{},
	}

	data, _ := json.Marshal(meta)
	if err := s.rdb.Set(ctx, chunkedKeyPrefix+uploadID, data, chunkedUploadTTL).Err(); err != nil {
		s.r2.AbortMultipartUpload(ctx, r2Key, r2UploadID)
		return nil, fmt.Errorf("save chunked meta: %w", err)
	}

	return meta, nil
}

// UploadChunk uploads a single part. Validates ownership (#1).
func (s *MediaService) UploadChunk(ctx context.Context, uploadID string, uploaderID uuid.UUID, partNumber int, data []byte) (int, int, error) {
	if partNumber < 1 || partNumber > 10000 {
		return 0, 0, model.ErrInvalidPartNum
	}

	key := chunkedKeyPrefix + uploadID

	// Read meta
	raw, err := s.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return 0, 0, model.ErrUploadNotFound
	}

	var meta model.ChunkedUploadMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return 0, 0, fmt.Errorf("parse chunked meta: %w", err)
	}

	// Ownership check (#1)
	if meta.UploaderID != uploaderID.String() {
		return 0, 0, model.ErrUploadForbidden
	}

	etag, err := s.r2.UploadPart(ctx, meta.R2Key, meta.R2UploadID, partNumber, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return 0, 0, fmt.Errorf("upload part: %w", err)
	}

	// Atomic update via Lua script to prevent lost-update race (#1 race fix, #4 error handling)
	appendScript := redis.NewScript(`
		local raw = redis.call('GET', KEYS[1])
		if not raw then return redis.error_reply('upload not found') end
		local meta = cjson.decode(raw)
		table.insert(meta.parts, {number = tonumber(ARGV[1]), etag = ARGV[2]})
		local updated = cjson.encode(meta)
		redis.call('SET', KEYS[1], updated, 'EX', tonumber(ARGV[3]))
		return #meta.parts
	`)
	result, err := appendScript.Run(ctx, s.rdb, []string{key},
		partNumber, etag, int(chunkedUploadTTL.Seconds())).Result()
	if err != nil {
		return 0, 0, fmt.Errorf("update chunked meta: %w", err)
	}

	uploadedCount := 0
	switch v := result.(type) {
	case int64:
		uploadedCount = int(v)
	}

	return uploadedCount, meta.TotalChunks, nil
}

// CompleteChunkedUpload finishes the multipart upload.
func (s *MediaService) CompleteChunkedUpload(ctx context.Context, uploadID string, uploaderID uuid.UUID, isOneTime bool) (*model.Media, error) {
	raw, err := s.rdb.Get(ctx, chunkedKeyPrefix+uploadID).Bytes()
	if err != nil {
		return nil, model.ErrUploadNotFound
	}

	var meta model.ChunkedUploadMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("parse chunked meta: %w", err)
	}

	if meta.UploaderID != uploaderID.String() {
		return nil, model.ErrUploadForbidden
	}

	// Complete S3 multipart
	var completed []storage.CompletedPart
	for _, p := range meta.Parts {
		completed = append(completed, storage.CompletedPart{Number: p.Number, ETag: p.ETag})
	}
	if err := s.r2.CompleteMultipartUpload(ctx, meta.R2Key, meta.R2UploadID, completed); err != nil {
		return nil, fmt.Errorf("complete multipart: %w", err)
	}

	m := &model.Media{
		ID:               uuid.MustParse(meta.ID),
		UploaderID:       uploaderID,
		Type:             meta.MediaType,
		MimeType:         meta.MimeType,
		OriginalFilename: strPtr(meta.Filename),
		SizeBytes:        meta.TotalSize,
		R2Key:            meta.R2Key,
		IsOneTime:        isOneTime,
		ProcessingStatus: model.ProcessingReady, // chunked files are typically large — no server-side processing
	}

	if err := s.store.Create(ctx, m); err != nil {
		// Best-effort R2 cleanup to prevent orphaned objects
		if delErr := s.r2.Delete(ctx, meta.R2Key); delErr != nil {
			slog.Error("failed to cleanup orphaned R2 object", "key", meta.R2Key, "error", delErr)
		}
		return nil, fmt.Errorf("create media record: %w", err)
	}

	// Cleanup Redis AFTER successful DB insert — prevents orphaned R2 objects without recovery path
	if err := s.rdb.Del(ctx, chunkedKeyPrefix+uploadID).Err(); err != nil {
		slog.Warn("failed to delete chunked upload key from Redis", "upload_id", uploadID, "error", err)
	}

	s.publishMediaReady(m.ID)
	return m, nil
}

// StartCleanupLoop runs orphan cleanup every interval.
func (s *MediaService) StartCleanupLoop(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				keys, err := s.store.CleanupOrphaned(ctx, 24)
				if err != nil {
					slog.Error("cleanup orphaned media failed", "error", err)
					continue
				}
				for _, key := range keys {
					if err := s.r2.Delete(ctx, key); err != nil {
						slog.Warn("delete orphaned R2 key failed", "key", key, "error", err)
					}
				}
				if len(keys) > 0 {
					slog.Info("cleaned up orphaned media", "r2_keys_deleted", len(keys))
				}
			}
		}
	}()
}

// publishMediaReady sends a NATS event when processing is done.
func (s *MediaService) publishMediaReady(mediaID uuid.UUID) {
	if s.nc == nil {
		return
	}
	ctx := context.Background()
	m, err := s.store.GetByID(ctx, mediaID)
	if err != nil || m == nil {
		return
	}

	event := map[string]interface{}{
		"event": "media_ready",
		"data": map[string]interface{}{
			"media_id":          m.ID.String(),
			"type":              m.Type,
			"processing_status": m.ProcessingStatus,
		},
	}
	data, _ := json.Marshal(event)
	subject := fmt.Sprintf("orbit.media.%s.ready", m.UploaderID.String())
	if err := s.nc.Publish(subject, data); err != nil {
		slog.Error("publish media_ready failed", "error", err)
	}
}

// --- Helpers ---

func extensionFromMIME(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/heic":
		return ".heic"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	case "audio/ogg":
		return ".ogg"
	case "audio/webm":
		return ".weba"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav":
		return ".wav"
	default:
		parts := strings.Split(mime, "/")
		if len(parts) == 2 {
			// Sanitize subtype to prevent path traversal in R2 keys
			sub := strings.Map(func(r rune) rune {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '+' || r == '.' || r == '-' {
					return r
				}
				return -1
			}, parts[1])
			if sub != "" {
				return "." + sub
			}
		}
		return ".bin"
	}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// GetStore returns the underlying media store (for messaging service to use shared DB).
func (s *MediaService) GetStore() *store.MediaStore {
	return s.store
}

// R2KeyToURL converts an R2 key to a presigned URL.
func (s *MediaService) R2KeyToURL(ctx context.Context, key string) string {
	url, err := s.r2.PresignedGetURL(ctx, key, presignTTL)
	if err != nil {
		return ""
	}
	return url
}

// BuildMediaResponse builds the API response for a media record.
func (s *MediaService) BuildMediaResponse(ctx context.Context, m *model.Media) *model.MediaResponse {
	resp := &model.MediaResponse{
		ID:               m.ID.String(),
		Type:             m.Type,
		MimeType:         m.MimeType,
		SizeBytes:        m.SizeBytes,
		Width:            m.Width,
		Height:           m.Height,
		DurationSeconds:  m.DurationSeconds,
		WaveformData:     m.WaveformData,
		ProcessingStatus: m.ProcessingStatus,
	}
	if m.OriginalFilename != nil {
		resp.OriginalFilename = *m.OriginalFilename
	}
	resp.URL = s.R2KeyToURL(ctx, m.R2Key)
	if m.ThumbnailR2Key != nil {
		resp.ThumbnailURL = s.R2KeyToURL(ctx, *m.ThumbnailR2Key)
	}
	if m.MediumR2Key != nil {
		resp.MediumURL = s.R2KeyToURL(ctx, *m.MediumR2Key)
	}
	return resp
}
