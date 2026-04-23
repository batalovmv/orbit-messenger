package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/metrics"
	"github.com/mst-corp/orbit/services/media/internal/model"
	"github.com/mst-corp/orbit/services/media/internal/scanner"
	"github.com/mst-corp/orbit/services/media/internal/storage"
	"github.com/mst-corp/orbit/services/media/internal/store"
)

const (
	presignTTL       = 1 * time.Hour
	chunkedUploadTTL = 1 * time.Hour
	chunkedKeyPrefix = "chunked:"
)

type r2Storage interface {
	Upload(ctx context.Context, key string, body io.Reader, contentType string, size int64) error
	Delete(ctx context.Context, key string) error
	PresignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error)
	InitMultipartUpload(ctx context.Context, key, contentType string) (string, error)
	UploadPart(ctx context.Context, key, uploadID string, partNum int, body io.Reader, size int64) (string, error)
	CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []storage.CompletedPart) error
	AbortMultipartUpload(ctx context.Context, key, uploadID string) error
	GetObject(ctx context.Context, key string) (io.ReadCloser, string, error)
	GetObjectRange(ctx context.Context, key, rangeHeader string) (*storage.RangeResult, error)
}

// MediaService orchestrates upload, processing, download, and deletion.
type MediaService struct {
	store               store.Store
	r2                  r2Storage
	rdb                 *redis.Client
	nc                  *nats.Conn
	maxUserStorageBytes int64
	scanner             scanner.Scanner
	presignGetURL       func(ctx context.Context, key string, ttl time.Duration) (string, error)
	auditRetryer        auditRetryer
	auditMetrics        *auditMetrics
}

// NewMediaService creates the service.
func NewMediaService(st store.Store, r2 *storage.R2Client, rdb *redis.Client, nc *nats.Conn) *MediaService {
	var r2Client r2Storage
	if r2 != nil {
		r2Client = r2
	}
	return NewMediaServiceWithR2(st, r2Client, rdb, nc)
}

func NewMediaServiceWithR2(st store.Store, r2 r2Storage, rdb *redis.Client, nc *nats.Conn) *MediaService {
	svc := &MediaService{
		store:        st,
		r2:           r2,
		rdb:          rdb,
		nc:           nc,
		auditRetryer: newAuditRetryer(),
	}
	if r2 != nil {
		svc.presignGetURL = r2.PresignedGetURL
	}
	return svc
}

func (s *MediaService) WithMaxUserStorageBytes(limit int64) *MediaService {
	s.maxUserStorageBytes = limit
	return s
}

func (s *MediaService) WithScanner(sc scanner.Scanner) *MediaService {
	s.scanner = sc
	return s
}

func (s *MediaService) WithAuditMetrics(reg *metrics.Registry) *MediaService {
	s.auditMetrics = newAuditMetrics(reg)
	if retryer, ok := s.auditRetryer.(*boundedAuditRetryer); ok {
		retryer.metrics = s.auditMetrics
	}
	return s
}

func (s *MediaService) WithAuditRetryer(retryer auditRetryer) *MediaService {
	if retryer != nil {
		s.auditRetryer = retryer
	}
	return s
}

func (s *MediaService) ensureUserStorageAvailable(ctx context.Context, userID uuid.UUID, incomingSize int64) error {
	if s.maxUserStorageBytes <= 0 {
		return nil
	}
	current, err := s.store.GetUserStorageBytes(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user storage bytes: %w", err)
	}
	if current+incomingSize > s.maxUserStorageBytes {
		return apperror.TooManyRequests("Storage quota exceeded")
	}
	return nil
}

// UploadEncrypted stores an opaque ciphertext blob for Phase 7.1 E2E media.
// The server never sees plaintext: no MIME sniff, no processing pipeline, no
// thumbnail generation. The client is expected to AES-256-GCM-encrypt the file
// and ship the ciphertext plus the declared media type (so the UI can render
// the right placeholder) and the original filename/size (for display only —
// both values are also carried inside the encrypted envelope that the
// recipient decrypts).
//
// Size limit is enforced by the declared media type. Quota accounting uses the
// ciphertext length (which is what actually consumes R2 storage).
func (s *MediaService) UploadEncrypted(ctx context.Context, uploaderID uuid.UUID, ciphertext []byte, declaredType, declaredFilename string, isOneTime bool) (*model.Media, error) {
	if declaredType == "" {
		return nil, apperror.BadRequest("media type is required for encrypted upload")
	}
	switch declaredType {
	case model.MediaTypePhoto, model.MediaTypeVideo, model.MediaTypeFile,
		model.MediaTypeVoice, model.MediaTypeVideoNote, model.MediaTypeGIF:
		// valid
	default:
		return nil, apperror.BadRequest("unknown media type for encrypted upload")
	}
	if len(ciphertext) == 0 {
		return nil, apperror.BadRequest("empty ciphertext")
	}
	// Allow ciphertext overhead (AES-256-GCM adds 16-byte tag, envelope up to
	// ~128 bytes of framing) on top of the plaintext size limit.
	maxSize := model.SizeLimit(declaredType) + 512
	if int64(len(ciphertext)) > maxSize {
		return nil, model.ErrFileTooLarge
	}
	if err := s.ensureUserStorageAvailable(ctx, uploaderID, int64(len(ciphertext))); err != nil {
		return nil, err
	}

	mediaID := uuid.New()
	r2Key := fmt.Sprintf("encrypted/%s/blob.bin", mediaID.String())

	if err := s.r2.Upload(ctx, r2Key, bytes.NewReader(ciphertext), "application/octet-stream", int64(len(ciphertext))); err != nil {
		return nil, fmt.Errorf("upload encrypted to R2: %w", err)
	}

	m := &model.Media{
		ID:               mediaID,
		UploaderID:       uploaderID,
		Type:             declaredType,
		MimeType:         "application/octet-stream",
		OriginalFilename: strPtr(declaredFilename),
		SizeBytes:        int64(len(ciphertext)),
		R2Key:            r2Key,
		IsOneTime:        isOneTime,
		ProcessingStatus: model.ProcessingReady,
	}

	if err := s.store.Create(ctx, m); err != nil {
		if delErr := s.r2.Delete(ctx, r2Key); delErr != nil {
			slog.Warn("cleanup encrypted R2 key after DB failure", "key", r2Key, "error", delErr)
		}
		return nil, fmt.Errorf("create encrypted media record: %w", err)
	}
	return m, nil
}

// Upload handles a simple (non-chunked) file upload.
func (s *MediaService) Upload(ctx context.Context, uploaderID uuid.UUID, fileData []byte, filename, mimeType, mediaType string, isOneTime bool, auditCtx *model.UploadAuditContext) (*model.Media, error) {
	if mediaType == "" {
		mediaType = model.DetectMediaType(mimeType)
	}
	if !model.AllowedMIME(mediaType, mimeType) {
		return nil, model.ErrMIMENotAllowed
	}
	if int64(len(fileData)) > model.SizeLimit(mediaType) {
		return nil, model.ErrFileTooLarge
	}
	if err := s.ensureUserStorageAvailable(ctx, uploaderID, int64(len(fileData))); err != nil {
		return nil, err
	}

	// ClamAV virus scan before upload
	if s.scanner != nil {
		scanCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		result, scanErr := s.scanner.Scan(scanCtx, bytes.NewReader(fileData), filename)
		if scanErr != nil {
			slog.Warn("virus scan failed, rejecting upload",
				"event", "scan_error", "error", scanErr,
				"filename", filename, "user_id", uploaderID)
			return nil, apperror.ServiceUnavailable("File scanning temporarily unavailable, please retry")
		}
		if result != nil && !result.Clean {
			slog.Warn("virus detected in upload",
				"event", "virus_detected", "virus", result.Virus,
				"filename", filename, "user_id", uploaderID,
				"size_bytes", len(fileData), "mime_type", mimeType)
			if auditCtx != nil {
				mediaID := uuid.New()
				if err := s.recordVirusDetectionAudit(auditCtx, mediaID, result.Virus); err != nil {
					return nil, err
				}
			}
			return nil, &apperror.AppError{Code: "virus_detected", Message: "File rejected: malware detected", Status: 422}
		}

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
func (s *MediaService) recordVirusDetectionAudit(auditCtx *model.UploadAuditContext, mediaID uuid.UUID, clamAVResult string) error {
	details, err := json.Marshal(map[string]any{
		"filename":          auditCtx.Filename,
		"mime_type":         auditCtx.MimeType,
		"size":              auditCtx.Size,
		"clamav_result":     clamAVResult,
		"upload_attempt_id": auditCtx.UploadAttemptID,
	})
	if err != nil {
		return fmt.Errorf("marshal virus detection audit details: %w", err)
	}

	var ipAddress *string
	if auditCtx.TrustedClientIP != "" {
		ipAddress = &auditCtx.TrustedClientIP
	}
	var userAgent *string
	if auditCtx.UserAgent != "" {
		userAgent = &auditCtx.UserAgent
	}

	retryer := s.auditRetryer
	if retryer == nil {
		retryer = newAuditRetryer()
	}
	attempts, appendErr := retryer.run(func(attemptCtx context.Context) error {
		return s.appendVirusDetectionAudit(attemptCtx, auditCtx, details, ipAddress, userAgent)
	})
	if appendErr != nil {
		slog.Error("virus detection audit persistently failed",
			"event", "audit_persistent_failure",
			"media_id", mediaID,
			"user_id", auditCtx.UserID,
			"virus_name", clamAVResult,
			"attempts_count", attempts,
			"error", appendErr,
		)
		return &apperror.AppError{
			Code:    "audit_unavailable",
			Message: "Upload service temporarily unavailable, please retry.",
			Status:  503,
		}
	}
	return nil
}

func (s *MediaService) appendVirusDetectionAudit(ctx context.Context, auditCtx *model.UploadAuditContext, details []byte, ipAddress, userAgent *string) error {
	if err := s.store.AppendAuditLog(
		ctx,
		auditCtx.UserID,
		model.AuditActionVirusDetected,
		model.AuditTargetTypeUpload,
		auditCtx.UploadAttemptID,
		details,
		ipAddress,
		userAgent,
	); err != nil {
		return fmt.Errorf("append virus detection audit log: %w", err)
	}
	return nil
}

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
		if delErr := s.r2.Delete(ctx, r2Key); delErr != nil {
			slog.Warn("cleanup R2 key after DB failure", "key", r2Key, "error", delErr)
		}
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

	var mp4Key *string
	mp4Data, err := os.ReadFile(tmpMP4)
	if err != nil {
		slog.Error("read converted mp4 failed", "error", err, "media_id", mediaID, "path", tmpMP4)
	} else {
		key := fmt.Sprintf("gif/%s/video.mp4", mediaID.String())
		if err := s.r2.Upload(ctx, key, bytes.NewReader(mp4Data), "video/mp4", int64(len(mp4Data))); err != nil {
			slog.Error("upload gif mp4 failed", "error", err)
		} else {
			mp4Key = &key
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

	if err := s.store.UpdateProcessingResult(ctx, mediaID, thumbKey, mp4Key, nil, nil, nil, nil); err != nil {
		slog.Error("update gif processing result failed", "error", err)
		if statusErr := s.store.UpdateProcessingStatus(ctx, mediaID, model.ProcessingFailed); statusErr != nil {
			slog.Error("failed to mark gif as failed", "error", statusErr)
		}
		return
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

// StreamFileRange returns a byte range of a file from storage.
func (s *MediaService) StreamFileRange(ctx context.Context, r2Key, rangeHeader string) (*storage.RangeResult, error) {
	return s.r2.GetObjectRange(ctx, r2Key, rangeHeader)
}

// GetR2Key returns the R2 key for the given media and variant.
// userID is used to enforce access control (S3 fix): the caller must be the uploader
// or the media must be attached to at least one published message.
func (s *MediaService) GetR2Key(ctx context.Context, id, userID uuid.UUID, variant string) (string, error) {
	ok, err := s.store.CanAccess(ctx, id, userID)
	if err != nil {
		return "", fmt.Errorf("access check: %w", err)
	}
	if !ok {
		return "", model.ErrAccessDenied
	}

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
func (s *MediaService) GetPresignedURL(ctx context.Context, id, userID uuid.UUID) (string, error) {
	if err := s.ensurePresignAccess(ctx, id, userID); err != nil {
		return "", err
	}
	m, err := s.store.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	if m == nil {
		return "", model.ErrMediaNotFound
	}
	return s.presignURL(ctx, m.R2Key)
}

// GetThumbnailURL returns a presigned URL for the thumbnail.
func (s *MediaService) GetThumbnailURL(ctx context.Context, id, userID uuid.UUID) (string, error) {
	if err := s.ensurePresignAccess(ctx, id, userID); err != nil {
		return "", err
	}
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
	return s.presignURL(ctx, *m.ThumbnailR2Key)
}

// GetMediumURL returns a presigned URL for the medium-resolution variant.
func (s *MediaService) GetMediumURL(ctx context.Context, id, userID uuid.UUID) (string, error) {
	if err := s.ensurePresignAccess(ctx, id, userID); err != nil {
		return "", err
	}
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
	return s.presignURL(ctx, *m.MediumR2Key)
}

// GetInfo returns media metadata.
// userID is used to enforce access control (S3 fix).
func (s *MediaService) GetInfo(ctx context.Context, id, userID uuid.UUID) (*model.Media, error) {
	ok, err := s.store.CanAccess(ctx, id, userID)
	if err != nil {
		return nil, fmt.Errorf("access check: %w", err)
	}
	if !ok {
		return nil, model.ErrAccessDenied
	}

	m, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, model.ErrMediaNotFound
	}
	return m, nil
}

func (s *MediaService) ensurePresignAccess(ctx context.Context, id, userID uuid.UUID) error {
	if userID == uuid.Nil {
		return fmt.Errorf("user ID is required")
	}

	ok, err := s.store.CanAccess(ctx, id, userID)
	if err != nil {
		return fmt.Errorf("access check: %w", err)
	}
	if !ok {
		return model.ErrMediaNotFound
	}

	return nil
}

func (s *MediaService) presignURL(ctx context.Context, key string) (string, error) {
	if s.presignGetURL == nil {
		return "", fmt.Errorf("presign client not configured")
	}
	return s.presignGetURL(ctx, key, presignTTL)
}

// Delete removes a media file (only uploader can delete).
// Uses atomic SQL to prevent TOCTOU race between ownership check and deletion.
func (s *MediaService) Delete(ctx context.Context, id, userID uuid.UUID) error {
	r2Key, thumbKey, medKey, err := s.store.DeleteByUploader(ctx, id, userID)
	if err != nil {
		return err
	}

	// Best-effort R2 cleanup after successful DB delete
	if err := s.r2.Delete(ctx, r2Key); err != nil {
		slog.Warn("cleanup R2 key failed", "key", r2Key, "error", err)
	}
	if thumbKey != nil {
		if err := s.r2.Delete(ctx, *thumbKey); err != nil {
			slog.Warn("cleanup thumbnail R2 key failed", "key", *thumbKey, "error", err)
		}
	}
	if medKey != nil {
		if err := s.r2.Delete(ctx, *medKey); err != nil {
			slog.Warn("cleanup medium R2 key failed", "key", *medKey, "error", err)
		}
	}
	return nil
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
	if err := s.ensureUserStorageAvailable(ctx, uploaderID, totalSize); err != nil {
		return nil, err
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

	data, err := json.Marshal(meta)
	if err != nil {
		if abortErr := s.r2.AbortMultipartUpload(ctx, r2Key, r2UploadID); abortErr != nil {
			slog.Warn("abort multipart upload failed after marshal error", "key", r2Key, "error", abortErr)
		}
		return nil, fmt.Errorf("marshal chunked meta: %w", err)
	}
	if err := s.rdb.Set(ctx, chunkedKeyPrefix+uploadID, data, chunkedUploadTTL).Err(); err != nil {
		if abortErr := s.r2.AbortMultipartUpload(ctx, r2Key, r2UploadID); abortErr != nil {
			slog.Warn("abort multipart upload failed after redis error", "key", r2Key, "error", abortErr)
		}
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

	// Read meta — fail-closed: Redis errors must not be treated as "not found"
	raw, err := s.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return 0, 0, model.ErrUploadNotFound
	}
	if err != nil {
		return 0, 0, fmt.Errorf("redis get chunked meta: %w", err)
	}

	var meta model.ChunkedUploadMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return 0, 0, fmt.Errorf("parse chunked meta: %w", err)
	}

	// Ownership check (#1)
	if meta.UploaderID != uploaderID.String() {
		return 0, 0, model.ErrUploadForbidden
	}

	// Validate part number against actual total chunks (#10 audit fix)
	if partNumber > meta.TotalChunks {
		return 0, 0, model.ErrInvalidPartNum
	}

	etag, err := s.r2.UploadPart(ctx, meta.R2Key, meta.R2UploadID, partNumber, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return 0, 0, fmt.Errorf("upload part: %w", err)
	}

	// Atomic update via Lua script to prevent lost-update race (#1 race fix, #4 error handling).
	// Deduplicates by part number so retried chunk uploads don't create duplicate entries
	// that would cause CompleteMultipartUpload to fail (#9 audit fix).
	appendScript := redis.NewScript(`
		local raw = redis.call('GET', KEYS[1])
		if not raw then return redis.error_reply('upload not found') end
		local meta = cjson.decode(raw)
		local partNum = tonumber(ARGV[1])
		for i, p in ipairs(meta.parts) do
			if p.number == partNum then
				table.remove(meta.parts, i)
				break
			end
		end
		table.insert(meta.parts, {number = partNum, etag = ARGV[2]})
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

// completeChunkedScript atomically verifies ownership and deletes the key.
// Returns the raw JSON if owner matches, or an error string if not.
// This prevents GetDel from destroying metadata before ownership is verified.
var completeChunkedScript = redis.NewScript(`
	local raw = redis.call('GET', KEYS[1])
	if not raw then return redis.error_reply('not_found') end
	local meta = cjson.decode(raw)
	if meta.uploader_id ~= ARGV[1] then return redis.error_reply('forbidden') end
	redis.call('DEL', KEYS[1])
	return raw
`)

var abortChunkedScript = redis.NewScript(`
	local raw = redis.call('GET', KEYS[1])
	if not raw then return redis.error_reply('not_found') end
	local meta = cjson.decode(raw)
	if meta.uploader_id ~= ARGV[1] then return redis.error_reply('forbidden') end
	redis.call('DEL', KEYS[1])
	return raw
`)

// AbortChunkedUpload cancels an in-progress multipart upload.
func (s *MediaService) AbortChunkedUpload(ctx context.Context, uploadID string, uploaderID uuid.UUID) error {
	key := chunkedKeyPrefix + uploadID
	rawStr, err := abortChunkedScript.Run(ctx, s.rdb, []string{key}, uploaderID.String()).Text()
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not_found") {
			return model.ErrUploadNotFound
		}
		if strings.Contains(errMsg, "forbidden") {
			return model.ErrUploadForbidden
		}
		return fmt.Errorf("abort chunked script: %w", err)
	}

	var meta model.ChunkedUploadMeta
	if err := json.Unmarshal([]byte(rawStr), &meta); err != nil {
		return fmt.Errorf("parse chunked meta: %w", err)
	}

	if err := s.r2.AbortMultipartUpload(ctx, meta.R2Key, meta.R2UploadID); err != nil {
		return fmt.Errorf("abort multipart: %w", err)
	}

	return nil
}

// CompleteChunkedUpload finishes the multipart upload.
func (s *MediaService) CompleteChunkedUpload(ctx context.Context, uploadID string, uploaderID uuid.UUID, isOneTime bool) (*model.Media, error) {
	// Lua script: atomically GET + verify owner + DEL — prevents destroying
	// metadata before ownership check (if wrong user, key stays intact).
	key := chunkedKeyPrefix + uploadID
	rawStr, err := completeChunkedScript.Run(ctx, s.rdb, []string{key}, uploaderID.String()).Text()
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not_found") {
			return nil, model.ErrUploadNotFound
		}
		if strings.Contains(errMsg, "forbidden") {
			return nil, model.ErrUploadForbidden
		}
		return nil, fmt.Errorf("complete chunked script: %w", err)
	}

	var meta model.ChunkedUploadMeta
	if err := json.Unmarshal([]byte(rawStr), &meta); err != nil {
		return nil, fmt.Errorf("parse chunked meta: %w", err)
	}

	// Complete S3 multipart
	var completed []storage.CompletedPart
	for _, p := range meta.Parts {
		completed = append(completed, storage.CompletedPart{Number: p.Number, ETag: p.ETag})
	}
	if err := s.r2.CompleteMultipartUpload(ctx, meta.R2Key, meta.R2UploadID, completed); err != nil {
		return nil, fmt.Errorf("complete multipart: %w", err)
	}

	// Content sniff: verify declared MIME matches actual file content.
	// Chunked init only checks client-supplied mime_type; we must verify after assembly.
	body, _, err := s.r2.GetObject(ctx, meta.R2Key)
	if err == nil {
		sniffBuf := make([]byte, 512)
		n, _ := io.ReadAtLeast(body, sniffBuf, 1)
		body.Close()
		if n > 0 {
			detectedMIME := http.DetectContentType(sniffBuf[:n])
			if !isAllowedChunkedMIME(meta.MediaType, meta.MimeType, detectedMIME) {
				if delErr := s.r2.Delete(ctx, meta.R2Key); delErr != nil {
					slog.Error("cleanup R2 after MIME mismatch", "key", meta.R2Key, "error", delErr)
				}
				return nil, model.ErrMIMENotAllowed
			}
		}
	}

	mediaID, err := uuid.Parse(meta.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid upload ID in metadata: %w", err)
	}

	m := &model.Media{
		ID:               mediaID,
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

	// Redis key already deleted atomically via Lua script above

	s.publishMediaReady(m.ID)
	return m, nil
}

func isAllowedChunkedMIME(mediaType, declaredMIME, detectedMIME string) bool {
	if mediaType == model.MediaTypeFile {
		return detectedMIME == "application/octet-stream" || model.AllowedMIME(mediaType, detectedMIME)
	}
	if detectedMIME == "application/octet-stream" {
		return declaredMIME == "application/octet-stream"
	}
	return detectedMIME == declaredMIME && model.AllowedMIME(mediaType, detectedMIME)
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

	eventData := map[string]interface{}{
		"media_id":          m.ID.String(),
		"type":              m.Type,
		"processing_status": m.ProcessingStatus,
	}
	if m.Width != nil {
		eventData["width"] = *m.Width
	}
	if m.Height != nil {
		eventData["height"] = *m.Height
	}
	if m.DurationSeconds != nil {
		eventData["duration_seconds"] = *m.DurationSeconds
	}
	if m.ThumbnailR2Key != nil {
		eventData["has_thumbnail"] = true
	}
	event := map[string]interface{}{
		"event": "media_ready",
		"data":  eventData,
	}
	data, err := json.Marshal(event)
	if err != nil {
		slog.Error("failed to marshal media_ready event", "error", err)
		return
	}
	subject := fmt.Sprintf("orbit.media.%s.ready", m.UploaderID.String())
	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  nats.Header{},
	}
	// Nats-Msg-Id enables JetStream server-side dedup and gateway dedup cache.
	msg.Header.Set("Nats-Msg-Id", uuid.New().String())
	if err := s.nc.PublishMsg(msg); err != nil {
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
	case "image/svg+xml":
		return ".svg"
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
func (s *MediaService) GetStore() store.Store {
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
		WaveformData:     bytesToInts(m.WaveformData),
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

// bytesToInts converts []byte waveform to []int for JSON serialization as number array.
func bytesToInts(b []byte) []int {
	if len(b) == 0 {
		return nil
	}
	out := make([]int, len(b))
	for i, v := range b {
		out[i] = int(v)
	}
	return out
}
