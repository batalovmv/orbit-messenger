// Thin wrappers around @noble/* primitives used throughout the E2E layer.
//
// Goal: every crypto operation goes through this file so that audits touch
// one place, and so that swapping a primitive (e.g. to a WebCrypto subtle
// implementation later) is a single-file change.

import { ed25519, x25519 } from '@noble/curves/ed25519.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { hkdf } from '@noble/hashes/hkdf.js';
import { gcm } from '@noble/ciphers/aes.js';

import type { KeyPair } from './types';

// ────────────────────────────────────────────────────────────────────────────
// Random
// ────────────────────────────────────────────────────────────────────────────

export function randomBytes(length: number): Uint8Array {
  const out = new Uint8Array(length);
  // crypto.getRandomValues is available in browsers, jsdom, and Node >= 19.
  crypto.getRandomValues(out);
  return out;
}

// ────────────────────────────────────────────────────────────────────────────
// Ed25519 (long-term identity keys + signatures)
// ────────────────────────────────────────────────────────────────────────────

export function generateEd25519KeyPair(): KeyPair {
  const kp = ed25519.keygen();
  return { publicKey: kp.publicKey, secretKey: kp.secretKey };
}

export function signEd25519(message: Uint8Array, secretKey: Uint8Array): Uint8Array {
  return ed25519.sign(message, secretKey);
}

export function verifyEd25519(
  signature: Uint8Array,
  message: Uint8Array,
  publicKey: Uint8Array,
): boolean {
  try {
    return ed25519.verify(signature, message, publicKey);
  } catch {
    return false;
  }
}

// ────────────────────────────────────────────────────────────────────────────
// X25519 (ECDH for prekeys + ratchet)
// ────────────────────────────────────────────────────────────────────────────

export function generateX25519KeyPair(): KeyPair {
  const kp = x25519.keygen();
  return { publicKey: kp.publicKey, secretKey: kp.secretKey };
}

export function x25519DH(secretKey: Uint8Array, peerPublicKey: Uint8Array): Uint8Array {
  return x25519.getSharedSecret(secretKey, peerPublicKey);
}

// ────────────────────────────────────────────────────────────────────────────
// SHA-256 / HKDF
// ────────────────────────────────────────────────────────────────────────────

export function sha256Digest(data: Uint8Array): Uint8Array {
  return sha256(data);
}

export function hkdfSha256(
  ikm: Uint8Array,
  salt: Uint8Array,
  info: Uint8Array,
  length: number,
): Uint8Array {
  return hkdf(sha256, ikm, salt, info, length);
}

// ────────────────────────────────────────────────────────────────────────────
// AES-256-GCM
// ────────────────────────────────────────────────────────────────────────────

// Nonce length required by AES-GCM (96 bits).
export const AES_NONCE_LENGTH = 12;
// Authentication tag length (128 bits) — noble appends this to ciphertext.
export const AES_TAG_LENGTH = 16;
export const AES_KEY_LENGTH = 32;

export function aes256GcmEncrypt(
  key: Uint8Array,
  nonce: Uint8Array,
  plaintext: Uint8Array,
  aad?: Uint8Array,
): Uint8Array {
  if (key.length !== AES_KEY_LENGTH) {
    throw new Error(`aes256GcmEncrypt: key must be ${AES_KEY_LENGTH} bytes`);
  }
  if (nonce.length !== AES_NONCE_LENGTH) {
    throw new Error(`aes256GcmEncrypt: nonce must be ${AES_NONCE_LENGTH} bytes`);
  }
  return gcm(key, nonce, aad).encrypt(plaintext);
}

export function aes256GcmDecrypt(
  key: Uint8Array,
  nonce: Uint8Array,
  ciphertext: Uint8Array,
  aad?: Uint8Array,
): Uint8Array {
  if (key.length !== AES_KEY_LENGTH) {
    throw new Error(`aes256GcmDecrypt: key must be ${AES_KEY_LENGTH} bytes`);
  }
  if (nonce.length !== AES_NONCE_LENGTH) {
    throw new Error(`aes256GcmDecrypt: nonce must be ${AES_NONCE_LENGTH} bytes`);
  }
  return gcm(key, nonce, aad).decrypt(ciphertext);
}

// ────────────────────────────────────────────────────────────────────────────
// Byte helpers
// ────────────────────────────────────────────────────────────────────────────

export function concatBytes(...arrays: Uint8Array[]): Uint8Array {
  let totalLength = 0;
  for (const a of arrays) totalLength += a.length;
  const out = new Uint8Array(totalLength);
  let offset = 0;
  for (const a of arrays) {
    out.set(a, offset);
    offset += a.length;
  }
  return out;
}

// Constant-time byte comparison. Used whenever we compare secrets/hashes.
export function constantTimeEquals(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a[i] ^ b[i];
  return diff === 0;
}
