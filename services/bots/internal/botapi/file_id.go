// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// FileIDCodec encodes/decodes Bot API `file_id` and `file_unique_id` values.
//
// Layout (file_id, 48 raw bytes → 64 base64url chars):
//
//	[0..16)  media UUID
//	[16..32) chat UUID  (the chat the bot saw the media in — used for the
//	         IsBotInstalled access check on download)
//	[32..48) HMAC-SHA256(media_id || chat_id || bot_id, secret)[:16]
//
// Including chat_id in the payload lets the download endpoint authorise
// access without storing per-bot file ledgers; tampering is prevented by
// the HMAC tag, which is bound to bot_id so file_ids cannot be reused
// across bots.
//
// Layout (file_unique_id, 11 raw bytes → 15 base64url chars):
//
//	HMAC-SHA256(media_id, secret)[:11]
//
// file_unique_id is the same for the same media regardless of which bot
// sees it — it is a stable identifier for deduplication clients.
type FileIDCodec struct {
	secret []byte
}

// NewFileIDCodec returns a codec keyed by the given secret. The secret
// should be at least 32 bytes; in production it reuses the bots service
// encryption key.
func NewFileIDCodec(secret []byte) *FileIDCodec {
	return &FileIDCodec{secret: secret}
}

// Encode produces a deterministic file_id for (media, chat, bot).
func (c *FileIDCodec) Encode(mediaID, chatID, botID uuid.UUID) string {
	if len(c.secret) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, c.secret)
	mac.Write(mediaID[:])
	mac.Write(chatID[:])
	mac.Write(botID[:])
	sig := mac.Sum(nil)[:16]

	buf := make([]byte, 0, 48)
	buf = append(buf, mediaID[:]...)
	buf = append(buf, chatID[:]...)
	buf = append(buf, sig...)
	return base64.RawURLEncoding.EncodeToString(buf)
}

// EncodeUnique returns the bot-independent file_unique_id for a media item.
func (c *FileIDCodec) EncodeUnique(mediaID uuid.UUID) string {
	if len(c.secret) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, c.secret)
	mac.Write(mediaID[:])
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:11])
}

// ErrInvalidFileID is returned when a file_id fails to decode or verify.
var ErrInvalidFileID = errors.New("invalid file_id")

// Decode parses a file_id, verifies the HMAC against the supplied bot, and
// returns the embedded media and chat ids. Returns ErrInvalidFileID on any
// format or signature failure.
func (c *FileIDCodec) Decode(fileID string, botID uuid.UUID) (mediaID, chatID uuid.UUID, err error) {
	if len(c.secret) == 0 {
		return uuid.Nil, uuid.Nil, ErrInvalidFileID
	}
	fileID = strings.TrimSpace(fileID)
	raw, err := base64.RawURLEncoding.DecodeString(fileID)
	if err != nil || len(raw) != 48 {
		return uuid.Nil, uuid.Nil, ErrInvalidFileID
	}
	copy(mediaID[:], raw[0:16])
	copy(chatID[:], raw[16:32])

	mac := hmac.New(sha256.New, c.secret)
	mac.Write(mediaID[:])
	mac.Write(chatID[:])
	mac.Write(botID[:])
	want := mac.Sum(nil)[:16]
	if !hmac.Equal(raw[32:48], want) {
		return uuid.Nil, uuid.Nil, ErrInvalidFileID
	}
	return mediaID, chatID, nil
}
