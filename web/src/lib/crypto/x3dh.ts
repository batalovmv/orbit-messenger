// X3DH (Extended Triple Diffie-Hellman) session bootstrap.
//
// Reference: https://signal.org/docs/specifications/x3dh/
//
// Alice (initiator) flow:
//   1. Fetch Bob's prekey bundle (IK_B, SPK_B, SPK_signature, OPK_B?).
//   2. Verify SPK_signature with IK_B.
//   3. Generate ephemeral EK_A.
//   4. DH1 = DH(IK_A, SPK_B)
//      DH2 = DH(EK_A, IK_B)
//      DH3 = DH(EK_A, SPK_B)
//      DH4 = DH(EK_A, OPK_B)   — only if OPK_B present
//   5. SK  = HKDF(DH1 || DH2 || DH3 [|| DH4], "OrbitX3DH", 32)
//
// Bob (responder) flow (on first incoming message):
//   1. Extract IK_A, EK_A, identifier(SPK_B used), identifier(OPK_B used?)
//   2. Look up matching SPK private + OPK private in local store.
//   3. DH1 = DH(SPK_B, IK_A)
//      DH2 = DH(IK_B, EK_A)
//      DH3 = DH(SPK_B, EK_A)
//      DH4 = DH(OPK_B, EK_A)   — only if OPK used
//   4. SK  = HKDF(DH1 || DH2 || DH3 [|| DH4], "OrbitX3DH", 32)
//
// CRITICAL asymmetry: Ed25519 identity keys are SIGNATURE keys, not ECDH
// keys. For the IK contribution we convert Ed25519 → X25519 (Montgomery form).
// The conversion is deterministic (RFC 7748 / Signal spec).

import {
  ed25519PublicToX25519 as _ed25519PublicToX25519,
  ed25519SecretToX25519 as _ed25519SecretToX25519,
} from './ed25519-to-x25519';
import {
  concatBytes,
  hkdfSha256,
  verifyEd25519,
  x25519DH,
} from './primitives';
import type { IdentityKeyPair, OneTimePreKey, PreKeyBundle, SignedPreKey } from './types';

const X3DH_INFO = new TextEncoder().encode('OrbitX3DH');
const DERIVED_LENGTH = 32;

// Constant salt of 32 zero bytes per Signal X3DH spec §3.3.
const HKDF_SALT = new Uint8Array(32);

// F constant from X3DH spec §2.2 — 32 bytes of 0xFF prepended to DH output
// for curve25519 (distinguishes the KDF input from other protocols).
const F_PREFIX = new Uint8Array(32).fill(0xff);

// Re-exports so callers can stay within the x3dh module surface.
export const ed25519PublicToX25519 = _ed25519PublicToX25519;
export const ed25519SecretToX25519 = _ed25519SecretToX25519;

// ────────────────────────────────────────────────────────────────────────────
// Alice side — session init from peer bundle
// ────────────────────────────────────────────────────────────────────────────

export type AliceX3DHResult = {
  sharedSecret: Uint8Array;         // 32B root seed for Double Ratchet
  ephemeralKey: Uint8Array;         // EK_A public (Bob needs this)
  usedOneTimePreKeyId?: number;     // so Bob knows which OPK to consume
};

export function x3dhInitAsAlice(
  aliceIdentity: IdentityKeyPair,
  aliceEphemeral: { publicKey: Uint8Array; secretKey: Uint8Array },
  bobBundle: PreKeyBundle,
): AliceX3DHResult {
  // 1. Verify signed prekey signature.
  const sigOk = verifyEd25519(
    bobBundle.signedPreKeySignature,
    bobBundle.signedPreKey,
    bobBundle.identityKey,
  );
  if (!sigOk) {
    throw new Error('x3dh: Bob signed prekey signature verification failed');
  }

  // 2. Convert Alice's Ed25519 identity secret to X25519 for DH1.
  const aliceIdX25519Secret = ed25519SecretToX25519(aliceIdentity.secretKey);
  // 3. Convert Bob's Ed25519 identity public to X25519 for DH2.
  const bobIdX25519Public = ed25519PublicToX25519(bobBundle.identityKey);

  // 4. Four DH computations.
  const dh1 = x25519DH(aliceIdX25519Secret, bobBundle.signedPreKey);
  const dh2 = x25519DH(aliceEphemeral.secretKey, bobIdX25519Public);
  const dh3 = x25519DH(aliceEphemeral.secretKey, bobBundle.signedPreKey);
  let dhInput = concatBytes(F_PREFIX, dh1, dh2, dh3);
  if (bobBundle.oneTimePreKey) {
    const dh4 = x25519DH(aliceEphemeral.secretKey, bobBundle.oneTimePreKey.publicKey);
    dhInput = concatBytes(dhInput, dh4);
  }

  const sharedSecret = hkdfSha256(dhInput, HKDF_SALT, X3DH_INFO, DERIVED_LENGTH);
  return {
    sharedSecret,
    ephemeralKey: aliceEphemeral.publicKey,
    usedOneTimePreKeyId: bobBundle.oneTimePreKey?.keyId,
  };
}

// ────────────────────────────────────────────────────────────────────────────
// Bob side — session init on first incoming message
// ────────────────────────────────────────────────────────────────────────────

export type BobX3DHInput = {
  bobIdentity: IdentityKeyPair;
  bobSignedPreKey: SignedPreKey;
  bobOneTimePreKey?: OneTimePreKey;
  aliceIdentityPublic: Uint8Array; // 32B Ed25519 public (from bootstrap header)
  aliceEphemeralPublic: Uint8Array; // 32B X25519 public (from bootstrap header)
};

export function x3dhInitAsBob(input: BobX3DHInput): Uint8Array {
  const bobIdX25519Secret = ed25519SecretToX25519(input.bobIdentity.secretKey);
  const aliceIdX25519Public = ed25519PublicToX25519(input.aliceIdentityPublic);

  const dh1 = x25519DH(input.bobSignedPreKey.keyPair.secretKey, aliceIdX25519Public);
  const dh2 = x25519DH(bobIdX25519Secret, input.aliceEphemeralPublic);
  const dh3 = x25519DH(input.bobSignedPreKey.keyPair.secretKey, input.aliceEphemeralPublic);
  let dhInput = concatBytes(F_PREFIX, dh1, dh2, dh3);
  if (input.bobOneTimePreKey) {
    const dh4 = x25519DH(input.bobOneTimePreKey.keyPair.secretKey, input.aliceEphemeralPublic);
    dhInput = concatBytes(dhInput, dh4);
  }

  return hkdfSha256(dhInput, HKDF_SALT, X3DH_INFO, DERIVED_LENGTH);
}
