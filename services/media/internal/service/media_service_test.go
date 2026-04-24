// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/metrics"
	"github.com/mst-corp/orbit/services/media/internal/model"
	"github.com/mst-corp/orbit/services/media/internal/scanner"
	"github.com/mst-corp/orbit/services/media/internal/store"
)

type quotaStore struct {
	userStorageBytes int64
}

func (q *quotaStore) Create(ctx context.Context, m *model.Media) error { return nil }
func (q *quotaStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Media, error) {
	return nil, nil
}
func (q *quotaStore) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]*model.Media, error) {
	return nil, nil
}
func (q *quotaStore) Delete(ctx context.Context, id uuid.UUID) error { return nil }
func (q *quotaStore) DeleteByUploader(ctx context.Context, id, uploaderID uuid.UUID) (string, *string, *string, error) {
	return "", nil, nil, nil
}
func (q *quotaStore) UpdateProcessingStatus(ctx context.Context, id uuid.UUID, status string) error {
	return nil
}
func (q *quotaStore) UpdateProcessingResult(ctx context.Context, id uuid.UUID, thumbnailKey, mediumKey *string, width, height *int, duration *float64, waveform []byte) error {
	return nil
}
func (q *quotaStore) GetByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]*store.MessageMediaRow, error) {
	return nil, nil
}
func (q *quotaStore) LinkToMessage(ctx context.Context, messageID, mediaID uuid.UUID, position int, isSpoiler bool) error {
	return nil
}
func (q *quotaStore) CleanupOrphaned(ctx context.Context, maxAgeHours int) ([]string, error) {
	return nil, nil
}
func (q *quotaStore) GetUserStorageBytes(ctx context.Context, userID uuid.UUID) (int64, error) {
	return q.userStorageBytes, nil
}
func (q *quotaStore) CanAccess(ctx context.Context, mediaID, userID uuid.UUID) (bool, error) {
	return true, nil
}
func (q *quotaStore) AppendAuditLog(ctx context.Context, actorID uuid.UUID, action, targetType, targetID string, details []byte, ipAddress, userAgent *string) error {
	return nil
}

type presignStore struct {
	quotaStore
	media        map[uuid.UUID]*model.Media
	canAccess    bool
	canAccessErr error
}

func (p *presignStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Media, error) {
	if p.media == nil {
		return nil, nil
	}
	return p.media[id], nil
}

func (p *presignStore) CanAccess(ctx context.Context, mediaID, userID uuid.UUID) (bool, error) {
	if p.canAccessErr != nil {
		return false, p.canAccessErr
	}
	return p.canAccess, nil
}

func TestEnsureUserStorageAvailable_DisabledByDefault(t *testing.T) {
	svc := NewMediaService(&quotaStore{userStorageBytes: 10 * 1024 * 1024}, nil, nil, nil).WithMaxUserStorageBytes(0)
	if err := svc.ensureUserStorageAvailable(context.Background(), uuid.New(), 50*1024*1024); err != nil {
		t.Fatalf("expected unlimited storage when quota disabled, got %v", err)
	}
}

func TestUpload_OverQuotaRejected(t *testing.T) {
	svc := NewMediaService(&quotaStore{userStorageBytes: 0}, nil, nil, nil).WithMaxUserStorageBytes(1024 * 1024)

	_, err := svc.Upload(context.Background(), uuid.New(), make([]byte, 2*1024*1024), "big.jpg", "image/jpeg", "photo", false, nil)
	appErr, ok := err.(*apperror.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T (%v)", err, err)
	}
	if appErr.Status != 429 {
		t.Fatalf("expected 429, got %d", appErr.Status)
	}
}

func TestInitChunkedUpload_OverQuotaRejected(t *testing.T) {
	svc := NewMediaService(&quotaStore{userStorageBytes: 512 * 1024}, nil, nil, nil).WithMaxUserStorageBytes(1024 * 1024)

	_, err := svc.InitChunkedUpload(context.Background(), uuid.New(), "video.mp4", "video/mp4", "video", 1024*1024)
	appErr, ok := err.(*apperror.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %T (%v)", err, err)
	}
	if appErr.Status != 429 {
		t.Fatalf("expected 429, got %d", appErr.Status)
	}
}

func TestIsAllowedChunkedMIME_RejectsUnknownMagicDeclaredAsImage(t *testing.T) {
	if isAllowedChunkedMIME("photo", "image/jpeg", "application/octet-stream") {
		t.Fatal("expected unknown-magic image declaration to be rejected")
	}
}

func TestIsAllowedChunkedMIME_FileAllowsOctetStreamFallback(t *testing.T) {
	if !isAllowedChunkedMIME("file", "application/octet-stream", "application/octet-stream") {
		t.Fatal("expected generic file octet-stream to be allowed")
	}
}

func TestGetPresignedURL_OwnerGetsURL(t *testing.T) {
	ownerID := uuid.New()
	mediaID := uuid.New()
	st := &presignStore{
		canAccess: true,
		media: map[uuid.UUID]*model.Media{
			mediaID: {
				ID:             mediaID,
				UploaderID:     ownerID,
				R2Key:          "media/original.jpg",
				ThumbnailR2Key: strPtr("media/thumb.jpg"),
				MediumR2Key:    strPtr("media/medium.jpg"),
			},
		},
	}
	svc := NewMediaService(st, nil, nil, nil)
	svc.presignGetURL = func(ctx context.Context, key string, ttl time.Duration) (string, error) {
		return "https://example.test/" + key, nil
	}

	url, err := svc.GetPresignedURL(context.Background(), mediaID, ownerID)
	if err != nil {
		t.Fatalf("expected presigned URL, got error: %v", err)
	}
	if url != "https://example.test/media/original.jpg" {
		t.Fatalf("unexpected URL: %q", url)
	}
}

func TestPresignedURLs_NonOwnerGetsMediaNotFound(t *testing.T) {
	ownerID := uuid.New()
	otherUserID := uuid.New()
	mediaID := uuid.New()
	st := &presignStore{
		canAccess: false,
		media: map[uuid.UUID]*model.Media{
			mediaID: {
				ID:             mediaID,
				UploaderID:     ownerID,
				R2Key:          "media/original.jpg",
				ThumbnailR2Key: strPtr("media/thumb.jpg"),
				MediumR2Key:    strPtr("media/medium.jpg"),
			},
		},
	}
	svc := NewMediaService(st, nil, nil, nil)
	svc.presignGetURL = func(ctx context.Context, key string, ttl time.Duration) (string, error) {
		t.Fatal("presign must not run when access is denied")
		return "", nil
	}

	tests := []struct {
		name string
		fn   func(context.Context, uuid.UUID, uuid.UUID) (string, error)
	}{
		{name: "original", fn: svc.GetPresignedURL},
		{name: "thumbnail", fn: svc.GetThumbnailURL},
		{name: "medium", fn: svc.GetMediumURL},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.fn(context.Background(), mediaID, otherUserID)
			if err != model.ErrMediaNotFound {
				t.Fatalf("expected ErrMediaNotFound, got %v", err)
			}
		})
	}
}

type auditStore struct {
	quotaStore
	appendAuditLogFn func(ctx context.Context, actorID uuid.UUID, action, targetType, targetID string, details []byte, ipAddress, userAgent *string) error
	appendAuditCalls int
	createCalls      int
}

type scannerStub struct {
	scanFn func(ctx context.Context, reader io.Reader, filename string) (*scanner.ScanResult, error)
}

func (s *scannerStub) Scan(ctx context.Context, reader io.Reader, filename string) (*scanner.ScanResult, error) {
	if s.scanFn != nil {
		return s.scanFn(ctx, reader, filename)
	}
	return &scanner.ScanResult{Clean: true}, nil
}

func (a *auditStore) Create(ctx context.Context, m *model.Media) error {
	a.createCalls++
	return nil
}

func (a *auditStore) AppendAuditLog(ctx context.Context, actorID uuid.UUID, action, targetType, targetID string, details []byte, ipAddress, userAgent *string) error {
	a.appendAuditCalls++
	if a.appendAuditLogFn != nil {
		return a.appendAuditLogFn(ctx, actorID, action, targetType, targetID, details, ipAddress, userAgent)
	}
	return nil
}

func TestRecordVirusDetectionAudit_WritesAuditRow(t *testing.T) {
	userID := uuid.New()
	var gotTargetID string
	var gotDetails []byte
	var gotIP, gotUserAgent *string

	svc := NewMediaService(&auditStore{
		appendAuditLogFn: func(_ context.Context, actorID uuid.UUID, action, targetType, targetID string, details []byte, ipAddress, userAgent *string) error {
			if actorID != userID {
				t.Fatalf("unexpected actor id: %s", actorID)
			}
			if action != model.AuditActionVirusDetected {
				t.Fatalf("unexpected action: %s", action)
			}
			if targetType != model.AuditTargetTypeUpload {
				t.Fatalf("unexpected target type: %s", targetType)
			}
			gotTargetID = targetID
			gotDetails = append([]byte(nil), details...)
			gotIP = ipAddress
			gotUserAgent = userAgent
			return nil
		},
	}, nil, nil, nil)

	err := svc.recordVirusDetectionAudit(&model.UploadAuditContext{
		UserID:          userID,
		TrustedClientIP: "203.0.113.10",
		UserAgent:       "OrbitSmoke/1.0",
		Filename:        "eicar.txt",
		MimeType:        "text/plain",
		Size:            68,
		UploadAttemptID: "attempt-123",
	}, uuid.New(), "Eicar-Signature")
	if err != nil {
		t.Fatalf("recordVirusDetectionAudit: %v", err)
	}
	if gotTargetID != "attempt-123" {
		t.Fatalf("unexpected target id: %s", gotTargetID)
	}
	if gotIP == nil || *gotIP != "203.0.113.10" {
		t.Fatalf("unexpected ip: %+v", gotIP)
	}
	if gotUserAgent == nil || *gotUserAgent != "OrbitSmoke/1.0" {
		t.Fatalf("unexpected user agent: %+v", gotUserAgent)
	}

	var details map[string]any
	if err := json.Unmarshal(gotDetails, &details); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}
	if details["upload_attempt_id"] != "attempt-123" {
		t.Fatalf("unexpected details: %+v", details)
	}
	if details["clamav_result"] != "Eicar-Signature" {
		t.Fatalf("unexpected clamav result: %+v", details)
	}
}

func TestRecordVirusDetectionAudit_StoreFailure(t *testing.T) {
	svc := NewMediaService(&auditStore{
		appendAuditLogFn: func(_ context.Context, actorID uuid.UUID, action, targetType, targetID string, details []byte, ipAddress, userAgent *string) error {
			return context.DeadlineExceeded
		},
	}, nil, nil, nil)

	err := svc.recordVirusDetectionAudit(&model.UploadAuditContext{
		UserID:          uuid.New(),
		UploadAttemptID: "attempt-err",
	}, uuid.New(), "Virus")
	if err == nil {
		t.Fatal("expected error when audit write fails")
	}
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Code != "audit_unavailable" {
		t.Fatalf("unexpected app error code: %s", appErr.Code)
	}
	if appErr.Status != 503 {
		t.Fatalf("unexpected status: %d", appErr.Status)
	}
}

func TestUpload_VirusDetectedAuditSuccessReturns422(t *testing.T) {
	userID := uuid.New()
	st := &auditStore{}
	svc := NewMediaService(st, nil, nil, nil).WithScanner(&scannerStub{
		scanFn: func(ctx context.Context, reader io.Reader, filename string) (*scanner.ScanResult, error) {
			return &scanner.ScanResult{Clean: false, Virus: "Eicar-Test-Signature"}, nil
		},
	})

	_, err := svc.Upload(context.Background(), userID, []byte("safe payload"), "eicar.txt", "text/plain", model.MediaTypeFile, false, &model.UploadAuditContext{
		UserID:          userID,
		TrustedClientIP: "203.0.113.10",
		UserAgent:       "OrbitSmoke/1.0",
		Filename:        "eicar.txt",
		MimeType:        "text/plain",
		Size:            int64(len([]byte("safe payload"))),
		UploadAttemptID: "attempt-malware",
	})
	if err == nil {
		t.Fatal("expected malware rejection")
	}
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Status != 422 {
		t.Fatalf("expected 422, got %d", appErr.Status)
	}
	if appErr.Code != "virus_detected" {
		t.Fatalf("unexpected error code: %s", appErr.Code)
	}
	if st.appendAuditCalls != 1 {
		t.Fatalf("expected 1 audit append call, got %d", st.appendAuditCalls)
	}
	if st.createCalls != 0 {
		t.Fatalf("expected no media create side effects, got %d", st.createCalls)
	}
}

func TestUpload_VirusDetectedAuditFailureReturns503AndLogs(t *testing.T) {
	userID := uuid.New()
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	metricsReg := metrics.New("media")
	st := &auditStore{
		appendAuditLogFn: func(_ context.Context, actorID uuid.UUID, action, targetType, targetID string, details []byte, ipAddress, userAgent *string) error {
			return context.DeadlineExceeded
		},
	}

	svc := NewMediaService(st, nil, nil, nil).
		WithAuditMetrics(metricsReg).
		WithScanner(&scannerStub{
			scanFn: func(ctx context.Context, reader io.Reader, filename string) (*scanner.ScanResult, error) {
				return &scanner.ScanResult{Clean: false, Virus: "Eicar-Test-Signature"}, nil
			},
		})

	prevDefault := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(prevDefault)

	_, err := svc.Upload(context.Background(), userID, []byte("safe payload"), "eicar.txt", "text/plain", model.MediaTypeFile, false, &model.UploadAuditContext{
		UserID:          userID,
		TrustedClientIP: "203.0.113.10",
		UserAgent:       "OrbitSmoke/1.0",
		Filename:        "eicar.txt",
		MimeType:        "text/plain",
		Size:            int64(len([]byte("safe payload"))),
		UploadAttemptID: "attempt-malware",
	})
	if err == nil {
		t.Fatal("expected malware rejection")
	}
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T", err)
	}
	if appErr.Status != 503 {
		t.Fatalf("expected 503, got %d", appErr.Status)
	}
	if appErr.Code != "audit_unavailable" {
		t.Fatalf("unexpected error code: %s", appErr.Code)
	}
	if appErr.Message != "Upload service temporarily unavailable, please retry." {
		t.Fatalf("unexpected error message: %q", appErr.Message)
	}
	if st.appendAuditCalls != 3 {
		t.Fatalf("expected 3 audit attempts, got %d", st.appendAuditCalls)
	}
	if st.createCalls != 0 {
		t.Fatalf("expected no media create side effects, got %d", st.createCalls)
	}
	logs := logBuf.String()
	for _, needle := range []string{"event=audit_persistent_failure", "user_id=" + userID.String(), "virus_name=Eicar-Test-Signature", "attempts_count=3"} {
		if !strings.Contains(logs, needle) {
			t.Fatalf("expected log to contain %q, got %q", needle, logs)
		}
	}
	metricFamilies, err := metricsReg.Prometheus().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	var transientCount, timeoutCount float64
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
			switch result {
			case "timeout":
				timeoutCount += metric.GetCounter().GetValue()
			case "transient_error":
				transientCount += metric.GetCounter().GetValue()
			}
		}
	}
	if timeoutCount != 3 {
		t.Fatalf("expected timeout metric count 3, got %v", timeoutCount)
	}
	if transientCount != 0 {
		t.Fatalf("expected transient_error metric count 0, got %v", transientCount)
	}
}

func TestPresignedURLs_NilUserIDGetsError(t *testing.T) {
	mediaID := uuid.New()
	st := &presignStore{canAccess: true}
	svc := NewMediaService(st, nil, nil, nil)
	svc.presignGetURL = func(ctx context.Context, key string, ttl time.Duration) (string, error) {
		t.Fatal("presign must not run for nil user ID")
		return "", nil
	}

	tests := []struct {
		name string
		fn   func(context.Context, uuid.UUID, uuid.UUID) (string, error)
	}{
		{name: "original", fn: svc.GetPresignedURL},
		{name: "thumbnail", fn: svc.GetThumbnailURL},
		{name: "medium", fn: svc.GetMediumURL},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.fn(context.Background(), mediaID, uuid.Nil)
			if err == nil {
				t.Fatal("expected error for nil user ID")
			}
		})
	}
}
