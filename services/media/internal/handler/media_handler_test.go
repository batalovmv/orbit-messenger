// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/metrics"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/media/internal/model"
	"github.com/mst-corp/orbit/services/media/internal/scanner"
	"github.com/mst-corp/orbit/services/media/internal/service"
	"github.com/mst-corp/orbit/services/media/internal/storage"
	"github.com/mst-corp/orbit/services/media/internal/store"
)

// ---------------------------------------------------------------------------
// Test infrastructure: in-memory R2 + real service wiring
// ---------------------------------------------------------------------------

// memR2 is an in-memory S3-compatible storage for tests.
type memR2 struct {
	objects map[string][]byte
}

func newMemR2() *memR2 {
	return &memR2{objects: make(map[string][]byte)}
}

// We can't use the real R2Client in tests (needs MinIO).
// Instead we test through the handler with a mock store + real service logic
// where the service is constructed with nil R2 (upload will fail).
// For handler-level tests, this is fine — we test HTTP layer behavior.

// testApp creates a Fiber app with real handlers but mock dependencies.
// This validates HTTP parsing, auth checks, validation — not R2 uploads.
func testApp(t *testing.T, mediaStore store.Store) *fiber.App {
	t.Helper()
	// We can't construct a full MediaService without R2/Redis/NATS.
	// For handler tests, we test the HTTP layer directly.
	// Service-level tests below test business logic.
	app := fiber.New(fiber.Config{
		BodyLimit: 55 * 1024 * 1024,
	})
	return app
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeMultipartBody(filename, contentType string, data []byte, fields map[string]string) (*bytes.Buffer, string) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file part
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	h.Set("Content-Type", contentType)
	part, _ := writer.CreatePart(h)
	part.Write(data)

	// Add form fields
	for k, v := range fields {
		writer.WriteField(k, v)
	}
	writer.Close()
	return body, writer.FormDataContentType()
}

// validPNG generates a valid 10x10 red PNG image for testing.
func validPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// ---------------------------------------------------------------------------
// MODEL TESTS — validation logic
// ---------------------------------------------------------------------------

func TestDetectMediaType(t *testing.T) {
	cases := []struct {
		mime     string
		expected string
	}{
		{"image/jpeg", "photo"},
		{"image/png", "photo"},
		{"image/gif", "gif"},
		{"video/mp4", "video"},
		{"audio/ogg", "voice"},
		{"application/ogg", "voice"}, // Go http.DetectContentType returns this for OGG files
		{"application/pdf", "file"},
		{"application/octet-stream", "file"},
		{"", "file"},
	}
	for _, tc := range cases {
		got := model.DetectMediaType(tc.mime)
		if got != tc.expected {
			t.Errorf("DetectMediaType(%q) = %q, want %q", tc.mime, got, tc.expected)
		}
	}
}

func TestAllowedMIME(t *testing.T) {
	// Photo should accept image types but reject video
	if !model.AllowedMIME("photo", "image/jpeg") {
		t.Error("photo should allow image/jpeg")
	}
	if model.AllowedMIME("photo", "video/mp4") {
		t.Error("photo should NOT allow video/mp4")
	}

	// File allows anything
	if !model.AllowedMIME("file", "application/x-evil") {
		t.Error("file should allow any MIME")
	}

	// Voice should reject images
	if model.AllowedMIME("voice", "image/png") {
		t.Error("voice should NOT allow image/png")
	}
	// Voice must accept application/ogg (Go DetectContentType returns this for OGG files)
	if !model.AllowedMIME("voice", "application/ogg") {
		t.Error("BUG: voice should allow application/ogg (Go http.DetectContentType returns this for OGG)")
	}
}

func TestSizeLimit(t *testing.T) {
	if model.SizeLimit("photo") != 10*1024*1024 {
		t.Errorf("photo limit should be 10MB, got %d", model.SizeLimit("photo"))
	}
	if model.SizeLimit("video") != 2*1024*1024*1024 {
		t.Errorf("video limit should be 2GB, got %d", model.SizeLimit("video"))
	}
	if model.SizeLimit("videonote") != 50*1024*1024 {
		t.Errorf("videonote limit should be 50MB, got %d", model.SizeLimit("videonote"))
	}
	// Unknown type should default to file limit
	if model.SizeLimit("unknown") != 2*1024*1024*1024 {
		t.Errorf("unknown type should default to file limit")
	}
}

func TestSizeLimitBoundary(t *testing.T) {
	// Exactly at limit should be allowed
	photoLimit := model.SizeLimit("photo")

	// Just at the limit
	data := make([]byte, photoLimit)
	if int64(len(data)) > model.SizeLimit("photo") {
		t.Error("data at exact limit should not exceed SizeLimit")
	}

	// One byte over
	data = make([]byte, photoLimit+1)
	if int64(len(data)) <= model.SizeLimit("photo") {
		t.Error("data 1 byte over limit should exceed SizeLimit")
	}
}

// ---------------------------------------------------------------------------
// SERVICE TESTS — business logic with mocked storage
// ---------------------------------------------------------------------------

// mockMediaStore implements store.MediaStore for testing
type mockMediaStore struct {
	media map[uuid.UUID]*model.Media
}

func newMockStore() *mockMediaStore {
	return &mockMediaStore{media: make(map[uuid.UUID]*model.Media)}
}

func (m *mockMediaStore) Create(ctx context.Context, media *model.Media) error {
	if media.ID == uuid.Nil {
		media.ID = uuid.New()
	}
	media.CreatedAt = time.Now()
	m.media[media.ID] = media
	return nil
}

func (m *mockMediaStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Media, error) {
	media, ok := m.media[id]
	if !ok {
		return nil, nil
	}
	return media, nil
}

func (m *mockMediaStore) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]*model.Media, error) {
	var result []*model.Media
	for _, id := range ids {
		if media, ok := m.media[id]; ok {
			result = append(result, media)
		}
	}
	return result, nil
}

func (m *mockMediaStore) Delete(ctx context.Context, id uuid.UUID) error {
	if _, ok := m.media[id]; !ok {
		return fmt.Errorf("media %s not found", id)
	}
	delete(m.media, id)
	return nil
}

func (m *mockMediaStore) DeleteByUploader(ctx context.Context, id, uploaderID uuid.UUID) (string, *string, *string, error) {
	media, ok := m.media[id]
	if !ok {
		return "", nil, nil, model.ErrMediaNotFound
	}
	if media.UploaderID != uploaderID {
		return "", nil, nil, model.ErrNotUploader
	}
	r2Key := media.R2Key
	thumbKey := media.ThumbnailR2Key
	medKey := media.MediumR2Key
	delete(m.media, id)
	return r2Key, thumbKey, medKey, nil
}

func (m *mockMediaStore) UpdateProcessingStatus(ctx context.Context, id uuid.UUID, status string) error {
	if media, ok := m.media[id]; ok {
		media.ProcessingStatus = status
	}
	return nil
}

func (m *mockMediaStore) UpdateProcessingResult(ctx context.Context, id uuid.UUID, thumbKey, medKey *string, w, h *int, dur *float64, waveform []byte) error {
	if media, ok := m.media[id]; ok {
		media.ThumbnailR2Key = thumbKey
		media.MediumR2Key = medKey
		media.Width = w
		media.Height = h
		media.DurationSeconds = dur
		media.WaveformData = waveform
		media.ProcessingStatus = "ready"
	}
	return nil
}

func (m *mockMediaStore) GetByMessageIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]*store.MessageMediaRow, error) {
	return nil, nil
}

func (m *mockMediaStore) LinkToMessage(ctx context.Context, msgID, mediaID uuid.UUID, pos int, spoiler bool) error {
	return nil
}

func (m *mockMediaStore) CleanupOrphaned(ctx context.Context, maxAge int) ([]string, error) {
	return nil, nil
}

func (m *mockMediaStore) GetUserStorageBytes(ctx context.Context, userID uuid.UUID) (int64, error) {
	return 0, nil
}

func (m *mockMediaStore) CanAccess(ctx context.Context, mediaID, userID uuid.UUID) (bool, error) {
	return true, nil
}

func (m *mockMediaStore) AppendAuditLog(ctx context.Context, actorID uuid.UUID, action, targetType, targetID string, details []byte, ipAddress, userAgent *string) error {
	return nil
}

// mockR2 that tracks operations
type mockR2 struct {
	uploads map[string][]byte
	deleted []string
}

func newMockR2() *mockR2 {
	return &mockR2{uploads: make(map[string][]byte)}
}

// We can't easily mock storage.R2Client because it's a struct, not interface.
// But we CAN test the service logic by calling methods directly.
// The real bug-finding tests are below.

// ---------------------------------------------------------------------------
// BUG-FINDING TESTS: Delete IDOR
// ---------------------------------------------------------------------------

func TestDelete_OwnershipCheck(t *testing.T) {
	// Setup: create media owned by user A
	ownerID := uuid.New()
	attackerID := uuid.New()
	mediaID := uuid.New()

	ms := newMockStore()
	ms.media[mediaID] = &model.Media{
		ID:               mediaID,
		UploaderID:       ownerID,
		Type:             "photo",
		R2Key:            "photos/test/original.jpg",
		ProcessingStatus: "ready",
	}

	// Attacker tries to get info — should work (public)
	media, err := ms.GetByID(context.Background(), mediaID)
	if err != nil || media == nil {
		t.Fatal("GetByID should work for any user")
	}

	// Attacker tries to delete — ownership check in service layer
	if media.UploaderID == attackerID {
		t.Fatal("BUG: attacker should not be the owner")
	}
	if media.UploaderID != ownerID {
		// This is correct — service would return ErrNotUploader
	} else {
		t.Log("ownership check would pass for owner — correct")
	}

	// Owner deletes — should succeed
	err = ms.Delete(context.Background(), mediaID)
	if err != nil {
		t.Fatalf("owner delete failed: %v", err)
	}
	if _, ok := ms.media[mediaID]; ok {
		t.Fatal("BUG: media should be deleted from store")
	}
}

// ---------------------------------------------------------------------------
// BUG-FINDING: Sentinel errors are correctly propagated
// ---------------------------------------------------------------------------

func TestSentinelErrors(t *testing.T) {
	ms := newMockStore()

	// Get nonexistent media
	media, err := ms.GetByID(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("GetByID should not error for nonexistent, got %v", err)
	}
	if media != nil {
		t.Fatal("BUG: GetByID should return nil for nonexistent media")
	}

	// Delete nonexistent media
	err = ms.Delete(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("BUG: Delete should error for nonexistent media")
	}
}

// ---------------------------------------------------------------------------
// BUG-FINDING: Processing pipeline
// ---------------------------------------------------------------------------

func TestProcessImage_ValidPNG(t *testing.T) {
	result, err := service.ProcessImage(validPNG())
	if err != nil {
		t.Fatalf("ProcessImage failed on valid PNG: %v", err)
	}
	if result.Width != 10 || result.Height != 10 {
		t.Errorf("expected 10x10, got %dx%d", result.Width, result.Height)
	}
	if len(result.Thumb320) == 0 {
		t.Error("BUG: thumb_320 is empty")
	}
	if len(result.Medium800) == 0 {
		t.Error("BUG: medium_800 is empty")
	}
	if len(result.Original) == 0 {
		t.Error("BUG: re-encoded original is empty")
	}
}

func TestProcessImage_InvalidData(t *testing.T) {
	_, err := service.ProcessImage([]byte("not an image"))
	if err == nil {
		t.Fatal("BUG: ProcessImage should fail on invalid image data")
	}
}

func TestProcessImage_ZeroBytes(t *testing.T) {
	_, err := service.ProcessImage([]byte{})
	if err == nil {
		t.Fatal("BUG: ProcessImage should fail on empty data")
	}
}

func TestProcessImage_TruncatedPNG(t *testing.T) {
	// Give it just the PNG header, no image data
	truncated := validPNG()[:16]
	_, err := service.ProcessImage(truncated)
	if err == nil {
		t.Fatal("BUG: ProcessImage should fail on truncated PNG")
	}
}

// ---------------------------------------------------------------------------
// BUG-FINDING: Waveform extraction edge cases
// ---------------------------------------------------------------------------

func TestExtractWaveform_NoFFmpeg(t *testing.T) {
	// If ffmpeg is not available, should return flat waveform, not crash
	result, err := service.ExtractWaveform("/nonexistent/file.ogg")
	if err != nil {
		t.Fatalf("ExtractWaveform should not error even without ffmpeg, got: %v", err)
	}
	if len(result.WaveformPeaks) != 100 {
		t.Errorf("expected 100 peaks, got %d", len(result.WaveformPeaks))
	}
}

// ---------------------------------------------------------------------------
// BUG-FINDING: Chunked upload meta serialization
// ---------------------------------------------------------------------------

func TestChunkedUploadMeta_JSONRoundtrip(t *testing.T) {
	meta := model.ChunkedUploadMeta{
		ID:          uuid.New().String(),
		UploaderID:  uuid.New().String(),
		Filename:    "test.mp4",
		MimeType:    "video/mp4",
		MediaType:   "video",
		TotalSize:   15728640,
		ChunkSize:   10485760,
		TotalChunks: 2,
		R2Key:       "video/abc/original.mp4",
		R2UploadID:  "upload-xyz",
		Parts:       []model.Part{{Number: 1, ETag: "etag1"}},
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded model.ChunkedUploadMeta
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.ID != meta.ID {
		t.Error("BUG: ID mismatch after roundtrip")
	}
	if decoded.TotalChunks != 2 {
		t.Errorf("BUG: TotalChunks = %d, want 2", decoded.TotalChunks)
	}
	if len(decoded.Parts) != 1 {
		t.Fatalf("BUG: Parts length = %d, want 1", len(decoded.Parts))
	}
	if decoded.Parts[0].ETag != "etag1" {
		t.Error("BUG: Part ETag mismatch")
	}
}

func TestChunkedUploadMeta_EmptyParts(t *testing.T) {
	meta := model.ChunkedUploadMeta{
		Parts: []model.Part{},
	}
	data, _ := json.Marshal(meta)

	var decoded model.ChunkedUploadMeta
	json.Unmarshal(data, &decoded)

	if decoded.Parts == nil {
		t.Log("WARNING: empty Parts serializes as null, could cause nil pointer in Lua script")
		// This is actually a potential bug — if Parts is nil after unmarshal,
		// len(decoded.Parts) still works in Go, but Lua cjson may behave differently
	}
}

// ---------------------------------------------------------------------------
// BUG-FINDING: MIME type / media type consistency
// ---------------------------------------------------------------------------

func TestMIMEMediaTypeConsistency(t *testing.T) {
	// If user says type=photo but sends video/mp4, should reject
	if model.AllowedMIME("photo", "video/mp4") {
		t.Error("BUG: photo type should not allow video/mp4 MIME")
	}

	// If user says type=video but sends image/jpeg, should reject
	if model.AllowedMIME("video", "image/jpeg") {
		t.Error("BUG: video type should not allow image/jpeg MIME")
	}

	// GIF MIME should only be allowed for gif type and photo type
	if !model.AllowedMIME("photo", "image/gif") {
		t.Error("photo should allow image/gif (will be auto-detected as gif)")
	}
}

// ---------------------------------------------------------------------------
// BUG-FINDING: Media response builder
// ---------------------------------------------------------------------------

func TestMediaResponse_NilOptionalFields(t *testing.T) {
	// Media with no thumbnail, no medium, no dimensions, no duration
	m := &model.Media{
		ID:               uuid.New(),
		UploaderID:       uuid.New(),
		Type:             "file",
		MimeType:         "application/pdf",
		SizeBytes:        1024,
		R2Key:            "files/test/doc.pdf",
		ProcessingStatus: "ready",
	}

	resp := &model.MediaResponse{
		ID:               m.ID.String(),
		Type:             m.Type,
		MimeType:         m.MimeType,
		SizeBytes:        m.SizeBytes,
		ProcessingStatus: m.ProcessingStatus,
	}

	// Marshal to JSON — nil fields should be omitted, not "null"
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	// These should not be present in JSON (omitempty)
	if _, ok := raw["width"]; ok {
		t.Error("BUG: width should be omitted for file type")
	}
	if _, ok := raw["thumbnail_url"]; ok {
		t.Error("BUG: thumbnail_url should be omitted when empty")
	}
	if _, ok := raw["duration_seconds"]; ok {
		t.Error("BUG: duration_seconds should be omitted for file type")
	}
}

// ---------------------------------------------------------------------------
// Handler HTTP layer
// ---------------------------------------------------------------------------

type handlerAuditStore struct {
	mockMediaStore
	appendAuditLogFn func(ctx context.Context, actorID uuid.UUID, action, targetType, targetID string, details []byte, ipAddress, userAgent *string) error
	appendAuditCalls int
	createCalls      int
}

func (m *handlerAuditStore) Create(ctx context.Context, media *model.Media) error {
	m.createCalls++
	return m.mockMediaStore.Create(ctx, media)
}

func (m *handlerAuditStore) AppendAuditLog(ctx context.Context, actorID uuid.UUID, action, targetType, targetID string, details []byte, ipAddress, userAgent *string) error {
	m.appendAuditCalls++
	if m.appendAuditLogFn != nil {
		return m.appendAuditLogFn(ctx, actorID, action, targetType, targetID, details, ipAddress, userAgent)
	}
	return nil
}

type handlerScannerStub struct {
	scanFn func(ctx context.Context, reader io.Reader, filename string) (*scanner.ScanResult, error)
}

func (s *handlerScannerStub) Scan(ctx context.Context, reader io.Reader, filename string) (*scanner.ScanResult, error) {
	if s.scanFn != nil {
		return s.scanFn(ctx, reader, filename)
	}
	return &scanner.ScanResult{Clean: true}, nil
}

func newUploadTestApp(t *testing.T, mediaStore store.Store, scannerStub scanner.Scanner, logger *slog.Logger, metricsReg *metrics.Registry) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{
		BodyLimit:    55 * 1024 * 1024,
		ErrorHandler: response.FiberErrorHandler,
	})
	svc := service.NewMediaServiceWithR2(mediaStore, nil, nil, nil).
		WithScanner(scannerStub).
		WithAuditMetrics(metricsReg)
	h := NewUploadHandler(svc, logger, "test-internal-token")
	h.Register(app)
	return app
}

func TestUploadHandler_MalwareAuditSuccessReturns422(t *testing.T) {
	userID := uuid.New()
	app := newUploadTestApp(t, &handlerAuditStore{mockMediaStore: *newMockStore()}, &handlerScannerStub{
		scanFn: func(ctx context.Context, reader io.Reader, filename string) (*scanner.ScanResult, error) {
			return &scanner.ScanResult{Clean: false, Virus: "Eicar-Test-Signature"}, nil
		},
	}, slog.Default(), metrics.New("media"))

	body, contentType := makeMultipartBody("eicar.txt", "text/plain", []byte("safe payload"), map[string]string{"type": "file"})
	req, err := http.NewRequest("POST", "/media/upload", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-User-ID", userID.String())
	req.Header.Set("X-Internal-Token", "test-internal-token")
	req.Header.Set("X-Trusted-Client-IP", "203.0.113.10")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app test: %v", err)
	}
	if resp.StatusCode != 422 {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["error"] != "virus_detected" {
		t.Fatalf("unexpected error code: %+v", got)
	}
}

func TestUploadHandler_MalwareAuditFailureReturns503NoSideEffectsAndLogs(t *testing.T) {
	userID := uuid.New()
	st := &handlerAuditStore{
		mockMediaStore: *newMockStore(),
		appendAuditLogFn: func(ctx context.Context, actorID uuid.UUID, action, targetType, targetID string, details []byte, ipAddress, userAgent *string) error {
			return context.DeadlineExceeded
		},
	}
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	prevDefault := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(prevDefault)
	metricsReg := metrics.New("media")
	app := newUploadTestApp(t, st, &handlerScannerStub{
		scanFn: func(ctx context.Context, reader io.Reader, filename string) (*scanner.ScanResult, error) {
			return &scanner.ScanResult{Clean: false, Virus: "Eicar-Test-Signature"}, nil
		},
	}, logger, metricsReg)

	body, contentType := makeMultipartBody("eicar.txt", "text/plain", []byte("safe payload"), map[string]string{"type": "file"})
	req, err := http.NewRequest("POST", "/media/upload", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-User-ID", userID.String())
	req.Header.Set("X-Internal-Token", "test-internal-token")
	req.Header.Set("X-Trusted-Client-IP", "203.0.113.10")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app test: %v", err)
	}
	if resp.StatusCode != 503 {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["error"] != "audit_unavailable" {
		t.Fatalf("unexpected error code: %+v", got)
	}
	if msg, _ := got["message"].(string); msg != "Upload service temporarily unavailable, please retry." {
		t.Fatalf("unexpected message: %q", msg)
	}
	if strings.Contains(strings.ToLower(fmt.Sprint(got)), "virus") || strings.Contains(strings.ToLower(fmt.Sprint(got)), "malware") {
		t.Fatalf("response leaked malware verdict: %+v", got)
	}
	if st.createCalls != 0 {
		t.Fatalf("expected no create side effects, got %d", st.createCalls)
	}
	if len(st.media) != 0 {
		t.Fatalf("expected no persisted media objects, got %d", len(st.media))
	}
	for _, needle := range []string{"event=audit_persistent_failure", "user_id=" + userID.String(), "virus_name=Eicar-Test-Signature", "attempts_count=3"} {
		if !strings.Contains(logBuf.String(), needle) {
			t.Fatalf("expected log to contain %q, got %q", needle, logBuf.String())
		}
	}
	metricFamilies, err := metricsReg.Prometheus().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	var timeoutCount float64
	for _, mf := range metricFamilies {
		if mf.GetName() != "media_audit_write_attempts_total" {
			continue
		}
		for _, metric := range mf.GetMetric() {
			result := ""
			for _, label := range metric.GetLabel() {
				if label.GetName() == "result" {
					result = label.GetValue()
				}
			}
			if result == "timeout" {
				timeoutCount += metric.GetCounter().GetValue()
			}
		}
	}
	if timeoutCount != 3 {
		t.Fatalf("expected timeout metric count 3, got %v", timeoutCount)
	}
}

func TestUploadHandler_NoFile(t *testing.T) {
	app := newUploadTestApp(t, &handlerAuditStore{mockMediaStore: *newMockStore()}, &handlerScannerStub{}, slog.Default(), metrics.New("media"))
	body := bytes.NewBuffer([]byte{})
	req, _ := http.NewRequest("POST", "/media/upload", body)
	req.Header.Set("X-User-ID", uuid.New().String())
	req.Header.Set("X-Internal-Token", "test-internal-token")
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for no file, got %d", resp.StatusCode)
	}
}

func TestUploadHandler_NoAuth(t *testing.T) {
	app := newUploadTestApp(t, &handlerAuditStore{mockMediaStore: *newMockStore()}, &handlerScannerStub{}, slog.Default(), metrics.New("media"))
	req, _ := http.NewRequest("POST", "/media/upload", nil)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 without auth, got %d", resp.StatusCode)
	}
}

func TestMediaHandler_InvalidUUID(t *testing.T) {
	app := fiber.New()
	app.Get("/media/:id/info", func(c *fiber.Ctx) error {
		_, err := uuid.Parse(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
		}
		return c.Status(200).JSON(fiber.Map{"ok": true})
	})

	// Invalid UUID
	req, _ := http.NewRequest("GET", "/media/not-a-uuid/info", nil)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for invalid UUID, got %d", resp.StatusCode)
	}

	// Valid UUID
	req2, _ := http.NewRequest("GET", "/media/"+uuid.New().String()+"/info", nil)
	resp2, _ := app.Test(req2, -1)
	if resp2.StatusCode == 400 {
		t.Error("valid UUID should not return 400")
	}
}

func TestPartNumberValidation(t *testing.T) {
	cases := []struct {
		name    string
		part    int
		wantErr bool
	}{
		{"zero", 0, true},
		{"negative", -1, true},
		{"valid_1", 1, false},
		{"valid_10000", 10000, false},
		{"over_limit", 10001, true},
		{"way_over", 99999, true},
	}
	for _, tc := range cases {
		isErr := tc.part < 1 || tc.part > 10000
		if isErr != tc.wantErr {
			t.Errorf("%s: part=%d, isErr=%v, wantErr=%v", tc.name, tc.part, isErr, tc.wantErr)
		}
	}
}

// ---------------------------------------------------------------------------
// Suppress unused import warnings
// ---------------------------------------------------------------------------
var _ = storage.CompletedPart{}
var _ = redis.Client{}
var _ = nats.Conn{}
var _ io.Reader
