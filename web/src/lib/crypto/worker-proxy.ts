// Worker proxy for the crypto layer.
//
// PHASE 7.0 STEP 1 STATUS: this is a main-thread shim that directly
// imports the crypto modules. The public API is already the async-
// returning shape a real Worker would expose, so Step 3 of Phase 7 can
// swap the implementation to a `new Worker(...)` + postMessage loop
// without touching any callers.
//
// Reason for the shim: generating 100 one-time pre-keys + full X3DH
// bootstrap completes in ~50ms in the main thread on a modern laptop,
// which is fine for the initial enrollment path. Moving to a real worker
// is a polish/UX step, not a correctness requirement.

import {
  buildEnrollmentBundle,
  getOrCreateIdentity,
  generateOneTimePreKeyBatch,
  generateSignedPreKey,
  needsOneTimePreKeyReplenishment,
  needsSignedPreKeyRotation,
  type EnrollmentBundle,
} from './device-manager';
import {
  decryptIncoming,
  encryptForPeers,
  type DecryptedMessage,
  type EncryptTarget,
} from './message-crypto';
import { computeSafetyNumber } from './safety-numbers';
import type {
  IdentityKeyPair,
  MessageEnvelope,
  OneTimePreKey,
  SignedPreKey,
} from './types';

export type CryptoWorker = {
  getOrCreateIdentity(): Promise<{
    deviceId: string;
    identityKeyPair: IdentityKeyPair;
    isNew: boolean;
  }>;
  buildEnrollmentBundle(): Promise<EnrollmentBundle>;
  rotateSignedPreKey(identity: IdentityKeyPair): Promise<SignedPreKey>;
  replenishOneTimePreKeys(): Promise<OneTimePreKey[]>;
  needsSignedPreKeyRotation(): Promise<boolean>;
  needsOneTimePreKeyReplenishment(): Promise<boolean>;

  encryptForPeers(
    senderDeviceId: string,
    senderIdentity: IdentityKeyPair,
    plaintext: Uint8Array,
    targets: readonly EncryptTarget[],
  ): Promise<MessageEnvelope>;
  decryptIncoming(
    ownDeviceId: string,
    ownIdentity: IdentityKeyPair,
    senderUserId: string,
    envelope: MessageEnvelope,
  ): Promise<DecryptedMessage | undefined>;

  computeSafetyNumber(
    userA: string,
    identityA: Uint8Array,
    userB: string,
    identityB: Uint8Array,
  ): Promise<string>;
};

let instance: CryptoWorker | undefined;

/**
 * Lazy singleton accessor — importing this module does not touch IndexedDB
 * or any crypto primitives. The first call to `getCryptoWorker()` triggers
 * the lazy import of the concrete implementation.
 */
export function getCryptoWorker(): CryptoWorker {
  if (instance) return instance;
  instance = {
    getOrCreateIdentity,
    buildEnrollmentBundle,
    rotateSignedPreKey: (identity) => generateSignedPreKey(identity),
    replenishOneTimePreKeys: () => generateOneTimePreKeyBatch(),
    needsSignedPreKeyRotation,
    needsOneTimePreKeyReplenishment,
    encryptForPeers,
    decryptIncoming,
    computeSafetyNumber: async (userA, identityA, userB, identityB) =>
      computeSafetyNumber(userA, identityA, userB, identityB),
  };
  return instance;
}
