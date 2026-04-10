package handler

import (
	"encoding/base64"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/auth/internal/model"
	"github.com/mst-corp/orbit/services/auth/internal/service"
)

type KeyHandler struct {
	keySvc *service.KeyService
	logger *slog.Logger
}

func NewKeyHandler(keySvc *service.KeyService, logger *slog.Logger) *KeyHandler {
	return &KeyHandler{keySvc: keySvc, logger: logger}
}

func (h *KeyHandler) Register(router fiber.Router) {
	keys := router.Group("/keys")
	keys.Post("/identity", h.RegisterDeviceKeys)
	keys.Post("/signed-prekey", h.RotateSignedPreKey)
	keys.Post("/one-time-prekeys", h.UploadOneTimePreKeys)
	keys.Get("/:userId/bundle", h.GetKeyBundle)
	keys.Get("/:userId/identity", h.GetIdentityKey)
	keys.Get("/count", h.GetPreKeyCount)
	keys.Get("/transparency-log", h.GetTransparencyLog)
}

func getKeyUserID(c *fiber.Ctx) (uuid.UUID, error) {
	idStr := c.Get("X-User-ID")
	if idStr == "" {
		return uuid.Nil, apperror.Unauthorized("missing user context")
	}
	return uuid.Parse(idStr)
}

func getDeviceID(c *fiber.Ctx) (uuid.UUID, error) {
	idStr := c.Get("X-Device-ID")
	if idStr == "" {
		return uuid.Nil, apperror.BadRequest("missing device ID")
	}
	return uuid.Parse(idStr)
}

type registerKeysRequest struct {
	IdentityKey           string `json:"identity_key"`
	SignedPreKey          string `json:"signed_prekey"`
	SignedPreKeySignature string `json:"signed_prekey_signature"`
	SignedPreKeyID        int    `json:"signed_prekey_id"`
}

type rotatePreKeyRequest struct {
	SignedPreKey          string `json:"signed_prekey"`
	SignedPreKeySignature string `json:"signed_prekey_signature"`
	SignedPreKeyID        int    `json:"signed_prekey_id"`
}

type uploadPreKeysRequest struct {
	PreKeys []preKeyItem `json:"prekeys"`
}

type preKeyItem struct {
	KeyID     int    `json:"key_id"`
	PublicKey string `json:"public_key"`
}

func (h *KeyHandler) RegisterDeviceKeys(c *fiber.Ctx) error {
	var req registerKeysRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}

	userID, err := getKeyUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	deviceID, err := getDeviceID(c)
	if err != nil {
		return response.Error(c, err)
	}

	identityKey, err := base64.RawURLEncoding.DecodeString(req.IdentityKey)
	if err != nil {
		return response.Error(c, apperror.BadRequest("invalid identity key"))
	}
	signedPreKey, err := base64.RawURLEncoding.DecodeString(req.SignedPreKey)
	if err != nil {
		return response.Error(c, apperror.BadRequest("invalid signed prekey"))
	}
	signedPreKeySignature, err := base64.RawURLEncoding.DecodeString(req.SignedPreKeySignature)
	if err != nil {
		return response.Error(c, apperror.BadRequest("invalid signed prekey signature"))
	}

	if err := h.keySvc.RegisterDeviceKeys(
		c.Context(),
		userID,
		deviceID,
		identityKey,
		signedPreKey,
		signedPreKeySignature,
		req.SignedPreKeyID,
	); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, fiber.Map{"status": "ok"})
}

func (h *KeyHandler) RotateSignedPreKey(c *fiber.Ctx) error {
	var req rotatePreKeyRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}

	userID, err := getKeyUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	deviceID, err := getDeviceID(c)
	if err != nil {
		return response.Error(c, err)
	}

	signedPreKey, err := base64.RawURLEncoding.DecodeString(req.SignedPreKey)
	if err != nil {
		return response.Error(c, apperror.BadRequest("invalid signed prekey"))
	}
	signedPreKeySignature, err := base64.RawURLEncoding.DecodeString(req.SignedPreKeySignature)
	if err != nil {
		return response.Error(c, apperror.BadRequest("invalid signed prekey signature"))
	}

	if err := h.keySvc.RotateSignedPreKey(
		c.Context(),
		userID,
		deviceID,
		signedPreKey,
		signedPreKeySignature,
		req.SignedPreKeyID,
	); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"status": "ok"})
}

func (h *KeyHandler) UploadOneTimePreKeys(c *fiber.Ctx) error {
	var req uploadPreKeysRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}
	if len(req.PreKeys) == 0 || len(req.PreKeys) > 100 {
		return response.Error(c, apperror.BadRequest("prekeys batch must contain 1 to 100 items"))
	}

	userID, err := getKeyUserID(c)
	if err != nil {
		return response.Error(c, err)
	}
	deviceID, err := getDeviceID(c)
	if err != nil {
		return response.Error(c, err)
	}

	prekeys := make([]model.OneTimePreKey, 0, len(req.PreKeys))
	for _, item := range req.PreKeys {
		publicKey, err := base64.RawURLEncoding.DecodeString(item.PublicKey)
		if err != nil {
			return response.Error(c, apperror.BadRequest("invalid one-time prekey"))
		}
		prekeys = append(prekeys, model.OneTimePreKey{
			KeyID:     item.KeyID,
			PublicKey: publicKey,
		})
	}

	count, err := h.keySvc.UploadOneTimePreKeys(c.Context(), userID, deviceID, prekeys)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, fiber.Map{"count": count})
}

func (h *KeyHandler) GetKeyBundle(c *fiber.Ctx) error {
	targetUserID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("invalid user ID"))
	}

	bundle, err := h.keySvc.GetKeyBundle(c.Context(), targetUserID)
	if err != nil {
		return response.Error(c, err)
	}

	type bundleResponse struct {
		IdentityKey           string `json:"identity_key"`
		SignedPreKey          string `json:"signed_prekey"`
		SignedPreKeySignature string `json:"signed_prekey_signature"`
		SignedPreKeyID        int    `json:"signed_prekey_id"`
		OneTimePreKey         string `json:"one_time_prekey,omitempty"`
		OneTimePreKeyID       *int   `json:"one_time_prekey_id,omitempty"`
		DeviceID              string `json:"device_id"`
	}

	resp := bundleResponse{
		IdentityKey:           base64.RawURLEncoding.EncodeToString(bundle.IdentityKey),
		SignedPreKey:          base64.RawURLEncoding.EncodeToString(bundle.SignedPreKey),
		SignedPreKeySignature: base64.RawURLEncoding.EncodeToString(bundle.SignedPreKeySignature),
		SignedPreKeyID:        bundle.SignedPreKeyID,
		OneTimePreKeyID:       bundle.OneTimePreKeyID,
		DeviceID:              bundle.DeviceID.String(),
	}
	if bundle.OneTimePreKey != nil {
		resp.OneTimePreKey = base64.RawURLEncoding.EncodeToString(bundle.OneTimePreKey)
	}

	return response.JSON(c, fiber.StatusOK, resp)
}

func (h *KeyHandler) GetIdentityKey(c *fiber.Ctx) error {
	userID, err := uuid.Parse(c.Params("userId"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("invalid user ID"))
	}

	key, err := h.keySvc.GetIdentityKey(c.Context(), userID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"identity_key": base64.RawURLEncoding.EncodeToString(key),
	})
}

func (h *KeyHandler) GetPreKeyCount(c *fiber.Ctx) error {
	userID, err := getKeyUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	count, err := h.keySvc.GetPreKeyCount(c.Context(), userID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"count": count})
}

func (h *KeyHandler) GetTransparencyLog(c *fiber.Ctx) error {
	userID, err := uuid.Parse(c.Query("user_id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("invalid user ID"))
	}

	entries, err := h.keySvc.GetTransparencyLog(c.Context(), userID, c.QueryInt("limit", 50))
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"entries": entries})
}
