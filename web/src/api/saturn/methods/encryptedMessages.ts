// Orchestration for encrypted DM send/receive (Phase 7 Steps 4-5).
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

import type { MessageEnvelope, PreKeyBundle } from '../../../lib/crypto/types';

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
  targetDeviceCount: number;
};

export type FanoutTarget = {
  userId: string;
  deviceId: string;
  bundle: PreKeyBundle;
};

/**
 * Collects the list of devices we need to encrypt for:
 *
 *   * All peer devices (so every device the peer owns can decrypt).
 *   * All of our own devices other than this one, so message history
 *     stays readable on our other sessions (per Signal multi-device
 *     model, docs/SIGNAL_PROTOCOL.md §Device Model).
 *
 * Each entry gets its own fresh bundle fetch. `fetchKeyBundle` is the
 * only way to atomically consume a one-time pre-key on the server —
 * we never cache bundles between runs.
 *
 * **Backend limitation as of Phase 7.0 (Step 5):** the backend endpoint
 * `GET /keys/:userId/bundle` always returns the primary (first)
 * device row for that user, and does not accept a `?device_id=` query
 * param. So in practice:
 *
 *   * Peer fanout is currently 1 target even if the peer has multiple
 *     devices — whichever device is returned by the backend.
 *   * Own-other-device fanout only works when the primary device
 *     returned by the backend differs from this session's deviceId
 *     (e.g. we are on a secondary browser and the primary device is
 *     the one registered first).
 *
 * When the backend grows per-device bundle lookup the loop will
 * naturally expand — the envelope already supports N entries.
 */
export async function collectFanoutTargets(params: {
  peerUserId: string;
  ownUserId: string;
  ownDeviceId: string;
}): Promise<FanoutTarget[]> {
  const { peerUserId, ownUserId, ownDeviceId } = params;
  const keysApi = await loadKeysApi();

  const targets: FanoutTarget[] = [];
  const seen = new Set<string>();

  // 1. Peer target(s) — mandatory. Throw a typed error so the UI shows
  //    the "peer not enrolled" message if the bundle fetch fails.
  let peerBundle: PreKeyBundle;
  try {
    peerBundle = await keysApi.fetchKeyBundle(peerUserId);
  } catch (err) {
    throw new EncryptedSendError(
      'peer_not_enrolled',
      'Получатель ещё не настроил ключи шифрования. Попросите его открыть приложение — после первого входа сообщение можно будет отправить.',
      err,
    );
  }
  targets.push({ userId: peerUserId, deviceId: peerBundle.deviceId, bundle: peerBundle });
  seen.add(`${peerUserId}:${peerBundle.deviceId}`);

  // 2. Own-other-device target(s) — optional. Failure to reach the
  //    bundle for our own user is tolerated (we might be the only
  //    device, or the server might have hiccupped).
  try {
    const ownBundle = await keysApi.fetchKeyBundle(ownUserId);
    if (ownBundle.deviceId !== ownDeviceId) {
      const key = `${ownUserId}:${ownBundle.deviceId}`;
      if (!seen.has(key)) {
        targets.push({ userId: ownUserId, deviceId: ownBundle.deviceId, bundle: ownBundle });
        seen.add(key);
      }
    }
  } catch {
    // Silent: we just won't fan out to our other device. Messages
    // will still land on peer devices.
  }

  return targets;
}

/**
 * Encrypt a plaintext message for a DM peer and ship the envelope.
 *
 * Phase 7.0 scope: text-only, single-peer DMs; fan-out across both
 * peer devices and our own other devices via `collectFanoutTargets`.
 * Media encryption lands in Phase 7.1 (see design doc §14.1).
 */
export async function sendEncryptedTextMessage(params: {
  chatId: string;
  peerUserId: string;
  plaintext: string;
  ownUserId?: string;
}): Promise<EncryptedSendResult> {
  const { chatId, peerUserId, plaintext, ownUserId } = params;

  const [crypto, keysApi] = await Promise.all([loadCryptoWorker(), loadKeysApi()]);

  // Ensure our own identity exists; non-enrolled devices cannot send.
  const own = await crypto.getOrCreateIdentity();

  const targets = await collectFanoutTargets({
    peerUserId,
    // If caller did not supply `ownUserId` skip own-device fanout by
    // pointing the lookup at the peer (guaranteed to dedupe out).
    ownUserId: ownUserId ?? peerUserId,
    ownDeviceId: own.deviceId,
  });

  const envelope = await crypto.encryptForPeers(
    own.deviceId,
    own.identityKeyPair,
    new TextEncoder().encode(plaintext),
    targets.map((t) => ({
      peerUserId: t.userId,
      peerDeviceId: t.deviceId,
      bundle: t.bundle,
    })),
  );

  const result = await keysApi.sendEncryptedMessage({
    chatId,
    envelope,
    deviceId: own.deviceId,
  });
  return { messageId: result.id, targetDeviceCount: targets.length };
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
