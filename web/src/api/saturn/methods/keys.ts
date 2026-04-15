// Saturn API methods for E2E key management (Phase 7).
//
// Wraps the backend key endpoints exposed by auth service (proxied through
// gateway at `/api/v1/keys/*`) and the encrypted-message endpoint exposed
// by messaging service. All key material is transported as base64url
// (RFC 4648 §5, no padding) — byte-for-byte compatible with Go's
// `base64.RawURLEncoding` that the backend uses.
//
// Endpoints backing these methods (all present in backend as of commit
// 0be6da3 — see docs/phase7-design.md §1.1):
//
//   POST   /api/v1/keys/identity           (X-Device-ID required)
//   POST   /api/v1/keys/signed-prekey      (X-Device-ID required)
//   POST   /api/v1/keys/one-time-prekeys   (X-Device-ID required)
//   GET    /api/v1/keys/:userId/bundle
//   GET    /api/v1/keys/:userId/identity
//   GET    /api/v1/keys/:userId/devices
//   GET    /api/v1/keys/count              (X-Device-ID required)
//   GET    /api/v1/keys/transparency-log
//   DELETE /api/v1/keys/device             (X-Device-ID required)
//   POST   /api/v1/chats/:id/messages/encrypted   (X-Device-ID required)
//   PUT    /api/v1/chats/:id/disappearing

import { decodeBase64Url, encodeBase64Url } from '../../../lib/crypto/base64url';
import type { MessageEnvelope, PreKeyBundle } from '../../../lib/crypto/types';
import { request } from '../client';

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

function deviceHeaders(deviceId: string): Record<string, string> {
  return { 'X-Device-ID': deviceId };
}

// ────────────────────────────────────────────────────────────────────────────
// Device enrollment — upload
// ────────────────────────────────────────────────────────────────────────────

export async function uploadIdentityKey({
  identityKey,
  signedPreKey,
  signedPreKeySignature,
  signedPreKeyId,
  deviceId,
}: {
  identityKey: Uint8Array;
  signedPreKey: Uint8Array;
  signedPreKeySignature: Uint8Array;
  signedPreKeyId: number;
  deviceId: string;
}): Promise<void> {
  await request<{ status: string }>('POST', '/keys/identity', {
    identity_key: encodeBase64Url(identityKey),
    signed_prekey: encodeBase64Url(signedPreKey),
    signed_prekey_signature: encodeBase64Url(signedPreKeySignature),
    signed_prekey_id: signedPreKeyId,
  }, { headers: deviceHeaders(deviceId) });
}

export async function uploadSignedPreKey({
  signedPreKey,
  signedPreKeySignature,
  signedPreKeyId,
  deviceId,
}: {
  signedPreKey: Uint8Array;
  signedPreKeySignature: Uint8Array;
  signedPreKeyId: number;
  deviceId: string;
}): Promise<void> {
  await request<{ status: string }>('POST', '/keys/signed-prekey', {
    signed_prekey: encodeBase64Url(signedPreKey),
    signed_prekey_signature: encodeBase64Url(signedPreKeySignature),
    signed_prekey_id: signedPreKeyId,
  }, { headers: deviceHeaders(deviceId) });
}

export async function uploadOneTimePreKeys({
  keys,
  deviceId,
}: {
  keys: ReadonlyArray<{ keyId: number; publicKey: Uint8Array }>;
  deviceId: string;
}): Promise<number> {
  if (keys.length === 0 || keys.length > 100) {
    throw new Error('uploadOneTimePreKeys: batch must contain 1 to 100 keys');
  }
  const resp = await request<{ count: number }>('POST', '/keys/one-time-prekeys', {
    prekeys: keys.map((k) => ({
      key_id: k.keyId,
      public_key: encodeBase64Url(k.publicKey),
    })),
  }, { headers: deviceHeaders(deviceId) });
  return resp.count;
}

// ────────────────────────────────────────────────────────────────────────────
// Key lookup
// ────────────────────────────────────────────────────────────────────────────

type FetchedBundleResponse = {
  identity_key: string;
  signed_prekey: string;
  signed_prekey_signature: string;
  signed_prekey_id: number;
  one_time_prekey?: string;
  one_time_prekey_id?: number;
  device_id: string;
};

/**
 * Fetch a pre-key bundle for starting a session with the target user.
 *
 * Backend atomically consumes one-time pre-keys on this call — every
 * successful call that returns `oneTimePreKey` burns the key on the
 * server, so callers must treat the bundle as exclusive and persist it
 * into local session state immediately.
 */
export async function fetchKeyBundle(userId: string): Promise<PreKeyBundle> {
  const resp = await request<FetchedBundleResponse>('GET', `/keys/${userId}/bundle`);
  const bundle: PreKeyBundle = {
    userId,
    deviceId: resp.device_id,
    identityKey: decodeBase64Url(resp.identity_key),
    signedPreKey: decodeBase64Url(resp.signed_prekey),
    signedPreKeySignature: decodeBase64Url(resp.signed_prekey_signature),
    signedPreKeyId: resp.signed_prekey_id,
  };
  if (resp.one_time_prekey && resp.one_time_prekey_id !== undefined) {
    bundle.oneTimePreKey = {
      keyId: resp.one_time_prekey_id,
      publicKey: decodeBase64Url(resp.one_time_prekey),
    };
  }
  return bundle;
}

/**
 * Fetch only the long-term identity key of a user. Used by Safety Numbers
 * UI to compute the 60-digit verification number without consuming a
 * one-time pre-key.
 */
export async function fetchIdentityKey(userId: string): Promise<Uint8Array> {
  const resp = await request<{ identity_key: string }>('GET', `/keys/${userId}/identity`);
  return decodeBase64Url(resp.identity_key);
}

export type DeviceInfo = {
  deviceId: string;
  createdAt: string;
};

export async function fetchUserDevices(userId: string): Promise<DeviceInfo[]> {
  const resp = await request<{ devices: Array<{ device_id: string; created_at: string }> }>(
    'GET',
    `/keys/${userId}/devices`,
  );
  return resp.devices.map((d) => ({
    deviceId: d.device_id,
    createdAt: d.created_at,
  }));
}

/**
 * Returns how many one-time pre-keys are still available on the server for
 * this device. When this drops below ~20 we run `uploadOneTimePreKeys`
 * again with a fresh batch.
 */
export async function fetchPreKeyCount(deviceId: string): Promise<number> {
  const resp = await request<{ count: number }>('GET', '/keys/count', undefined, {
    headers: deviceHeaders(deviceId),
  });
  return resp.count;
}

// ────────────────────────────────────────────────────────────────────────────
// Key transparency log (compliance audit)
// ────────────────────────────────────────────────────────────────────────────

export type TransparencyLogEntry = {
  id: number;
  userId: string;
  deviceId: string;
  eventType: string;
  publicKeyHash: string;
  createdAt: string;
};

type RawTransparencyEntry = {
  id: number;
  user_id: string;
  device_id: string;
  event_type: string;
  public_key_hash: string;
  created_at: string;
};

/**
 * Fetch the key transparency log. If `userId` is omitted the caller's own
 * log is returned. Used by compliance/audit UI to verify key history.
 */
export async function fetchKeyTransparencyLog(
  userId?: string,
  limit = 50,
): Promise<TransparencyLogEntry[]> {
  const query = new URLSearchParams();
  if (userId) query.set('user_id', userId);
  if (limit !== 50) query.set('limit', String(limit));
  const qs = query.toString();
  const path = qs ? `/keys/transparency-log?${qs}` : '/keys/transparency-log';
  const resp = await request<{ entries: RawTransparencyEntry[] }>('GET', path);
  return (resp.entries ?? []).map((e) => ({
    id: e.id,
    userId: e.user_id,
    deviceId: e.device_id,
    eventType: e.event_type,
    publicKeyHash: e.public_key_hash,
    createdAt: e.created_at,
  }));
}

// ────────────────────────────────────────────────────────────────────────────
// Device revocation
// ────────────────────────────────────────────────────────────────────────────

/**
 * Revoke the current device on the server. Called on "Log out of this
 * device" flow. The caller should also clear local IndexedDB crypto state
 * (`clearAllCryptoState`) after a successful revoke.
 */
export async function revokeDevice(deviceId: string): Promise<void> {
  await request<{ status: string }>('DELETE', '/keys/device', undefined, {
    headers: deviceHeaders(deviceId),
  });
}

// ────────────────────────────────────────────────────────────────────────────
// Encrypted message transport
// ────────────────────────────────────────────────────────────────────────────

/**
 * Send an encrypted message envelope to a chat.
 *
 * Backend stores `envelope` as raw BYTEA JSON (server-opaque). The message
 * is published over NATS to every member of the chat; each device client
 * decrypts its own entry from `envelope.devices[selfDeviceId]`.
 */
export async function sendEncryptedMessage({
  chatId,
  envelope,
  deviceId,
  mediaIds,
}: {
  chatId: string;
  envelope: MessageEnvelope;
  deviceId: string;
  mediaIds?: string[];
}): Promise<{ id: string }> {
  const body: Record<string, unknown> = { envelope };
  if (mediaIds && mediaIds.length > 0) {
    body.media_ids = mediaIds;
  }
  return request<{ id: string }>(
    'POST',
    `/chats/${chatId}/messages/encrypted`,
    body,
    { headers: deviceHeaders(deviceId) },
  );
}

// ────────────────────────────────────────────────────────────────────────────
// Disappearing messages
// ────────────────────────────────────────────────────────────────────────────

/**
 * Set the disappearing-messages timer for a chat in seconds. Pass 0 to
 * disable. Accepted values match the UI dropdown in Chat settings:
 * 0 (off), 86400 (24h), 604800 (7d), 2_592_000 (30d).
 */
export async function setDisappearingTimer({
  chatId,
  seconds,
}: {
  chatId: string;
  seconds: number;
}): Promise<void> {
  if (!Number.isFinite(seconds) || seconds < 0) {
    throw new Error('setDisappearingTimer: timer must be non-negative number of seconds');
  }
  await request<unknown>('PUT', `/chats/${chatId}/disappearing`, { timer: Math.trunc(seconds) });
}
