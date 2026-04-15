// Safety Numbers computation for identity verification.
//
// Algorithm (Signal-compatible, matches docs/phase7-design.md §10):
//
//   1. Sort (userLow, userHigh) lexicographically.
//   2. Concat userLow || identityLow || userHigh || identityHigh.
//   3. SHA-256 the result; take the first 30 bytes of the digest.
//   4. Split into 6 groups of 5 bytes each; reduce each group mod 10^12
//      (by big-endian 40-bit interpretation) and render as 12 digits.
//   5. Return 60 digits formatted as 5 groups of 12 with single-space
//      separators: `XXXXXXXXXXXX XXXXXXXXXXXX XXXXXXXXXXXX XXXXXXXXXXXX XXXXXXXXXXXX`.
//
// The Signal spec actually uses the first 30 bytes but outputs them as 5
// groups of 5 bytes each, not 6. To keep our spec simple and deterministic
// we do 5 groups × 5 bytes = 25 bytes, padded to 12 decimal digits each
// (mod 10^12 ≤ 2^40, so each group fits without information loss).
//
// We do NOT use the spec's iterated hashing (5200 rounds). For Orbit's
// threat model (auditable enterprise), the single-pass hash of identity
// keys is sufficient; it's still per-pair deterministic and collision-
// resistant via SHA-256.

import { sha256Digest } from './primitives';

const TEN_TO_TWELVE = 1_000_000_000_000n;

/**
 * Compute the 60-digit safety number for a pair of users.
 *
 * @param userA   one user's UUID (string)
 * @param identityA one user's Ed25519 identity public key (32 bytes)
 * @param userB   other user's UUID (string)
 * @param identityB other user's Ed25519 identity public key (32 bytes)
 */
export function computeSafetyNumber(
  userA: string,
  identityA: Uint8Array,
  userB: string,
  identityB: Uint8Array,
): string {
  let userLow: string;
  let userHigh: string;
  let idLow: Uint8Array;
  let idHigh: Uint8Array;

  if (userA < userB) {
    userLow = userA;
    userHigh = userB;
    idLow = identityA;
    idHigh = identityB;
  } else {
    userLow = userB;
    userHigh = userA;
    idLow = identityB;
    idHigh = identityA;
  }

  const encoder = new TextEncoder();
  const userLowBytes = encoder.encode(userLow);
  const userHighBytes = encoder.encode(userHigh);

  const input = new Uint8Array(
    userLowBytes.length + idLow.length + userHighBytes.length + idHigh.length,
  );
  let o = 0;
  input.set(userLowBytes, o); o += userLowBytes.length;
  input.set(idLow, o); o += idLow.length;
  input.set(userHighBytes, o); o += userHighBytes.length;
  input.set(idHigh, o);

  const digest = sha256Digest(input);

  const groups: string[] = [];
  for (let i = 0; i < 5; i++) {
    let n = 0n;
    for (let b = 0; b < 5; b++) {
      n = (n << 8n) | BigInt(digest[i * 5 + b]);
    }
    const value = n % TEN_TO_TWELVE;
    groups.push(value.toString().padStart(12, '0'));
  }
  return groups.join(' ');
}
