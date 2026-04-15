// Double Ratchet implementation — simplified variant tuned for Orbit.
//
// Reference: https://signal.org/docs/specifications/doubleratchet/
//
// Design notes:
//
// * We implement the core DH+symmetric ratchet plus a small skipped-message
//   cache so out-of-order delivery inside a single chain is handled. We do
//   not persist skipped keys across sessions (they live in RAM until the
//   next DH ratchet step). For Phase 7.0 this is acceptable: NATS → WS
//   delivers messages in order per chat.
//
// * Root key advances on every DH ratchet step via HKDF.
// * Chain keys advance on every message via HMAC-like hash.
// * Message keys are 32B AES-256 keys derived from the chain key.
//
// CRITICAL SECURITY NOTE: every message key is used exactly once. Never
// reuse nonce/key combinations.

import {
  aes256GcmDecrypt,
  aes256GcmEncrypt,
  AES_NONCE_LENGTH,
  concatBytes,
  generateX25519KeyPair,
  hkdfSha256,
  x25519DH,
} from './primitives';
import type { KeyPair, RatchetHeader, RatchetState } from './types';

const ROOT_KDF_INFO = new TextEncoder().encode('OrbitRatchetRoot');
const CHAIN_KDF_CONST_MESSAGE = new Uint8Array([0x01]);
const CHAIN_KDF_CONST_CHAIN = new Uint8Array([0x02]);
const MESSAGE_KEY_INFO = new TextEncoder().encode('OrbitRatchetMsg');
const MAX_SKIPPED_MESSAGES = 1024;

// In-memory per-session skipped message store keyed by
// `${base64(dhPub)}:${messageNumber}` → messageKey.
export type SkippedKeys = Map<string, Uint8Array>;

// ────────────────────────────────────────────────────────────────────────────
// KDFs
// ────────────────────────────────────────────────────────────────────────────

// Root KDF: HKDF(RK, DH_output) → (new RK, new CK). Returns 64 bytes split.
function kdfRoot(rootKey: Uint8Array, dhOutput: Uint8Array): {
  rootKey: Uint8Array;
  chainKey: Uint8Array;
} {
  const out = hkdfSha256(dhOutput, rootKey, ROOT_KDF_INFO, 64);
  return { rootKey: out.slice(0, 32), chainKey: out.slice(32, 64) };
}

// Chain KDF: derives the next chain key and a message key from the current
// chain key. Uses HKDF with two different info bytes for domain separation.
function kdfChainStep(chainKey: Uint8Array): {
  nextChainKey: Uint8Array;
  messageKey: Uint8Array;
} {
  const nextChainKey = hkdfSha256(chainKey, new Uint8Array(0), CHAIN_KDF_CONST_CHAIN, 32);
  const messageKey = hkdfSha256(chainKey, new Uint8Array(0), CHAIN_KDF_CONST_MESSAGE, 32);
  return { nextChainKey, messageKey };
}

// Derive the AES key + nonce from a message key.
function deriveMessageKey(messageKey: Uint8Array): { aesKey: Uint8Array; nonce: Uint8Array } {
  const out = hkdfSha256(messageKey, new Uint8Array(0), MESSAGE_KEY_INFO, 32 + AES_NONCE_LENGTH);
  return { aesKey: out.slice(0, 32), nonce: out.slice(32, 32 + AES_NONCE_LENGTH) };
}

// ────────────────────────────────────────────────────────────────────────────
// State construction
// ────────────────────────────────────────────────────────────────────────────

/**
 * Initialize ratchet state on Alice's side after X3DH. Alice starts with a
 * fresh DH keypair and an initial root key from X3DH. She does not have
 * Bob's ratchet public key yet; she'll obtain it after Bob's first reply.
 *
 * Alice must be able to encrypt at least one message (the bootstrap one) before
 * receiving any reply, so we seed a sending chain directly by ratcheting the
 * root key against a DH(sendingKey, bobSignedPreKey) output.
 */
export function initRatchetAsAlice(
  sharedSecret: Uint8Array,
  bobSignedPreKeyPublic: Uint8Array,
): RatchetState {
  const sendingKeyPair = generateX25519KeyPair();
  const dhOut = x25519DH(sendingKeyPair.secretKey, bobSignedPreKeyPublic);
  const { rootKey, chainKey } = kdfRoot(sharedSecret, dhOut);

  return {
    rootKey,
    sendingKeyPair,
    receivingKey: bobSignedPreKeyPublic,
    sendingChainKey: chainKey,
    receivingChainKey: undefined,
    sendingMessageNumber: 0,
    receivingMessageNumber: 0,
    previousSendingChainLength: 0,
    hasSentFirstMessage: false,
  };
}

/**
 * Initialize ratchet state on Bob's side. Bob has a signed prekey pair that
 * was used on Alice's X3DH, so his initial sending key pair IS the signed
 * prekey pair (not a fresh one) — that way Alice's first DH computation
 * aligns with Bob's ratchet root seeding.
 */
export function initRatchetAsBob(
  sharedSecret: Uint8Array,
  bobSignedPreKeyPair: KeyPair,
): RatchetState {
  return {
    rootKey: sharedSecret,
    sendingKeyPair: bobSignedPreKeyPair,
    receivingKey: undefined,
    sendingChainKey: undefined,
    receivingChainKey: undefined,
    sendingMessageNumber: 0,
    receivingMessageNumber: 0,
    previousSendingChainLength: 0,
    hasSentFirstMessage: false,
  };
}

// ────────────────────────────────────────────────────────────────────────────
// Encryption
// ────────────────────────────────────────────────────────────────────────────

export type EncryptOutput = {
  header: RatchetHeader;
  ciphertext: Uint8Array;
};

export function ratchetEncrypt(state: RatchetState, plaintext: Uint8Array): EncryptOutput {
  if (!state.sendingChainKey) {
    throw new Error('ratchet: cannot encrypt before a sending chain exists');
  }
  const { nextChainKey, messageKey } = kdfChainStep(state.sendingChainKey);
  const { aesKey, nonce } = deriveMessageKey(messageKey);
  const ciphertext = aes256GcmEncrypt(aesKey, nonce, plaintext);

  const header: RatchetHeader = {
    dhPublicKey: state.sendingKeyPair.publicKey,
    messageNumber: state.sendingMessageNumber,
    previousChainLength: state.previousSendingChainLength,
  };

  state.sendingChainKey = nextChainKey;
  state.sendingMessageNumber += 1;
  state.hasSentFirstMessage = true;

  return { header, ciphertext };
}

// ────────────────────────────────────────────────────────────────────────────
// DH ratchet step
// ────────────────────────────────────────────────────────────────────────────

// Called whenever we receive a message with a different DH public key than
// the one we currently track. Advances the root key twice: once against the
// received key (seeds receiving chain), and once against our new fresh DH
// key (seeds new sending chain).
function dhRatchetStep(state: RatchetState, remoteDhKey: Uint8Array): void {
  state.previousSendingChainLength = state.sendingMessageNumber;
  state.sendingMessageNumber = 0;
  state.receivingMessageNumber = 0;
  state.receivingKey = remoteDhKey;

  // Receiving chain seed: DH(our old send secret, their new pub).
  const dhRecv = x25519DH(state.sendingKeyPair.secretKey, remoteDhKey);
  const recvDerived = kdfRoot(state.rootKey, dhRecv);
  state.rootKey = recvDerived.rootKey;
  state.receivingChainKey = recvDerived.chainKey;

  // Fresh sending key pair → new sending chain seed: DH(new send secret, their new pub).
  state.sendingKeyPair = generateX25519KeyPair();
  const dhSend = x25519DH(state.sendingKeyPair.secretKey, remoteDhKey);
  const sendDerived = kdfRoot(state.rootKey, dhSend);
  state.rootKey = sendDerived.rootKey;
  state.sendingChainKey = sendDerived.chainKey;
}

// ────────────────────────────────────────────────────────────────────────────
// Decryption
// ────────────────────────────────────────────────────────────────────────────

function base64KeyLabel(dhPub: Uint8Array): string {
  // Cheap stringification — we only use this for in-memory Map keys.
  let s = '';
  for (let i = 0; i < dhPub.length; i++) s += dhPub[i].toString(16).padStart(2, '0');
  return s;
}

function skipMessageKeys(
  state: RatchetState,
  skipped: SkippedKeys,
  until: number,
): void {
  if (!state.receivingChainKey) return;
  if (until - state.receivingMessageNumber > MAX_SKIPPED_MESSAGES) {
    throw new Error('ratchet: too many skipped messages in chain');
  }
  const dhLabel = state.receivingKey ? base64KeyLabel(state.receivingKey) : '';
  while (state.receivingMessageNumber < until) {
    const { nextChainKey, messageKey } = kdfChainStep(state.receivingChainKey);
    skipped.set(`${dhLabel}:${state.receivingMessageNumber}`, messageKey);
    state.receivingChainKey = nextChainKey;
    state.receivingMessageNumber += 1;
  }
}

function tryDecryptWithSkipped(
  skipped: SkippedKeys,
  header: RatchetHeader,
  ciphertext: Uint8Array,
): Uint8Array | undefined {
  const label = base64KeyLabel(header.dhPublicKey);
  const key = `${label}:${header.messageNumber}`;
  const messageKey = skipped.get(key);
  if (!messageKey) return undefined;
  skipped.delete(key);
  const { aesKey, nonce } = deriveMessageKey(messageKey);
  return aes256GcmDecrypt(aesKey, nonce, ciphertext);
}

export function ratchetDecrypt(
  state: RatchetState,
  skipped: SkippedKeys,
  header: RatchetHeader,
  ciphertext: Uint8Array,
): Uint8Array {
  // Try skipped keys from a previous chain first.
  const viaSkipped = tryDecryptWithSkipped(skipped, header, ciphertext);
  if (viaSkipped) return viaSkipped;

  // If incoming DH key is different from the one tracked → DH ratchet step.
  const trackingDifferentKey = !state.receivingKey
    || !bytesEqual(state.receivingKey, header.dhPublicKey);
  if (trackingDifferentKey) {
    // Skip any leftover messages from the previous receiving chain.
    if (state.receivingKey && state.receivingChainKey) {
      skipMessageKeys(state, skipped, header.previousChainLength);
    }
    dhRatchetStep(state, header.dhPublicKey);
  }

  // Skip inside the current chain if needed.
  skipMessageKeys(state, skipped, header.messageNumber);

  if (!state.receivingChainKey) {
    throw new Error('ratchet: no receiving chain key after DH step');
  }
  const { nextChainKey, messageKey } = kdfChainStep(state.receivingChainKey);
  state.receivingChainKey = nextChainKey;
  state.receivingMessageNumber += 1;
  const { aesKey, nonce } = deriveMessageKey(messageKey);
  return aes256GcmDecrypt(aesKey, nonce, ciphertext);
}

function bytesEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

// ────────────────────────────────────────────────────────────────────────────
// Serialization — opaque blob stored in IndexedDB
// ────────────────────────────────────────────────────────────────────────────

// Binary layout (all big-endian u32, little-endian key bytes):
//
//   32B rootKey
//   32B sendingPub
//   32B sendingSec
//   u8  hasReceivingKey
//   [32B receivingKey]
//   u8  hasSendingChain
//   [32B sendingChainKey]
//   u8  hasReceivingChain
//   [32B receivingChainKey]
//   u32 sendingMessageNumber
//   u32 receivingMessageNumber
//   u32 previousSendingChainLength
//   u8  hasSentFirstMessage

export function serializeRatchetState(state: RatchetState): Uint8Array {
  const parts: Uint8Array[] = [];
  parts.push(state.rootKey);
  parts.push(state.sendingKeyPair.publicKey);
  parts.push(state.sendingKeyPair.secretKey);
  parts.push(new Uint8Array([state.receivingKey ? 1 : 0]));
  if (state.receivingKey) parts.push(state.receivingKey);
  parts.push(new Uint8Array([state.sendingChainKey ? 1 : 0]));
  if (state.sendingChainKey) parts.push(state.sendingChainKey);
  parts.push(new Uint8Array([state.receivingChainKey ? 1 : 0]));
  if (state.receivingChainKey) parts.push(state.receivingChainKey);

  const counters = new Uint8Array(12);
  const view = new DataView(counters.buffer);
  view.setUint32(0, state.sendingMessageNumber >>> 0, false);
  view.setUint32(4, state.receivingMessageNumber >>> 0, false);
  view.setUint32(8, state.previousSendingChainLength >>> 0, false);
  parts.push(counters);
  parts.push(new Uint8Array([state.hasSentFirstMessage ? 1 : 0]));
  return concatBytes(...parts);
}

export function deserializeRatchetState(bytes: Uint8Array): RatchetState {
  let o = 0;
  const take = (n: number) => {
    const slice = bytes.slice(o, o + n);
    o += n;
    return slice;
  };

  const rootKey = take(32);
  const sendingPub = take(32);
  const sendingSec = take(32);

  const hasReceivingKey = bytes[o++] === 1;
  const receivingKey = hasReceivingKey ? take(32) : undefined;

  const hasSendingChain = bytes[o++] === 1;
  const sendingChainKey = hasSendingChain ? take(32) : undefined;

  const hasReceivingChain = bytes[o++] === 1;
  const receivingChainKey = hasReceivingChain ? take(32) : undefined;

  const view = new DataView(bytes.buffer, bytes.byteOffset + o, 12);
  const sendingMessageNumber = view.getUint32(0, false);
  const receivingMessageNumber = view.getUint32(4, false);
  const previousSendingChainLength = view.getUint32(8, false);
  o += 12;
  const hasSentFirstMessage = bytes[o++] === 1;

  return {
    rootKey,
    sendingKeyPair: { publicKey: sendingPub, secretKey: sendingSec },
    receivingKey,
    sendingChainKey,
    receivingChainKey,
    sendingMessageNumber,
    receivingMessageNumber,
    previousSendingChainLength,
    hasSentFirstMessage,
  };
}
