import { decodeBase64Url, encodeBase64Url } from '../../../lib/crypto/base64url';
import type { MessageEnvelope } from '../../../lib/crypto/types';
import * as client from '../client';
import {
  fetchIdentityKey,
  fetchKeyBundle,
  fetchKeyTransparencyLog,
  fetchPreKeyCount,
  fetchUserDevices,
  revokeDevice,
  sendEncryptedMessage,
  setDisappearingTimer,
  uploadIdentityKey,
  uploadOneTimePreKeys,
  uploadSignedPreKey,
} from './keys';

const DEVICE_ID = 'b1946ac9-2de0-4c0e-a3b4-7f8d0000001a';

function rawBytes(bytes: number[]): Uint8Array {
  return new Uint8Array(bytes);
}

describe('keys upload methods', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('uploadIdentityKey base64url-encodes all key material and sets X-Device-ID header', async () => {
    const spy = jest.spyOn(client, 'request').mockResolvedValue({ status: 'ok' } as unknown as never);

    const identityKey = rawBytes([1, 2, 3, 4]);
    const signedPreKey = rawBytes([5, 6, 7, 8]);
    const signature = rawBytes([9, 10, 11, 12]);

    await uploadIdentityKey({
      identityKey,
      signedPreKey,
      signedPreKeySignature: signature,
      signedPreKeyId: 7,
      deviceId: DEVICE_ID,
    });

    expect(spy).toHaveBeenCalledTimes(1);
    const [method, path, body, options] = spy.mock.calls[0];
    expect(method).toBe('POST');
    expect(path).toBe('/keys/identity');
    expect(body).toEqual({
      identity_key: encodeBase64Url(identityKey),
      signed_prekey: encodeBase64Url(signedPreKey),
      signed_prekey_signature: encodeBase64Url(signature),
      signed_prekey_id: 7,
    });
    expect(options?.headers).toEqual({ 'X-Device-ID': DEVICE_ID });
  });

  it('uploadSignedPreKey posts signed prekey with device header', async () => {
    const spy = jest.spyOn(client, 'request').mockResolvedValue({ status: 'ok' } as unknown as never);
    await uploadSignedPreKey({
      signedPreKey: rawBytes([0xaa, 0xbb]),
      signedPreKeySignature: rawBytes([0xcc, 0xdd]),
      signedPreKeyId: 3,
      deviceId: DEVICE_ID,
    });

    const [method, path, body, options] = spy.mock.calls[0];
    expect(method).toBe('POST');
    expect(path).toBe('/keys/signed-prekey');
    expect(body).toMatchObject({ signed_prekey_id: 3 });
    expect(options?.headers).toEqual({ 'X-Device-ID': DEVICE_ID });
  });

  it('uploadOneTimePreKeys serializes each key and returns server count', async () => {
    const spy = jest.spyOn(client, 'request').mockResolvedValue({ count: 2 } as unknown as never);

    const result = await uploadOneTimePreKeys({
      keys: [
        { keyId: 1, publicKey: rawBytes([1, 1, 1]) },
        { keyId: 2, publicKey: rawBytes([2, 2, 2]) },
      ],
      deviceId: DEVICE_ID,
    });

    expect(result).toBe(2);
    const body = spy.mock.calls[0][2] as { prekeys: Array<{ key_id: number; public_key: string }> };
    expect(body.prekeys).toHaveLength(2);
    expect(body.prekeys[0]).toEqual({ key_id: 1, public_key: encodeBase64Url(rawBytes([1, 1, 1])) });
  });

  it('uploadOneTimePreKeys rejects empty or oversized batches without calling the server', async () => {
    const spy = jest.spyOn(client, 'request').mockResolvedValue({ count: 0 } as unknown as never);

    await expect(uploadOneTimePreKeys({ keys: [], deviceId: DEVICE_ID })).rejects.toThrow(
      /1 to 100/,
    );
    const oversized = Array.from({ length: 101 }, (_, i) => ({
      keyId: i,
      publicKey: rawBytes([i & 0xff]),
    }));
    await expect(uploadOneTimePreKeys({ keys: oversized, deviceId: DEVICE_ID })).rejects.toThrow(
      /1 to 100/,
    );
    expect(spy).not.toHaveBeenCalled();
  });
});

describe('keys fetch methods', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('fetchKeyBundle decodes base64url material into Uint8Array and maps snake_case fields', async () => {
    const bytes = rawBytes([10, 20, 30]);
    jest.spyOn(client, 'request').mockResolvedValue({
      identity_key: encodeBase64Url(bytes),
      signed_prekey: encodeBase64Url(bytes),
      signed_prekey_signature: encodeBase64Url(bytes),
      signed_prekey_id: 4,
      one_time_prekey: encodeBase64Url(bytes),
      one_time_prekey_id: 77,
      device_id: DEVICE_ID,
    } as unknown as never);

    const bundle = await fetchKeyBundle('bob-user-id');
    expect(bundle.userId).toBe('bob-user-id');
    expect(bundle.deviceId).toBe(DEVICE_ID);
    expect(bundle.signedPreKeyId).toBe(4);
    expect(bundle.identityKey).toEqual(bytes);
    expect(bundle.signedPreKey).toEqual(bytes);
    expect(bundle.signedPreKeySignature).toEqual(bytes);
    expect(bundle.oneTimePreKey).toEqual({ keyId: 77, publicKey: bytes });
  });

  it('fetchKeyBundle omits oneTimePreKey when the server returns none', async () => {
    jest.spyOn(client, 'request').mockResolvedValue({
      identity_key: encodeBase64Url(rawBytes([1])),
      signed_prekey: encodeBase64Url(rawBytes([2])),
      signed_prekey_signature: encodeBase64Url(rawBytes([3])),
      signed_prekey_id: 1,
      device_id: DEVICE_ID,
    } as unknown as never);

    const bundle = await fetchKeyBundle('user');
    expect(bundle.oneTimePreKey).toBeUndefined();
  });

  it('fetchIdentityKey decodes the single key field', async () => {
    const bytes = rawBytes([99, 100, 101]);
    jest.spyOn(client, 'request').mockResolvedValue({
      identity_key: encodeBase64Url(bytes),
    } as unknown as never);
    const result = await fetchIdentityKey('alice-id');
    expect(result).toEqual(bytes);
    // Round-trip sanity: we encode then decode and must get the same bytes back.
    expect(decodeBase64Url(encodeBase64Url(bytes))).toEqual(bytes);
  });

  it('fetchUserDevices normalizes device list shape', async () => {
    jest.spyOn(client, 'request').mockResolvedValue({
      devices: [
        { device_id: 'dev-1', created_at: '2026-04-15T10:00:00Z' },
        { device_id: 'dev-2', created_at: '2026-04-15T11:00:00Z' },
      ],
    } as unknown as never);
    const devices = await fetchUserDevices('alice');
    expect(devices).toHaveLength(2);
    expect(devices[0]).toEqual({ deviceId: 'dev-1', createdAt: '2026-04-15T10:00:00Z' });
  });

  it('fetchPreKeyCount sends X-Device-ID and returns the numeric count', async () => {
    const spy = jest.spyOn(client, 'request').mockResolvedValue({ count: 42 } as unknown as never);
    const result = await fetchPreKeyCount(DEVICE_ID);
    expect(result).toBe(42);
    const options = spy.mock.calls[0][3];
    expect(options?.headers).toEqual({ 'X-Device-ID': DEVICE_ID });
  });

  it('fetchKeyTransparencyLog appends user_id and limit query params when provided', async () => {
    const spy = jest.spyOn(client, 'request').mockResolvedValue({ entries: [] } as unknown as never);
    await fetchKeyTransparencyLog('alice', 10);
    expect(spy.mock.calls[0][1]).toBe('/keys/transparency-log?user_id=alice&limit=10');

    await fetchKeyTransparencyLog();
    expect(spy.mock.calls[1][1]).toBe('/keys/transparency-log');
  });

  it('fetchKeyTransparencyLog maps server entries to camelCase', async () => {
    jest.spyOn(client, 'request').mockResolvedValue({
      entries: [
        {
          id: 1,
          user_id: 'alice',
          device_id: 'dev-1',
          event_type: 'register',
          public_key_hash: 'abcd',
          created_at: '2026-04-15T10:00:00Z',
        },
      ],
    } as unknown as never);
    const entries = await fetchKeyTransparencyLog('alice');
    expect(entries).toHaveLength(1);
    expect(entries[0]).toEqual({
      id: 1,
      userId: 'alice',
      deviceId: 'dev-1',
      eventType: 'register',
      publicKeyHash: 'abcd',
      createdAt: '2026-04-15T10:00:00Z',
    });
  });
});

describe('keys misc methods', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('revokeDevice sends DELETE with X-Device-ID header', async () => {
    const spy = jest.spyOn(client, 'request').mockResolvedValue({ status: 'ok' } as unknown as never);
    await revokeDevice(DEVICE_ID);
    const [method, path, body, options] = spy.mock.calls[0];
    expect(method).toBe('DELETE');
    expect(path).toBe('/keys/device');
    expect(body).toBeUndefined();
    expect(options?.headers).toEqual({ 'X-Device-ID': DEVICE_ID });
  });

  it('sendEncryptedMessage posts envelope verbatim with device header', async () => {
    const spy = jest.spyOn(client, 'request').mockResolvedValue({ id: 'msg-1' } as unknown as never);
    const envelope: MessageEnvelope = {
      v: 1,
      sender_device_id: DEVICE_ID,
      devices: { 'peer-device': { type: 2, body: 'AAECAwQ' } },
    };
    const result = await sendEncryptedMessage({
      chatId: 'chat-abc',
      envelope,
      deviceId: DEVICE_ID,
    });
    expect(result).toEqual({ id: 'msg-1' });
    const [method, path, body, options] = spy.mock.calls[0];
    expect(method).toBe('POST');
    expect(path).toBe('/chats/chat-abc/messages/encrypted');
    expect(body).toEqual({ envelope });
    expect(options?.headers).toEqual({ 'X-Device-ID': DEVICE_ID });
  });

  it('setDisappearingTimer uses PUT and truncates non-integer seconds', async () => {
    const spy = jest.spyOn(client, 'request').mockResolvedValue(undefined as unknown as never);
    await setDisappearingTimer({ chatId: 'chat-1', seconds: 86_400 });
    expect(spy.mock.calls[0][0]).toBe('PUT');
    expect(spy.mock.calls[0][1]).toBe('/chats/chat-1/disappearing');
    expect(spy.mock.calls[0][2]).toEqual({ timer: 86_400 });

    await setDisappearingTimer({ chatId: 'chat-1', seconds: 100.9 });
    expect(spy.mock.calls[1][2]).toEqual({ timer: 100 });
  });

  it('setDisappearingTimer rejects negative or non-finite values', async () => {
    const spy = jest.spyOn(client, 'request').mockResolvedValue(undefined as unknown as never);
    await expect(setDisappearingTimer({ chatId: 'chat-1', seconds: -1 })).rejects.toThrow(
      /non-negative/,
    );
    await expect(setDisappearingTimer({ chatId: 'chat-1', seconds: Number.NaN })).rejects.toThrow(
      /non-negative/,
    );
    expect(spy).not.toHaveBeenCalled();
  });
});
