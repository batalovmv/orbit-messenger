// Ed25519 ↔ X25519 conversion for XEdDSA-compatible identity key reuse.
//
// The Ed25519 and X25519 curves share the same underlying curve25519 — they
// are the Edwards and Montgomery forms respectively, and the birational map
// between y (Edwards) and u (Montgomery) is:
//
//   u = (1 + y) / (1 - y)   mod p,   p = 2^255 - 19
//
// For private keys the transformation is: SHA-512 the Ed25519 seed, keep
// the first 32 bytes, then clamp per RFC 7748. This matches libsodium's
// `crypto_sign_ed25519_sk_to_curve25519` / Signal libsignal's same helper.
//
// Noble v2 removed the convenience `edwardsToMontgomery*` helpers, so we
// implement them here with bigint math. Both routines are pure and
// deterministic.

import { sha512 } from '@noble/hashes/sha2.js';

// p = 2^255 - 19
const P = (1n << 255n) - 19n;

function bytesToNumberLE(bytes: Uint8Array): bigint {
  let n = 0n;
  for (let i = bytes.length - 1; i >= 0; i--) {
    n = (n << 8n) | BigInt(bytes[i]);
  }
  return n;
}

function numberToBytesLE(n: bigint, length: number): Uint8Array {
  const out = new Uint8Array(length);
  let v = n;
  for (let i = 0; i < length; i++) {
    out[i] = Number(v & 0xffn);
    v >>= 8n;
  }
  return out;
}

// Fermat's little theorem: a^(p-2) ≡ a^-1 mod p for prime p.
function modInverse(a: bigint, p: bigint): bigint {
  return modPow(mod(a, p), p - 2n, p);
}

function mod(a: bigint, p: bigint): bigint {
  const r = a % p;
  return r < 0n ? r + p : r;
}

function modPow(base: bigint, exp: bigint, p: bigint): bigint {
  let result = 1n;
  let b = mod(base, p);
  let e = exp;
  while (e > 0n) {
    if (e & 1n) result = mod(result * b, p);
    e >>= 1n;
    b = mod(b * b, p);
  }
  return result;
}

/**
 * Convert an Ed25519 public key (32 bytes, little-endian y with sign bit in
 * bit 255) to the matching X25519 public key (32 bytes, little-endian u).
 */
export function ed25519PublicToX25519(edPublic: Uint8Array): Uint8Array {
  if (edPublic.length !== 32) {
    throw new Error('ed25519PublicToX25519: expected 32-byte input');
  }
  // Copy and mask the sign bit out — we only want the y-coordinate magnitude.
  const yBytes = edPublic.slice();
  yBytes[31] &= 0x7f;
  const y = bytesToNumberLE(yBytes);

  const oneMinusY = mod(1n - y, P);
  if (oneMinusY === 0n) {
    throw new Error('ed25519PublicToX25519: invalid point (1 - y = 0)');
  }
  const u = mod((1n + y) * modInverse(oneMinusY, P), P);
  return numberToBytesLE(u, 32);
}

/**
 * Convert an Ed25519 secret seed (32 bytes) to the matching X25519 private
 * key (32 bytes). Matches libsodium's `crypto_sign_ed25519_sk_to_curve25519`
 * and behaves identically to Signal's helper.
 */
export function ed25519SecretToX25519(edSecret: Uint8Array): Uint8Array {
  if (edSecret.length !== 32) {
    throw new Error('ed25519SecretToX25519: expected 32-byte seed');
  }
  const hash = sha512(edSecret);
  const out = hash.slice(0, 32);
  // Clamp per RFC 7748 §5.
  out[0] &= 248;
  out[31] &= 127;
  out[31] |= 64;
  return out;
}
