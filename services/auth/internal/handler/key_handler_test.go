package handler

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/auth/internal/model"
	"github.com/mst-corp/orbit/services/auth/internal/service"
)

type mockKeyStore struct {
	upsertFn          func(ctx context.Context, keys *model.UserDeviceKeys) error
	getByUserDeviceFn func(ctx context.Context, userID, deviceID uuid.UUID) (*model.UserDeviceKeys, error)
	listByUserFn      func(ctx context.Context, userID uuid.UUID) ([]model.UserDeviceKeys, error)
	deleteByDeviceFn  func(ctx context.Context, userID, deviceID uuid.UUID) error
	getIdentityKeyFn  func(ctx context.Context, userID uuid.UUID) ([]byte, error)
}

func (m *mockKeyStore) Upsert(ctx context.Context, keys *model.UserDeviceKeys) error {
	if m.upsertFn != nil {
		return m.upsertFn(ctx, keys)
	}
	return nil
}

func (m *mockKeyStore) GetByUserAndDevice(ctx context.Context, userID, deviceID uuid.UUID) (*model.UserDeviceKeys, error) {
	if m.getByUserDeviceFn != nil {
		return m.getByUserDeviceFn(ctx, userID, deviceID)
	}
	return nil, nil
}

func (m *mockKeyStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]model.UserDeviceKeys, error) {
	if m.listByUserFn != nil {
		return m.listByUserFn(ctx, userID)
	}
	return nil, nil
}

func (m *mockKeyStore) DeleteByDevice(ctx context.Context, userID, deviceID uuid.UUID) error {
	if m.deleteByDeviceFn != nil {
		return m.deleteByDeviceFn(ctx, userID, deviceID)
	}
	return nil
}

func (m *mockKeyStore) GetIdentityKey(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	if m.getIdentityKeyFn != nil {
		return m.getIdentityKeyFn(ctx, userID)
	}
	return nil, nil
}

type mockPreKeyStore struct {
	uploadBatchFn    func(ctx context.Context, userID, deviceID uuid.UUID, keys []model.OneTimePreKey) (int, error)
	consumeOneFn     func(ctx context.Context, userID uuid.UUID) (*model.OneTimePreKey, error)
	countRemainingFn func(ctx context.Context, userID uuid.UUID) (int, error)
	deleteByDeviceFn func(ctx context.Context, userID, deviceID uuid.UUID) error
}

func (m *mockPreKeyStore) UploadBatch(ctx context.Context, userID, deviceID uuid.UUID, keys []model.OneTimePreKey) (int, error) {
	if m.uploadBatchFn != nil {
		return m.uploadBatchFn(ctx, userID, deviceID, keys)
	}
	return 0, nil
}

func (m *mockPreKeyStore) ConsumeOne(ctx context.Context, userID uuid.UUID) (*model.OneTimePreKey, error) {
	if m.consumeOneFn != nil {
		return m.consumeOneFn(ctx, userID)
	}
	return nil, nil
}

func (m *mockPreKeyStore) CountRemaining(ctx context.Context, userID uuid.UUID) (int, error) {
	if m.countRemainingFn != nil {
		return m.countRemainingFn(ctx, userID)
	}
	return 0, nil
}

func (m *mockPreKeyStore) DeleteByDevice(ctx context.Context, userID, deviceID uuid.UUID) error {
	if m.deleteByDeviceFn != nil {
		return m.deleteByDeviceFn(ctx, userID, deviceID)
	}
	return nil
}

type mockTransparencyStore struct {
	appendFn     func(ctx context.Context, entry *model.KeyTransparencyEntry) error
	listByUserFn func(ctx context.Context, userID uuid.UUID, limit int) ([]model.KeyTransparencyEntry, error)
}

func (m *mockTransparencyStore) Append(ctx context.Context, entry *model.KeyTransparencyEntry) error {
	if m.appendFn != nil {
		return m.appendFn(ctx, entry)
	}
	return nil
}

func (m *mockTransparencyStore) ListByUser(ctx context.Context, userID uuid.UUID, limit int) ([]model.KeyTransparencyEntry, error) {
	if m.listByUserFn != nil {
		return m.listByUserFn(ctx, userID, limit)
	}
	return nil, nil
}

func setupKeyTestApp(t *testing.T) (*fiber.App, *mockKeyStore, *mockPreKeyStore, *mockTransparencyStore) {
	t.Helper()

	keyStore := &mockKeyStore{}
	preKeyStore := &mockPreKeyStore{}
	transparencyStore := &mockTransparencyStore{}

	keySvc := service.NewKeyService(keyStore, preKeyStore, transparencyStore)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	keyHandler := NewKeyHandler(keySvc, logger)

	app := fiber.New(fiber.Config{
		ErrorHandler: response.FiberErrorHandler,
	})
	keyHandler.Register(app)

	return app, keyStore, preKeyStore, transparencyStore
}

func encodeKey(size int, b byte) string {
	raw := make([]byte, size)
	for i := range raw {
		raw[i] = b
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestRegisterDeviceKeys_Success(t *testing.T) {
	app, keyStore, _, transparencyStore := setupKeyTestApp(t)
	called := false
	keyStore.upsertFn = func(ctx context.Context, keys *model.UserDeviceKeys) error {
		called = true
		return nil
	}
	transparencyStore.appendFn = func(ctx context.Context, entry *model.KeyTransparencyEntry) error {
		return nil
	}

	resp := doRequest(app, http.MethodPost, "/keys/identity", map[string]interface{}{
		"identity_key":            encodeKey(32, 1),
		"signed_prekey":           encodeKey(32, 2),
		"signed_prekey_signature": encodeKey(64, 3),
		"signed_prekey_id":        7,
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-Device-ID": uuid.New().String(),
	})

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if !called {
		t.Fatal("expected key store upsert to be called")
	}
}

func TestRegisterDeviceKeys_MissingUserID(t *testing.T) {
	app, _, _, _ := setupKeyTestApp(t)

	resp := doRequest(app, http.MethodPost, "/keys/identity", map[string]interface{}{
		"identity_key":            encodeKey(32, 1),
		"signed_prekey":           encodeKey(32, 2),
		"signed_prekey_signature": encodeKey(64, 3),
		"signed_prekey_id":        7,
	}, map[string]string{
		"X-Device-ID": uuid.New().String(),
	})

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestRegisterDeviceKeys_InvalidKeySize(t *testing.T) {
	app, _, _, _ := setupKeyTestApp(t)

	resp := doRequest(app, http.MethodPost, "/keys/identity", map[string]interface{}{
		"identity_key":            encodeKey(31, 1),
		"signed_prekey":           encodeKey(32, 2),
		"signed_prekey_signature": encodeKey(64, 3),
		"signed_prekey_id":        7,
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-Device-ID": uuid.New().String(),
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRegisterDeviceKeys_MissingDeviceID(t *testing.T) {
	app, _, _, _ := setupKeyTestApp(t)

	resp := doRequest(app, http.MethodPost, "/keys/identity", map[string]interface{}{
		"identity_key":            encodeKey(32, 1),
		"signed_prekey":           encodeKey(32, 2),
		"signed_prekey_signature": encodeKey(64, 3),
		"signed_prekey_id":        7,
	}, map[string]string{
		"X-User-ID": uuid.New().String(),
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRotateSignedPreKey_Success(t *testing.T) {
	app, keyStore, _, transparencyStore := setupKeyTestApp(t)
	userID := uuid.New()
	deviceID := uuid.New()
	keyStore.getByUserDeviceFn = func(ctx context.Context, gotUserID, gotDeviceID uuid.UUID) (*model.UserDeviceKeys, error) {
		return &model.UserDeviceKeys{
			UserID:                gotUserID,
			DeviceID:              gotDeviceID,
			IdentityKey:           []byte("12345678901234567890123456789012"),
			SignedPreKey:          []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			SignedPreKeySignature: []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
			SignedPreKeyID:        1,
		}, nil
	}
	keyStore.upsertFn = func(ctx context.Context, keys *model.UserDeviceKeys) error {
		return nil
	}
	transparencyStore.appendFn = func(ctx context.Context, entry *model.KeyTransparencyEntry) error {
		return nil
	}

	resp := doRequest(app, http.MethodPost, "/keys/signed-prekey", map[string]interface{}{
		"signed_prekey":           encodeKey(32, 9),
		"signed_prekey_signature": encodeKey(64, 8),
		"signed_prekey_id":        2,
	}, map[string]string{
		"X-User-ID":   userID.String(),
		"X-Device-ID": deviceID.String(),
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestUploadOneTimePreKeys_Success(t *testing.T) {
	app, _, preKeyStore, transparencyStore := setupKeyTestApp(t)
	preKeyStore.uploadBatchFn = func(ctx context.Context, userID, deviceID uuid.UUID, keys []model.OneTimePreKey) (int, error) {
		return len(keys), nil
	}
	transparencyStore.appendFn = func(ctx context.Context, entry *model.KeyTransparencyEntry) error {
		return nil
	}

	prekeys := make([]map[string]interface{}, 0, 5)
	for i := 0; i < 5; i++ {
		prekeys = append(prekeys, map[string]interface{}{
			"key_id":     i + 1,
			"public_key": encodeKey(32, byte(i+1)),
		})
	}

	resp := doRequest(app, http.MethodPost, "/keys/one-time-prekeys", map[string]interface{}{
		"prekeys": prekeys,
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-Device-ID": uuid.New().String(),
	})

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	result := parseResponse(resp)
	if result["count"] != float64(5) {
		t.Fatalf("expected count 5, got %v", result["count"])
	}
}

func TestUploadOneTimePreKeys_TooMany(t *testing.T) {
	app, _, _, _ := setupKeyTestApp(t)

	prekeys := make([]map[string]interface{}, 0, 101)
	for i := 0; i < 101; i++ {
		prekeys = append(prekeys, map[string]interface{}{
			"key_id":     i + 1,
			"public_key": encodeKey(32, 1),
		})
	}

	resp := doRequest(app, http.MethodPost, "/keys/one-time-prekeys", map[string]interface{}{
		"prekeys": prekeys,
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-Device-ID": uuid.New().String(),
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUploadOneTimePreKeys_EmptyBatch(t *testing.T) {
	app, _, _, _ := setupKeyTestApp(t)

	resp := doRequest(app, http.MethodPost, "/keys/one-time-prekeys", map[string]interface{}{
		"prekeys": []map[string]interface{}{},
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-Device-ID": uuid.New().String(),
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetKeyBundle_Success(t *testing.T) {
	app, keyStore, preKeyStore, _ := setupKeyTestApp(t)
	userID := uuid.New()
	deviceID := uuid.New()
	identity := []byte("12345678901234567890123456789012")
	signed := []byte("abcdefghijklmnopqrstuvwxyzABCDEF")
	signature := make([]byte, 64)
	for i := range signature {
		signature[i] = byte(i + 1)
	}
	preKeyBytes := []byte("ZYXWVUTSRQPONMLKJIHGFEDCBA987654")

	keyStore.listByUserFn = func(ctx context.Context, gotUserID uuid.UUID) ([]model.UserDeviceKeys, error) {
		return []model.UserDeviceKeys{{
			UserID:                gotUserID,
			DeviceID:              deviceID,
			IdentityKey:           identity,
			SignedPreKey:          signed,
			SignedPreKeySignature: signature,
			SignedPreKeyID:        3,
			UploadedAt:            time.Now(),
		}}, nil
	}
	preKeyStore.consumeOneFn = func(ctx context.Context, gotUserID uuid.UUID) (*model.OneTimePreKey, error) {
		return &model.OneTimePreKey{
			ID:        1,
			UserID:    gotUserID,
			DeviceID:  deviceID,
			KeyID:     9,
			PublicKey: preKeyBytes,
		}, nil
	}

	resp := doRequest(app, http.MethodGet, "/keys/"+userID.String()+"/bundle", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseResponse(resp)
	if result["identity_key"] != base64.RawURLEncoding.EncodeToString(identity) {
		t.Fatalf("unexpected identity_key: %v", result["identity_key"])
	}
	if result["signed_prekey"] != base64.RawURLEncoding.EncodeToString(signed) {
		t.Fatalf("unexpected signed_prekey: %v", result["signed_prekey"])
	}
	if result["one_time_prekey"] != base64.RawURLEncoding.EncodeToString(preKeyBytes) {
		t.Fatalf("unexpected one_time_prekey: %v", result["one_time_prekey"])
	}
}

func TestGetKeyBundle_UserNotFound(t *testing.T) {
	app, keyStore, _, _ := setupKeyTestApp(t)
	keyStore.listByUserFn = func(ctx context.Context, gotUserID uuid.UUID) ([]model.UserDeviceKeys, error) {
		return nil, nil
	}

	resp := doRequest(app, http.MethodGet, "/keys/"+uuid.New().String()+"/bundle", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestGetIdentityKey_Success(t *testing.T) {
	app, keyStore, _, _ := setupKeyTestApp(t)
	userID := uuid.New()
	identity := []byte("12345678901234567890123456789012")
	keyStore.getIdentityKeyFn = func(ctx context.Context, gotUserID uuid.UUID) ([]byte, error) {
		return identity, nil
	}

	resp := doRequest(app, http.MethodGet, "/keys/"+userID.String()+"/identity", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseResponse(resp)
	if result["identity_key"] != base64.RawURLEncoding.EncodeToString(identity) {
		t.Fatalf("unexpected identity_key: %v", result["identity_key"])
	}
}

func TestGetPreKeyCount_Success(t *testing.T) {
	app, _, preKeyStore, _ := setupKeyTestApp(t)
	preKeyStore.countRemainingFn = func(ctx context.Context, userID uuid.UUID) (int, error) {
		return 12, nil
	}

	resp := doRequest(app, http.MethodGet, "/keys/count", nil, map[string]string{
		"X-User-ID": uuid.New().String(),
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	result := parseResponse(resp)
	if result["count"] != float64(12) {
		t.Fatalf("expected count 12, got %v", result["count"])
	}
}

func TestGetTransparencyLog_Success(t *testing.T) {
	app, _, _, transparencyStore := setupKeyTestApp(t)
	userID := uuid.New()
	deviceID := uuid.New()
	transparencyStore.listByUserFn = func(ctx context.Context, gotUserID uuid.UUID, limit int) ([]model.KeyTransparencyEntry, error) {
		return []model.KeyTransparencyEntry{{
			ID:            1,
			UserID:        gotUserID,
			DeviceID:      deviceID,
			EventType:     "identity_key_registered",
			PublicKeyHash: "abc123",
			CreatedAt:     time.Now(),
		}}, nil
	}

	resp := doRequest(app, http.MethodGet, "/keys/transparency-log?user_id="+userID.String()+"&limit=10", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseResponse(resp)
	entries, ok := result["entries"].([]interface{})
	if !ok {
		t.Fatalf("expected entries array, got %T", result["entries"])
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}
