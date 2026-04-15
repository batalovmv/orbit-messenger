import type { MessageEnvelope, PreKeyBundle } from '../../../lib/crypto/types';
import * as keysApi from './keys';
import { collectFanoutTargets, decodeEncryptedContentField, EncryptedSendError } from './encryptedMessages';

function fakeBundle(deviceId: string): PreKeyBundle {
  return {
    userId: 'dummy',
    deviceId,
    identityKey: new Uint8Array(32).fill(1),
    signedPreKey: new Uint8Array(32).fill(2),
    signedPreKeySignature: new Uint8Array(64).fill(3),
    signedPreKeyId: 1,
  };
}

// Helper: encode an object as standard base64 (matches Go's default
// `[]byte → base64` behaviour on the backend).
function encodeStandardBase64(json: string): string {
  return btoa(json);
}

describe('decodeEncryptedContentField', () => {
  it('decodes a valid envelope emitted by the backend', () => {
    const envelope: MessageEnvelope = {
      v: 1,
      sender_device_id: 'device-1',
      devices: {
        'device-peer': { type: 2, body: 'AAECAw' },
      },
    };
    const serverField = encodeStandardBase64(JSON.stringify(envelope));
    const decoded = decodeEncryptedContentField(serverField);

    expect(decoded.v).toBe(1);
    expect(decoded.sender_device_id).toBe('device-1');
    expect(decoded.devices['device-peer']).toEqual({ type: 2, body: 'AAECAw' });
  });

  it('rejects payloads that are not v1 envelopes', () => {
    const bad = encodeStandardBase64(JSON.stringify({ hello: 'world' }));
    expect(() => decodeEncryptedContentField(bad)).toThrow(/not a v1 envelope/);
  });

  it('rejects envelopes missing sender_device_id', () => {
    const broken = encodeStandardBase64(JSON.stringify({ v: 1, devices: {} }));
    expect(() => decodeEncryptedContentField(broken)).toThrow(/not a v1 envelope/);
  });
});

describe('collectFanoutTargets', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('returns a single peer target when own-user bundle lookup fails', async () => {
    jest.spyOn(keysApi, 'fetchKeyBundle').mockImplementation(async (userId: string) => {
      if (userId === 'peer-1') return fakeBundle('peer-device-A');
      throw new Error('network');
    });

    const targets = await collectFanoutTargets({
      peerUserId: 'peer-1',
      ownUserId: 'self-1',
      ownDeviceId: 'own-device',
    });

    expect(targets).toHaveLength(1);
    expect(targets[0]).toMatchObject({ userId: 'peer-1', deviceId: 'peer-device-A' });
  });

  it('includes an own-other-device target when backend returns a different device id', async () => {
    jest.spyOn(keysApi, 'fetchKeyBundle').mockImplementation(async (userId: string) => {
      if (userId === 'peer-1') return fakeBundle('peer-device-A');
      if (userId === 'self-1') return fakeBundle('primary-device');
      throw new Error('unexpected');
    });

    const targets = await collectFanoutTargets({
      peerUserId: 'peer-1',
      ownUserId: 'self-1',
      ownDeviceId: 'secondary-device',
    });

    expect(targets).toHaveLength(2);
    expect(targets.find((t) => t.userId === 'peer-1')?.deviceId).toBe('peer-device-A');
    expect(targets.find((t) => t.userId === 'self-1')?.deviceId).toBe('primary-device');
  });

  it('skips own-other-device target when backend returns this same device', async () => {
    jest.spyOn(keysApi, 'fetchKeyBundle').mockImplementation(async (userId: string) => {
      if (userId === 'peer-1') return fakeBundle('peer-device-A');
      if (userId === 'self-1') return fakeBundle('own-device');
      throw new Error('unexpected');
    });

    const targets = await collectFanoutTargets({
      peerUserId: 'peer-1',
      ownUserId: 'self-1',
      ownDeviceId: 'own-device',
    });

    expect(targets).toHaveLength(1);
    expect(targets[0].userId).toBe('peer-1');
  });

  it('throws EncryptedSendError("peer_not_enrolled") when the peer has no bundle', async () => {
    jest.spyOn(keysApi, 'fetchKeyBundle').mockRejectedValue(new Error('404'));
    await expect(collectFanoutTargets({
      peerUserId: 'peer-x',
      ownUserId: 'self-1',
      ownDeviceId: 'own-device',
    })).rejects.toBeInstanceOf(EncryptedSendError);
  });
});
