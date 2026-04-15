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

async function loadKeyStore() {
  return import('../../../lib/crypto/key-store');
}

// ────────────────────────────────────────────────────────────────────────────
// Identity pinning — Phase 7 Step 10 security fix
// ────────────────────────────────────────────────────────────────────────────
//
// Without a per-peer identity pin, a compromised server can substitute
// arbitrary identity keys in either the X3DH bundle (send side) or the
// bootstrap header of an incoming message (receive side), and make the
// client encrypt-to-attacker or accept-from-attacker without any visible
// signal. `verifyAndPinPeerIdentity` closes that gap:
//
//   1. If we already have a pinned identity for this user, compare the
//      claimed key against the pin. Mismatch → throw — caller surfaces a
//      Russian "ключи изменились" error in the UI.
//   2. Otherwise, fetch `GET /keys/:userId/identity` (independent of the
//      bundle call), compare the claimed key against the server's
//      published identity. Mismatch → throw. Match → pin (TOFU).
//
// Hashes are hex SHA-256 of the raw key bytes; comparisons are
// constant-time at the string level.

async function hashIdentityHex(key: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', key as unknown as BufferSource);
  const bytes = new Uint8Array(digest);
  let hex = '';
  for (let i = 0; i < bytes.length; i++) hex += bytes[i].toString(16).padStart(2, '0');
  return hex;
}

function constantTimeStringEquals(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return diff === 0;
}

export async function verifyAndPinPeerIdentity(
  peerUserId: string,
  claimedIdentityKey: Uint8Array,
): Promise<void> {
  const claimedHash = await hashIdentityHex(claimedIdentityKey);
  const keyStore = await loadKeyStore();
  const pinned = await keyStore.getVerified(peerUserId);

  if (pinned) {
    if (!constantTimeStringEquals(pinned.identityHash, claimedHash)) {
      throw new EncryptedSendError(
        'identity_mismatch',
        'Ключи безопасности собеседника изменились. Откройте профиль и заново проверьте личность перед отправкой сообщения.',
      );
    }
    return;
  }

  // First contact — cross-check against the server's published identity
  // for this user. If the two sources disagree, the bundle/envelope
  // header is being tampered with.
  const keysApi = await loadKeysApi();
  let serverKey: Uint8Array;
  try {
    serverKey = await keysApi.fetchIdentityKey(peerUserId);
  } catch (err) {
    throw new EncryptedSendError(
      'peer_not_enrolled',
      'Получатель ещё не настроил ключи шифрования. Попросите его открыть приложение — после первого входа сообщение можно будет отправить.',
      err,
    );
  }
  const serverHash = await hashIdentityHex(serverKey);
  if (!constantTimeStringEquals(claimedHash, serverHash)) {
    throw new EncryptedSendError(
      'identity_mismatch',
      'Заявленный ключ отправителя не совпадает с ключом, опубликованным на сервере. Сообщение отклонено.',
    );
  }

  await keyStore.pinIdentityTofu(peerUserId, serverHash);
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
  ownIdentityPublic?: Uint8Array;
}): Promise<FanoutTarget[]> {
  const {
    peerUserId, ownUserId, ownDeviceId, ownIdentityPublic,
  } = params;
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

  // Phase 7 Step 10 security fix: before trusting the bundle, verify the
  // returned identity key against either the local pin (if any) or the
  // server's published identity endpoint. Catches bundle substitution by
  // a compromised key server.
  await verifyAndPinPeerIdentity(peerUserId, peerBundle.identityKey);

  targets.push({ userId: peerUserId, deviceId: peerBundle.deviceId, bundle: peerBundle });
  seen.add(`${peerUserId}:${peerBundle.deviceId}`);

  // 2. Own-other-device target(s) — optional. Failure to reach the
  //    bundle for our own user is tolerated (we might be the only
  //    device, or the server might have hiccupped).
  try {
    const ownBundle = await keysApi.fetchKeyBundle(ownUserId);
    if (ownBundle.deviceId !== ownDeviceId) {
      // When fanning out to our OWN other device, the identity key on
      // the bundle must match our own long-term identity — otherwise
      // the server is trying to get us to encrypt to an attacker and
      // label it as ourselves. Skip silently (without throwing) if the
      // check fails: peer delivery still succeeds.
      const identityMatches = ownIdentityPublic
        ? constantTimeBytesEquals(ownBundle.identityKey, ownIdentityPublic)
        : false;
      if (identityMatches) {
        const key = `${ownUserId}:${ownBundle.deviceId}`;
        if (!seen.has(key)) {
          targets.push({ userId: ownUserId, deviceId: ownBundle.deviceId, bundle: ownBundle });
          seen.add(key);
        }
      }
    }
  } catch {
    // Silent: we just won't fan out to our other device. Messages
    // will still land on peer devices.
  }

  return targets;
}

function constantTimeBytesEquals(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a[i] ^ b[i];
  return diff === 0;
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
    ownIdentityPublic: own.identityKeyPair.publicKey,
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

export type EncryptedSendErrorCode =
  | 'peer_not_enrolled'
  | 'identity_mismatch'
  | 'internal';

export class EncryptedSendError extends Error {
  readonly code: EncryptedSendErrorCode;

  constructor(code: EncryptedSendErrorCode, message: string, cause?: unknown) {
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

  const entry = envelope.devices[own.deviceId];
  if (!entry) return undefined;

  // Phase 7 Step 10 security fix: if this entry carries a bootstrap
  // header (first message / session restart), verify the claimed
  // sender identity BEFORE any crypto work. Without this check a
  // compromised server could forge a PreKey message claiming to be
  // `senderUserId` with an attacker-controlled identity key, and
  // `bootstrapSessionAsBob` would happily derive a session with it
  // and render the attacker's plaintext as if it came from the real
  // user.
  try {
    const { decodeEntry } = await import('../../../lib/crypto/envelope');
    const ratchetMsg = decodeEntry(entry);
    if (ratchetMsg.header.bootstrap) {
      await verifyAndPinPeerIdentity(
        senderUserId,
        ratchetMsg.header.bootstrap.aliceIdentityKey,
      );
    }
  } catch (err) {
    if (err instanceof EncryptedSendError) {
      // Propagate the typed error so wsHandler can render the Russian
      // mismatch message instead of a generic "failed to decrypt".
      throw err;
    }
    throw err;
  }

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
