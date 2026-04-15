// Phase 7.1: client-side media encryption.
//
// Each media file gets a fresh random AES-256-GCM key and 12-byte nonce. The
// sender encrypts the bytes client-side, uploads the ciphertext to the media
// service as an opaque blob, and then references the resulting `media_id`
// inside the E2E envelope of an encrypted message together with the key and
// nonce. The receiver pulls the ciphertext from `/media/{id}` and decrypts it
// using the key material carried in the envelope.
//
// The server never sees the key, never sees plaintext, never inspects mime
// type, never generates thumbnails. All three constraints are enforced by
// `services/media/internal/service/media_service.go` (see UploadEncrypted) and
// `services/messaging/internal/store/media_message_store.go`
// (CreateEncryptedWithMedia).
//
// This file only exposes high-level helpers; the actual crypto primitives
// live in `./primitives.ts` and are shared with the rest of the E2E layer.

import {
  AES_KEY_LENGTH,
  AES_NONCE_LENGTH,
  aes256GcmDecrypt,
  aes256GcmEncrypt,
  randomBytes,
} from './primitives';

export type MediaCryptoKey = {
  key: Uint8Array;   // 32 bytes — AES-256 session key, generated per-file
  nonce: Uint8Array; // 12 bytes — per-file random nonce (never reused)
};

export function generateMediaKey(): MediaCryptoKey {
  return {
    key: randomBytes(AES_KEY_LENGTH),
    nonce: randomBytes(AES_NONCE_LENGTH),
  };
}

export function encryptMediaBlob(plaintext: Uint8Array, material: MediaCryptoKey): Uint8Array {
  return aes256GcmEncrypt(material.key, material.nonce, plaintext);
}

export function decryptMediaBlob(ciphertext: Uint8Array, material: MediaCryptoKey): Uint8Array {
  return aes256GcmDecrypt(material.key, material.nonce, ciphertext);
}

// Convenience: encrypt a File/Blob all at once by reading it into an
// ArrayBuffer first. Returns a fresh key material object so callers can stash
// it inside the envelope they ship alongside the upload.
export async function encryptFileForUpload(file: Blob): Promise<{
  ciphertext: Uint8Array;
  material: MediaCryptoKey;
}> {
  const buf = await file.arrayBuffer();
  const material = generateMediaKey();
  const ciphertext = encryptMediaBlob(new Uint8Array(buf), material);
  return { ciphertext, material };
}
