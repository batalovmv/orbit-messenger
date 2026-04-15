// Device enrollment orchestration (Phase 7 Step 3).
//
// Called as fire-and-forget after a successful auth (login, register, or
// session refresh). Lazily imports the crypto worker + Saturn key methods
// — the initial auth screen never loads any of this code, so the bundle
// impact is gated behind a successful login.
//
// Flow on a fresh device (no IndexedDB identity):
//   1. Generate Ed25519 identity key pair + device UUID.
//   2. Generate signed pre-key, sign with identity.
//   3. Generate 100 one-time pre-keys.
//   4. Upload identity + signed pre-key in one call.
//   5. Upload one-time pre-key batch in a second call.
//
// Flow on a re-login (identity already in IDB):
//   1. Verify server-side one-time pre-key count. If < 20, replenish with
//      a fresh batch of 100.
//   2. If signed pre-key is older than 7 days, rotate it.
//
// Errors are logged as warnings only. Enrollment failure does NOT break
// the auth flow — a user whose crypto layer is broken can still use
// non-E2E chats.

import type { CryptoWorker } from './worker-proxy';
import type { IdentityKeyPair } from './types';

type KeysApi = typeof import('../../api/saturn/methods/keys');

// Swappable for tests — jest.spyOn the exports below and the runner
// picks up the mocks.
export const enrollmentDeps = {
  loadWorker: async (): Promise<CryptoWorker> => {
    const { getCryptoWorker } = await import('./worker-proxy');
    return getCryptoWorker();
  },
  loadKeysApi: async (): Promise<KeysApi> => import('../../api/saturn/methods/keys'),
  loadKeyStore: async () => import('./key-store'),
};

let currentRun: Promise<void> | undefined;

/**
 * Ensure this device is enrolled and its pre-key pools are topped up.
 * Safe to call on every auth success — idempotent, cheap when already
 * enrolled.
 *
 * Returns immediately if an enrollment run is already in progress, so
 * concurrent auth events do not stampede the key endpoints.
 */
export function ensureEnrollment(): Promise<void> {
  if (currentRun) return currentRun;
  currentRun = runEnrollment().finally(() => {
    currentRun = undefined;
  });
  return currentRun;
}

async function runEnrollment(): Promise<void> {
  try {
    const [crypto, keysApi] = await Promise.all([
      enrollmentDeps.loadWorker(),
      enrollmentDeps.loadKeysApi(),
    ]);

    const identity = await crypto.getOrCreateIdentity();

    if (identity.isNew) {
      await firstTimeEnrollment(crypto, keysApi, identity.deviceId);
      return;
    }

    await rotateIfStale(crypto, keysApi, identity.deviceId, identity.identityKeyPair);
    await replenishIfLow(crypto, keysApi, identity.deviceId);
  } catch (err) {
    // eslint-disable-next-line no-console
    console.warn('[crypto] enrollment failed — E2E features will be unavailable', err);
  }
}

async function firstTimeEnrollment(
  crypto: CryptoWorker,
  keysApi: KeysApi,
  deviceId: string,
): Promise<void> {
  const bundle = await crypto.buildEnrollmentBundle();

  await keysApi.uploadIdentityKey({
    identityKey: bundle.identityPublic,
    signedPreKey: bundle.signedPreKey.publicKey,
    signedPreKeySignature: bundle.signedPreKey.signature,
    signedPreKeyId: bundle.signedPreKey.keyId,
    deviceId,
  });

  if (bundle.oneTimePreKeys.length > 0) {
    await keysApi.uploadOneTimePreKeys({
      keys: bundle.oneTimePreKeys.map((k) => ({ keyId: k.keyId, publicKey: k.publicKey })),
      deviceId,
    });
  }
}

async function rotateIfStale(
  crypto: CryptoWorker,
  keysApi: KeysApi,
  deviceId: string,
  identityKeyPair: IdentityKeyPair,
): Promise<void> {
  const stale = await crypto.needsSignedPreKeyRotation();
  if (!stale) return;

  const signed = await crypto.rotateSignedPreKey(identityKeyPair);
  await keysApi.uploadSignedPreKey({
    signedPreKey: signed.keyPair.publicKey,
    signedPreKeySignature: signed.signature,
    signedPreKeyId: signed.keyId,
    deviceId,
  });
}

async function replenishIfLow(
  crypto: CryptoWorker,
  keysApi: KeysApi,
  deviceId: string,
): Promise<void> {
  // Prefer the server-side count — that's the authoritative number of
  // keys still claimable by peers. Local IndexedDB count lags because we
  // only delete on consumption by Bob.
  let serverCount: number;
  try {
    serverCount = await keysApi.fetchPreKeyCount(deviceId);
  } catch {
    // If the count call fails, fall back to local count so we can still
    // replenish when obviously needed.
    const store = await enrollmentDeps.loadKeyStore();
    serverCount = await store.countOneTimePreKeys();
  }
  if (serverCount >= 20) return;

  const batch = await crypto.replenishOneTimePreKeys();
  if (batch.length === 0) return;
  await keysApi.uploadOneTimePreKeys({
    keys: batch.map((k) => ({ keyId: k.keyId, publicKey: k.keyPair.publicKey })),
    deviceId,
  });
}
