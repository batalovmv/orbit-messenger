package model

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors for service layer.
var (
	ErrMediaNotFound    = errors.New("media not found")
	ErrNotUploader      = errors.New("forbidden: not the uploader")
	ErrAccessDenied     = errors.New("forbidden: access denied")
	ErrNoThumbnail      = errors.New("no thumbnail available")
	ErrNoMedium         = errors.New("no medium resolution available")
	ErrUploadNotFound   = errors.New("chunked upload not found or expired")
	ErrUploadForbidden  = errors.New("forbidden: not the upload owner")
	ErrFileTooLarge     = errors.New("file too large")
	ErrMIMENotAllowed   = errors.New("MIME type not allowed")
	ErrInvalidPartNum   = errors.New("invalid part number (must be 1-10000)")
)

// Media types
const (
	MediaTypePhoto     = "photo"
	MediaTypeVideo     = "video"
	MediaTypeFile      = "file"
	MediaTypeVoice     = "voice"
	MediaTypeVideoNote = "videonote"
	MediaTypeGIF       = "gif"
)

// Processing statuses
const (
	ProcessingPending    = "pending"
	ProcessingProcessing = "processing"
	ProcessingReady      = "ready"
	ProcessingFailed     = "failed"
)

// Size limits (bytes)
const (
	MaxPhotoSize     = 10 * 1024 * 1024       // 10 MB
	MaxVideoNoteSize = 50 * 1024 * 1024       // 50 MB
	MaxGIFSize       = 20 * 1024 * 1024       // 20 MB
	MaxVoiceSize     = 200 * 1024 * 1024      // 200 MB
	MaxFileSize      = 2 * 1024 * 1024 * 1024 // 2 GB
	MaxVideoSize     = 2 * 1024 * 1024 * 1024 // 2 GB

	// Simple upload limit (anything bigger requires chunked)
	SimpleUploadLimit = 50 * 1024 * 1024 // 50 MB
	ChunkSize         = 10 * 1024 * 1024 // 10 MB per chunk
)

// Media represents a stored media file.
type Media struct {
	ID               uuid.UUID `json:"id"`
	UploaderID       uuid.UUID `json:"uploader_id"`
	Type             string    `json:"type"`
	MimeType         string    `json:"mime_type"`
	OriginalFilename *string   `json:"original_filename,omitempty"`
	SizeBytes        int64     `json:"size_bytes"`
	R2Key            string    `json:"r2_key"`
	ThumbnailR2Key   *string   `json:"thumbnail_r2_key,omitempty"`
	MediumR2Key      *string   `json:"medium_r2_key,omitempty"`
	Width            *int      `json:"width,omitempty"`
	Height           *int      `json:"height,omitempty"`
	DurationSeconds  *float64  `json:"duration_seconds,omitempty"`
	WaveformData     []byte    `json:"waveform_data,omitempty"`
	IsOneTime        bool      `json:"is_one_time"`
	ProcessingStatus string    `json:"processing_status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// MediaResponse is the JSON response for API endpoints.
type MediaResponse struct {
	ID               string   `json:"id"`
	Type             string   `json:"type"`
	MimeType         string   `json:"mime_type"`
	OriginalFilename string   `json:"original_filename,omitempty"`
	SizeBytes        int64    `json:"size_bytes"`
	URL              string   `json:"url,omitempty"`
	ThumbnailURL     string   `json:"thumbnail_url,omitempty"`
	MediumURL        string   `json:"medium_url,omitempty"`
	Width            *int     `json:"width,omitempty"`
	Height           *int     `json:"height,omitempty"`
	DurationSeconds  *float64 `json:"duration_seconds,omitempty"`
	WaveformData     []int    `json:"waveform_data,omitempty"`
	ProcessingStatus string   `json:"processing_status"`
}

// ChunkedUploadMeta stored in Redis.
type ChunkedUploadMeta struct {
	ID          string `json:"id"`
	UploaderID  string `json:"uploader_id"`
	Filename    string `json:"filename"`
	MimeType    string `json:"mime_type"`
	MediaType   string `json:"media_type"`
	TotalSize   int64  `json:"total_size"`
	ChunkSize   int    `json:"chunk_size"`
	TotalChunks int    `json:"total_chunks"`
	R2Key       string `json:"r2_key"`
	R2UploadID  string `json:"r2_upload_id"`
	Parts       []Part `json:"parts"`
}

// Part represents a completed S3 multipart upload part.
type Part struct {
	Number int    `json:"number"`
	ETag   string `json:"etag"`
}

// SizeLimit returns the max upload size for a given media type.
func SizeLimit(mediaType string) int64 {
	switch mediaType {
	case MediaTypePhoto:
		return MaxPhotoSize
	case MediaTypeVideo:
		return MaxVideoSize
	case MediaTypeFile:
		return MaxFileSize
	case MediaTypeVoice:
		return MaxVoiceSize
	case MediaTypeVideoNote:
		return MaxVideoNoteSize
	case MediaTypeGIF:
		return MaxGIFSize
	default:
		return MaxFileSize
	}
}

// AllowedMIME returns whether a MIME type is allowed for the given media type.
func AllowedMIME(mediaType, mime string) bool {
	switch mediaType {
	case MediaTypePhoto:
		return mime == "image/jpeg" || mime == "image/png" || mime == "image/webp" || mime == "image/gif" || mime == "image/heic"
	case MediaTypeVideo:
		return mime == "video/mp4" || mime == "video/webm" || mime == "video/quicktime"
	case MediaTypeVoice:
		return mime == "audio/ogg" || mime == "application/ogg" || mime == "audio/webm" || mime == "audio/mpeg" || mime == "audio/wav"
	case MediaTypeVideoNote:
		return mime == "video/mp4" || mime == "video/webm"
	case MediaTypeGIF:
		return mime == "image/gif"
	case MediaTypeFile:
		return true // any MIME for files
	default:
		return true
	}
}

// DetectMediaType guesses the media type from a MIME string.
func DetectMediaType(mime string) string {
	switch {
	case mime == "image/gif":
		return MediaTypeGIF
	case len(mime) > 6 && mime[:6] == "image/":
		return MediaTypePhoto
	case len(mime) > 6 && mime[:6] == "video/":
		return MediaTypeVideo
	case len(mime) > 6 && mime[:6] == "audio/", mime == "application/ogg":
		return MediaTypeVoice
	default:
		return MediaTypeFile
	}
}
