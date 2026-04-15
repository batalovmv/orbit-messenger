// Device enrollment orchestration — pure crypto + key-store logic, no
// network. The caller (Saturn-layer action handler, Step 3 of Phase 7) is
// responsible for pushing the generated public material to the server.
//
// This module:
//
//   * lazily generates a device UUID
//   * generates an Ed25519 identity key pair on first use
//   * generates + stores a signed pre-key signed by identity
//   * generates + stores a batch of one-time pre-keys
//   * reports which public material needs to be uploaded

import { randomUUID } from './random-uuid';
import {
  generateEd25519KeyPair,
  generateX25519KeyPair,
  signEd25519,
} from './primitives';
import {
  countOneTimePreKeys,
  getIdentity,
  getLatestSignedPreKey,
  putIdentity,
  putOneTimePreKey,
  putSignedPreKey,
} from './key-store';
import type {
  IdentityKeyPair,
  OneTimePreKey,
  SignedPreKey,
} from './types';

export const LOW_PREKEY_THRESHOLD = 20;
export const PREKEY_BATCH_SIZE = 100;
export const SIGNED_PREKEY_ROTATE_INTERVAL_MS = 7 * 24 * 60 * 60 * 1000; // 7 days

/**
 * Returns the long-term identity record for this device, generating one on
 * first call. Safe to call repeatedly — subsequent calls return the cached
 * value.
 */
export async function getOrCreateIdentity(): Promise<{
  deviceId: string;
  identityKeyPair: IdentityKeyPair;
  isNew: boolean;
}> {
  const existing = await getIdentity();
  if (existing) {
    return {
      deviceId: existing.deviceId,
      identityKeyPair: existing.identityKeyPair,
      isNew: false,
    };
  }
  const identityKeyPair = generateEd25519KeyPair();
  const deviceId = randomUUID();
  await putIdentity({
    deviceId,
    identityKeyPair,
    createdAt: Date.now(),
  });
  return { deviceId, identityKeyPair, isNew: true };
}

/**
 * Generates and stores the next signed pre-key, signing its public key with
 * the identity secret. Returns the stored record (public parts are what the
 * caller uploads to the server).
 */
export async function generateSignedPreKey(
  identity: IdentityKeyPair,
  nextKeyId?: number,
): Promise<SignedPreKey> {
  const latest = await getLatestSignedPreKey();
  const keyId = nextKeyId ?? (latest ? latest.keyId + 1 : 1);
  const keyPair = generateX25519KeyPair();
  const signature = signEd25519(keyPair.publicKey, identity.secretKey);
  const record: SignedPreKey = {
    keyId,
    keyPair,
    signature,
    createdAt: Date.now(),
  };
  await putSignedPreKey(record);
  return record;
}

/**
 * Generates and stores a batch of one-time pre-keys. Returns the stored
 * records — the caller uploads only public parts.
 */
export async function generateOneTimePreKeyBatch(
  count = PREKEY_BATCH_SIZE,
  startingKeyId?: number,
): Promise<OneTimePreKey[]> {
  const base = startingKeyId ?? Math.floor(Date.now() / 1000);
  const result: OneTimePreKey[] = [];
  for (let i = 0; i < count; i++) {
    const record: OneTimePreKey = {
      keyId: base + i,
      keyPair: generateX25519KeyPair(),
    };
    await putOneTimePreKey(record);
    result.push(record);
  }
  return result;
}

/**
 * Determine whether the signed pre-key needs rotation (older than 7 days or
 * absent).
 */
export async function needsSignedPreKeyRotation(): Promise<boolean> {
  const latest = await getLatestSignedPreKey();
  if (!latest) return true;
  return Date.now() - latest.createdAt > SIGNED_PREKEY_ROTATE_INTERVAL_MS;
}

/**
 * Determine whether the one-time pre-key pool needs replenishing. Pool is
 * considered low at <20 keys per design spec.
 */
export async function needsOneTimePreKeyReplenishment(): Promise<boolean> {
  const count = await countOneTimePreKeys();
  return count < LOW_PREKEY_THRESHOLD;
}

/**
 * Full enrollment bundle — what the caller needs to upload to the server
 * immediately after auth success on a brand-new device. All public material
 * is returned raw (Uint8Array); Saturn layer base64url-encodes before HTTP.
 */
export type EnrollmentBundle = {
  deviceId: string;
  identityPublic: Uint8Array;
  signedPreKey: {
    keyId: number;
    publicKey: Uint8Array;
    signature: Uint8Array;
  };
  oneTimePreKeys: Array<{
    keyId: number;
    publicKey: Uint8Array;
  }>;
};

export async function buildEnrollmentBundle(): Promise<EnrollmentBundle> {
  const { deviceId, identityKeyPair } = await getOrCreateIdentity();
  const signed = await generateSignedPreKey(identityKeyPair);
  const oneTimes = await generateOneTimePreKeyBatch();
  return {
    deviceId,
    identityPublic: identityKeyPair.publicKey,
    signedPreKey: {
      keyId: signed.keyId,
      publicKey: signed.keyPair.publicKey,
      signature: signed.signature,
    },
    oneTimePreKeys: oneTimes.map((k) => ({
      keyId: k.keyId,
      publicKey: k.keyPair.publicKey,
    })),
  };
}
