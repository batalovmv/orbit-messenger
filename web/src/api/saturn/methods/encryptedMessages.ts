// Orchestration for encrypted DM send/receive (Phase 7 Step 4).
//
// Lives in the Saturn methods layer because it hops between three
// subsystems:
//
//   * Saturn HTTP methods (`keys.ts` — fetchKeyBundle, sendEncryptedMessage)
//   * Crypto layer (`lib/crypto/worker-proxy` — encryptForPeers / decryptIncoming)
//   * Local identity/device state from IndexedDB
//
// We keep it lazily-imported from the send/receive hot paths so non-E2E
// users never pull the crypto chunk into their bundle, and so anything
// outside this module only sees async promises.

import type { MessageEnvelope } from '../../../lib/crypto/types';

// All crypto work goes through the singleton worker (currently a
// main-thread shim — see docs/phase7-design.md §14.3).
async function loadCryptoWorker() {
  const { getCryptoWorker } = await import('../../../lib/crypto/worker-proxy');
  return getCryptoWorker();
}

async function loadKeysApi() {
  return import('./keys');
}

// ────────────────────────────────────────────────────────────────────────────
// SEND
// ────────────────────────────────────────────────────────────────────────────

export type EncryptedSendResult = {
  messageId: string;
};

/**
 * Encrypt a plaintext message for a DM peer and ship the envelope.
 *
 * Phase 7.0 scope: text-only, single-peer DMs. Multi-device fanout lands
 * in Step 5. Media encryption lands in Phase 7.1 (see design doc §14.1).
 */
export async function sendEncryptedTextMessage(params: {
  chatId: string;
  peerUserId: string;
  plaintext: string;
}): Promise<EncryptedSendResult> {
  const { chatId, peerUserId, plaintext } = params;

  const [crypto, keysApi] = await Promise.all([loadCryptoWorker(), loadKeysApi()]);

  // Ensure our own identity exists; non-enrolled devices cannot send.
  const own = await crypto.getOrCreateIdentity();

  // Fetch a fresh bundle — this call atomically consumes a one-time
  // pre-key on the server when the peer has any available. Store layer
  // decides whether to bootstrap a new session or reuse an existing one.
  let bundle;
  try {
    bundle = await keysApi.fetchKeyBundle(peerUserId);
  } catch (err) {
    throw new EncryptedSendError(
      'peer_not_enrolled',
      'Получатель ещё не настроил ключи шифрования. Попросите его открыть приложение — после первого входа сообщение можно будет отправить.',
      err,
    );
  }

  const envelope = await crypto.encryptForPeers(
    own.deviceId,
    own.identityKeyPair,
    new TextEncoder().encode(plaintext),
    [
      {
        peerUserId,
        peerDeviceId: bundle.deviceId,
        bundle,
      },
    ],
  );

  const result = await keysApi.sendEncryptedMessage({
    chatId,
    envelope,
    deviceId: own.deviceId,
  });
  return { messageId: result.id };
}

export class EncryptedSendError extends Error {
  readonly code: 'peer_not_enrolled' | 'internal';

  constructor(code: 'peer_not_enrolled' | 'internal', message: string, cause?: unknown) {
    super(message);
    this.name = 'EncryptedSendError';
    this.code = code;
    if (cause !== undefined) {
      (this as { cause?: unknown }).cause = cause;
    }
  }
}

// ────────────────────────────────────────────────────────────────────────────
// RECEIVE — envelope transport decoding
// ────────────────────────────────────────────────────────────────────────────

/**
 * The backend marshals `encrypted_content BYTEA` via Go's default
 * `[]byte → base64` encoder, which uses standard base64 (RFC 4648 §4,
 * with padding). Decode back to a UTF-8 JSON string and parse.
 */
export function decodeEncryptedContentField(serverField: string): MessageEnvelope {
  const binary = atob(serverField);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  const json = new TextDecoder().decode(bytes);
  const parsed = JSON.parse(json);
  if (!parsed || parsed.v !== 1 || typeof parsed.sender_device_id !== 'string') {
    throw new Error('decodeEncryptedContentField: not a v1 envelope');
  }
  return parsed as MessageEnvelope;
}

export type DecryptedIncoming = {
  plaintext: string;
  senderDeviceId: string;
};

/**
 * Decrypt an incoming envelope for this device. Returns `undefined` if
 * the envelope was not addressed to our own device id (sender excluded
 * us) — the UI should then render a "[зашифровано: не для этого
 * устройства]" placeholder.
 */
export async function decryptIncomingEnvelope(params: {
  senderUserId: string;
  envelope: MessageEnvelope;
}): Promise<DecryptedIncoming | undefined> {
  const { senderUserId, envelope } = params;
  const crypto = await loadCryptoWorker();
  const own = await crypto.getOrCreateIdentity();

  const result = await crypto.decryptIncoming(
    own.deviceId,
    own.identityKeyPair,
    senderUserId,
    envelope,
  );
  if (!result) return undefined;

  return {
    plaintext: new TextDecoder().decode(result.plaintext),
    senderDeviceId: result.senderDeviceId,
  };
}
