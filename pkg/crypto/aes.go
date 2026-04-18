package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// DeriveKey derives a 32-byte AES-256 key from an arbitrary secret string.
func DeriveKey(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

// Encrypt encrypts plaintext using AES-256-GCM.
// Returns base64-encoded ciphertext (nonce prepended).
func Encrypt(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptContentField unwraps an AES-256-GCM ciphertext stored in a nullable
// string column. It mutates *ct in place with the plaintext. If the column is
// NULL/empty it is a no-op. When decryption fails (legacy rows written before
// at-rest encryption was enabled, or ciphertext produced with a different key)
// the value is left as-is so existing rows remain readable instead of causing
// 500 errors. The store layer should use this for any SELECT of an encrypted
// content column to stay consistent with the rest of the read paths.
func DecryptContentField(ct *string, key []byte) {
	if ct == nil || *ct == "" {
		return
	}
	pt, err := Decrypt(*ct, key)
	if err != nil {
		return
	}
	*ct = pt
}

// EncryptContentField wraps nullable plaintext with AES-256-GCM. Returns nil
// when the input is nil so INSERTs/UPDATEs preserve NULLs for rows that never
// had plaintext (media-only, stickers, polls).
func EncryptContentField(p *string, key []byte) (*string, error) {
	if p == nil {
		return nil, nil
	}
	ct, err := Encrypt(*p, key)
	if err != nil {
		return nil, fmt.Errorf("encrypt content: %w", err)
	}
	return &ct, nil
}

// Decrypt decrypts ciphertext produced by Encrypt.
func Decrypt(ciphertext string, key []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}
