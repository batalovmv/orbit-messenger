package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/auth/internal/model"
	"github.com/mst-corp/orbit/services/auth/internal/store"
)

type KeyService struct {
	keys         store.KeyStore
	prekeys      store.PreKeyStore
	transparency store.TransparencyStore
}

func NewKeyService(keys store.KeyStore, prekeys store.PreKeyStore, transparency store.TransparencyStore) *KeyService {
	return &KeyService{keys: keys, prekeys: prekeys, transparency: transparency}
}

func (s *KeyService) RegisterDeviceKeys(ctx context.Context, userID, deviceID uuid.UUID, identityKey, signedPreKey, signedPreKeySig []byte, signedPreKeyID int) error {
	if len(identityKey) != 32 {
		return apperror.BadRequest("identity key must be 32 bytes")
	}
	if len(signedPreKey) != 32 {
		return apperror.BadRequest("signed prekey must be 32 bytes")
	}
	if len(signedPreKeySig) != 64 {
		return apperror.BadRequest("signed prekey signature must be 64 bytes")
	}

	err := s.keys.Upsert(ctx, &model.UserDeviceKeys{
		UserID:                userID,
		DeviceID:              deviceID,
		IdentityKey:           identityKey,
		SignedPreKey:          signedPreKey,
		SignedPreKeySignature: signedPreKeySig,
		SignedPreKeyID:        signedPreKeyID,
	})
	if err != nil {
		return fmt.Errorf("upsert device keys: %w", err)
	}

	if err := s.appendTransparency(ctx, userID, deviceID, "identity_key_registered", identityKey); err != nil {
		return fmt.Errorf("append transparency entry: %w", err)
	}

	slog.Info("device keys registered", "user_id", userID, "device_id", deviceID)
	return nil
}

func (s *KeyService) RotateSignedPreKey(ctx context.Context, userID, deviceID uuid.UUID, signedPreKey, signedPreKeySig []byte, signedPreKeyID int) error {
	if len(signedPreKey) != 32 {
		return apperror.BadRequest("signed prekey must be 32 bytes")
	}
	if len(signedPreKeySig) != 64 {
		return apperror.BadRequest("signed prekey signature must be 64 bytes")
	}

	existing, err := s.keys.GetByUserAndDevice(ctx, userID, deviceID)
	if err != nil {
		return fmt.Errorf("get device keys: %w", err)
	}
	if existing == nil {
		return apperror.NotFound("device keys not registered")
	}

	existing.SignedPreKey = signedPreKey
	existing.SignedPreKeySignature = signedPreKeySig
	existing.SignedPreKeyID = signedPreKeyID
	if err := s.keys.Upsert(ctx, existing); err != nil {
		return fmt.Errorf("update signed prekey: %w", err)
	}

	if err := s.appendTransparency(ctx, userID, deviceID, "signed_prekey_rotated", signedPreKey); err != nil {
		return fmt.Errorf("append transparency entry: %w", err)
	}
	return nil
}

func (s *KeyService) UploadOneTimePreKeys(ctx context.Context, userID, deviceID uuid.UUID, keys []model.OneTimePreKey) (int, error) {
	if len(keys) == 0 {
		return 0, apperror.BadRequest("prekeys are required")
	}
	if len(keys) > 100 {
		return 0, apperror.BadRequest("prekey batch must not exceed 100")
	}
	for _, key := range keys {
		if len(key.PublicKey) != 32 {
			return 0, apperror.BadRequest("one-time prekey must be 32 bytes")
		}
	}

	count, err := s.prekeys.UploadBatch(ctx, userID, deviceID, keys)
	if err != nil {
		return 0, fmt.Errorf("upload prekeys: %w", err)
	}

	if err := s.appendTransparency(ctx, userID, deviceID, "prekeys_uploaded", keys[0].PublicKey); err != nil {
		return 0, fmt.Errorf("append transparency entry: %w", err)
	}
	return count, nil
}

func (s *KeyService) GetKeyBundle(ctx context.Context, targetUserID uuid.UUID) (*model.KeyBundle, error) {
	devices, err := s.keys.ListByUser(ctx, targetUserID)
	if err != nil {
		return nil, fmt.Errorf("list user keys: %w", err)
	}
	if len(devices) == 0 {
		return nil, apperror.NotFound("user has no registered keys")
	}

	selected := devices[0]
	prekey, err := s.prekeys.ConsumeOne(ctx, targetUserID)
	if err != nil {
		return nil, fmt.Errorf("consume prekey: %w", err)
	}
	if prekey != nil {
		// Find the device that owns the consumed prekey. If the device was
		// revoked but orphaned prekeys remain, discard the prekey to avoid
		// returning a bundle with mismatched identity_key / device_id.
		found := false
		for _, device := range devices {
			if device.DeviceID == prekey.DeviceID {
				selected = device
				found = true
				break
			}
		}
		if !found {
			slog.Warn("orphaned prekey consumed — device no longer registered",
				"user_id", targetUserID, "prekey_device_id", prekey.DeviceID)
			prekey = nil // drop orphaned prekey; bundle still valid without OTK
		}
	}

	bundle := &model.KeyBundle{
		IdentityKey:           selected.IdentityKey,
		SignedPreKey:          selected.SignedPreKey,
		SignedPreKeySignature: selected.SignedPreKeySignature,
		SignedPreKeyID:        selected.SignedPreKeyID,
		DeviceID:              selected.DeviceID,
	}
	if prekey != nil {
		bundle.OneTimePreKey = prekey.PublicKey
		bundle.OneTimePreKeyID = &prekey.KeyID
	}
	return bundle, nil
}

func (s *KeyService) GetIdentityKey(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	identityKey, err := s.keys.GetIdentityKey(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get identity key: %w", err)
	}
	if identityKey == nil {
		return nil, apperror.NotFound("user has no identity key")
	}
	return identityKey, nil
}

func (s *KeyService) ListUserDevices(ctx context.Context, userID uuid.UUID) ([]model.UserDeviceKeys, error) {
	devices, err := s.keys.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list user devices: %w", err)
	}
	return devices, nil
}

func (s *KeyService) GetPreKeyCount(ctx context.Context, userID, deviceID uuid.UUID) (int, error) {
	count, err := s.prekeys.CountRemaining(ctx, userID, deviceID)
	if err != nil {
		return 0, fmt.Errorf("count prekeys: %w", err)
	}
	return count, nil
}

func (s *KeyService) GetTransparencyLog(ctx context.Context, userID uuid.UUID, limit int) ([]model.KeyTransparencyEntry, error) {
	entries, err := s.transparency.ListByUser(ctx, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list transparency log: %w", err)
	}
	return entries, nil
}

func (s *KeyService) RevokeDevice(ctx context.Context, userID, deviceID uuid.UUID) error {
	if err := s.keys.DeleteByDevice(ctx, userID, deviceID); err != nil {
		return fmt.Errorf("delete device keys: %w", err)
	}
	if err := s.prekeys.DeleteByDevice(ctx, userID, deviceID); err != nil {
		return fmt.Errorf("delete device prekeys: %w", err)
	}
	if err := s.appendTransparency(ctx, userID, deviceID, "device_revoked", []byte(deviceID.String())); err != nil {
		return fmt.Errorf("append transparency entry: %w", err)
	}

	slog.Info("device revoked", "user_id", userID, "device_id", deviceID)
	return nil
}

func (s *KeyService) appendTransparency(ctx context.Context, userID, deviceID uuid.UUID, eventType string, data []byte) error {
	return s.transparency.Append(ctx, &model.KeyTransparencyEntry{
		UserID:        userID,
		DeviceID:      deviceID,
		EventType:     eventType,
		PublicKeyHash: hashKey(data),
	})
}

func hashKey(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
