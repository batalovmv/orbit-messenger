// Session lifecycle — load or create a Double Ratchet state for a given
// (peerUserId, peerDeviceId) pair, and persist back after each operation.

import {
  deserializeRatchetState,
  initRatchetAsAlice,
  initRatchetAsBob,
  serializeRatchetState,
} from './double-ratchet';
import { generateX25519KeyPair } from './primitives';
import {
  getSession,
  putSession,
  getOneTimePreKey,
  getSignedPreKey,
  deleteOneTimePreKey,
} from './key-store';
import { x3dhInitAsAlice, x3dhInitAsBob } from './x3dh';
import type {
  BootstrapHeader,
  IdentityKeyPair,
  KeyPair,
  PreKeyBundle,
  RatchetState,
} from './types';

/**
 * Load an existing session from disk, or return undefined. Used by receive
 * path to check whether a session already exists before running Bob's
 * bootstrap.
 */
export async function loadSession(
  peerUserId: string,
  peerDeviceId: string,
): Promise<RatchetState | undefined> {
  const blob = await getSession(peerUserId, peerDeviceId);
  if (!blob) return undefined;
  return deserializeRatchetState(blob);
}

export async function persistSession(
  peerUserId: string,
  peerDeviceId: string,
  state: RatchetState,
): Promise<void> {
  await putSession(peerUserId, peerDeviceId, serializeRatchetState(state));
}

/**
 * Bootstrap a new session on Alice's side from Bob's bundle. Returns both
 * the fresh ratchet state and the bootstrap header that Alice must attach
 * to her first outgoing message so Bob can reconstruct the session.
 */
export async function bootstrapSessionAsAlice(
  aliceIdentity: IdentityKeyPair,
  bobBundle: PreKeyBundle,
): Promise<{ state: RatchetState; bootstrap: BootstrapHeader }> {
  const aliceEphemeral = generateX25519KeyPair();
  const x3dh = x3dhInitAsAlice(aliceIdentity, aliceEphemeral, bobBundle);

  const state = initRatchetAsAlice(x3dh.sharedSecret, bobBundle.signedPreKey);

  const bootstrap: BootstrapHeader = {
    aliceIdentityKey: aliceIdentity.publicKey,
    aliceEphemeralKey: aliceEphemeral.publicKey,
    bobSignedPreKeyId: bobBundle.signedPreKeyId,
    bobOneTimePreKeyId: bobBundle.oneTimePreKey?.keyId,
  };
  return { state, bootstrap };
}

/**
 * Bootstrap a new session on Bob's side from an incoming bootstrap header.
 * Looks up the referenced signed pre-key + (optional) one-time pre-key from
 * local storage, runs Bob-side X3DH, and initializes the ratchet state. The
 * one-time pre-key, if used, is deleted after successful bootstrap.
 */
export async function bootstrapSessionAsBob(
  bobIdentity: IdentityKeyPair,
  bootstrap: BootstrapHeader,
): Promise<RatchetState> {
  const signedPreKey = await getSignedPreKey(bootstrap.bobSignedPreKeyId);
  if (!signedPreKey) {
    throw new Error(
      `session: unknown signed pre-key id ${bootstrap.bobSignedPreKeyId} referenced by peer`,
    );
  }

  let oneTimeKeyPair: KeyPair | undefined;
  if (bootstrap.bobOneTimePreKeyId !== undefined) {
    const otp = await getOneTimePreKey(bootstrap.bobOneTimePreKeyId);
    if (!otp) {
      throw new Error(
        `session: unknown one-time pre-key id ${bootstrap.bobOneTimePreKeyId}`,
      );
    }
    oneTimeKeyPair = otp.keyPair;
  }

  const sharedSecret = x3dhInitAsBob({
    bobIdentity,
    bobSignedPreKey: signedPreKey,
    bobOneTimePreKey: oneTimeKeyPair
      ? { keyId: bootstrap.bobOneTimePreKeyId!, keyPair: oneTimeKeyPair }
      : undefined,
    aliceIdentityPublic: bootstrap.aliceIdentityKey,
    aliceEphemeralPublic: bootstrap.aliceEphemeralKey,
  });

  if (bootstrap.bobOneTimePreKeyId !== undefined) {
    // One-time pre-keys must be consumed exactly once.
    await deleteOneTimePreKey(bootstrap.bobOneTimePreKeyId);
  }

  return initRatchetAsBob(sharedSecret, signedPreKey.keyPair);
}
