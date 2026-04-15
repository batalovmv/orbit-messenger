// Top-level encrypt/decrypt API used by the Saturn-layer send/receive
// pipeline (Step 4 of Phase 7). This is the only surface area the rest of
// the app should depend on — everything else in `lib/crypto/` is internal.
//
// Function contract:
//
//   encryptForPeers(senderDeviceId, senderIdentity, plaintextUtf8, targets)
//     → MessageEnvelope suitable for `sendEncryptedMessage` backend endpoint.
//
//   decryptIncoming(ownDeviceId, ownIdentity, envelope)
//     → { plaintext, senderDeviceId } or undefined if the envelope doesn't
//       target this device.

import { decodeEntry, encodeEntry } from './envelope';
import {
  ratchetDecrypt,
  ratchetEncrypt,
  type SkippedKeys,
} from './double-ratchet';
import {
  bootstrapSessionAsAlice,
  bootstrapSessionAsBob,
  loadSession,
  persistSession,
} from './session-manager';
import type {
  BootstrapHeader,
  IdentityKeyPair,
  MessageEnvelope,
  PreKeyBundle,
  RatchetMessage,
} from './types';
import { EnvelopeType } from './types';

// In-memory per-session skipped key store. Keyed by `peerUserId:peerDeviceId`.
// Lost on tab reload — acceptable for Phase 7.0 since messages generally
// arrive in order over NATS → WS.
const skippedPerSession: Map<string, SkippedKeys> = new Map();

function sessionKey(peerUserId: string, peerDeviceId: string): string {
  return `${peerUserId}:${peerDeviceId}`;
}

function getSkippedKeys(peerUserId: string, peerDeviceId: string): SkippedKeys {
  const key = sessionKey(peerUserId, peerDeviceId);
  let entry = skippedPerSession.get(key);
  if (!entry) {
    entry = new Map();
    skippedPerSession.set(key, entry);
  }
  return entry;
}

export type EncryptTarget = {
  peerUserId: string;
  peerDeviceId: string;
  // Bundle is required when no session exists yet. Ignored if session
  // already present locally.
  bundle?: PreKeyBundle;
};

/**
 * Encrypt a plaintext message for a set of target devices (both peer
 * devices and the sender's own other devices).
 *
 * Produces a single `MessageEnvelope` with one entry per target.
 */
export async function encryptForPeers(
  senderDeviceId: string,
  senderIdentity: IdentityKeyPair,
  plaintext: Uint8Array,
  targets: readonly EncryptTarget[],
): Promise<MessageEnvelope> {
  if (targets.length === 0) {
    throw new Error('encryptForPeers: at least one target device required');
  }

  const envelope: MessageEnvelope = {
    v: 1,
    sender_device_id: senderDeviceId,
    devices: {},
  };

  for (const target of targets) {
    let state = await loadSession(target.peerUserId, target.peerDeviceId);
    let bootstrap: BootstrapHeader | undefined;

    if (!state) {
      if (!target.bundle) {
        throw new Error(
          `encryptForPeers: no local session for ${target.peerUserId}:${target.peerDeviceId} and no bundle provided`,
        );
      }
      const bootstrapResult = await bootstrapSessionAsAlice(senderIdentity, target.bundle);
      state = bootstrapResult.state;
      bootstrap = bootstrapResult.bootstrap;
    }

    const { header, ciphertext } = ratchetEncrypt(state, plaintext);
    const msg: RatchetMessage = {
      type: bootstrap ? EnvelopeType.PreKey : EnvelopeType.Normal,
      header: { ...header, bootstrap },
      ciphertext,
    };
    envelope.devices[target.peerDeviceId] = encodeEntry(msg);
    await persistSession(target.peerUserId, target.peerDeviceId, state);
  }

  return envelope;
}

export type DecryptedMessage = {
  plaintext: Uint8Array;
  senderDeviceId: string;
};

/**
 * Decrypt an incoming envelope for this device. Returns undefined if the
 * envelope does not contain an entry for our own device id (e.g. sender
 * excluded us).
 *
 * Caller is responsible for passing in the sender's `peerUserId` since the
 * envelope only carries `sender_device_id`.
 */
export async function decryptIncoming(
  ownDeviceId: string,
  ownIdentity: IdentityKeyPair,
  senderUserId: string,
  envelope: MessageEnvelope,
): Promise<DecryptedMessage | undefined> {
  const entry = envelope.devices[ownDeviceId];
  if (!entry) return undefined;

  const ratchetMsg = decodeEntry(entry);
  const senderDeviceId = envelope.sender_device_id;

  let state = await loadSession(senderUserId, senderDeviceId);

  if (!state) {
    if (!ratchetMsg.header.bootstrap || ratchetMsg.type !== EnvelopeType.PreKey) {
      throw new Error(
        'decryptIncoming: no session and no bootstrap header — cannot decrypt',
      );
    }
    state = await bootstrapSessionAsBob(ownIdentity, ratchetMsg.header.bootstrap);
  }

  const skipped = getSkippedKeys(senderUserId, senderDeviceId);
  const plaintext = ratchetDecrypt(state, skipped, ratchetMsg.header, ratchetMsg.ciphertext);
  await persistSession(senderUserId, senderDeviceId, state);
  return { plaintext, senderDeviceId };
}
