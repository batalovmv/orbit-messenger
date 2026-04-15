import 'fake-indexeddb/auto';

import type { MessageEnvelope, PreKeyBundle } from '../../../lib/crypto/types';
import * as keyStore from '../../../lib/crypto/key-store';
import * as keysApi from './keys';
import { collectFanoutTargets, decodeEncryptedContentField, EncryptedSendError } from './encryptedMessages';

// All fakeBundle calls use this identity key — mock fetchIdentityKey to
// return the same value so `verifyAndPinPeerIdentity` finds a match.
const FAKE_IDENTITY_KEY = new Uint8Array(32).fill(1);

function fakeBundle(deviceId: string, identityKey = FAKE_IDENTITY_KEY): PreKeyBundle {
  return {
    userId: 'dummy',
    deviceId,
    identityKey,
    signedPreKey: new Uint8Array(32).fill(2),
    signedPreKeySignature: new Uint8Array(64).fill(3),
    signedPreKeyId: 1,
  };
}

// Shared helper: default mocks that make `verifyAndPinPeerIdentity` a
// no-op (no existing pin → server returns matching key → pin written).
function installIdentityPinMocks(serverKey = FAKE_IDENTITY_KEY) {
  jest.spyOn(keysApi, 'fetchIdentityKey').mockResolvedValue(serverKey);
  jest.spyOn(keyStore, 'getVerified').mockResolvedValue(undefined);
  jest.spyOn(keyStore, 'pinIdentityTofu').mockResolvedValue(undefined);
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
    installIdentityPinMocks();
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
    installIdentityPinMocks();
    jest.spyOn(keysApi, 'fetchKeyBundle').mockImplementation(async (userId: string) => {
      if (userId === 'peer-1') return fakeBundle('peer-device-A');
      if (userId === 'self-1') return fakeBundle('primary-device');
      throw new Error('unexpected');
    });

    const targets = await collectFanoutTargets({
      peerUserId: 'peer-1',
      ownUserId: 'self-1',
      ownDeviceId: 'secondary-device',
      // FAKE_IDENTITY_KEY is also the identity returned for self-1, so
      // the own-identity match succeeds and the second target is kept.
      ownIdentityPublic: FAKE_IDENTITY_KEY,
    });

    expect(targets).toHaveLength(2);
    expect(targets.find((t) => t.userId === 'peer-1')?.deviceId).toBe('peer-device-A');
    expect(targets.find((t) => t.userId === 'self-1')?.deviceId).toBe('primary-device');
  });

  it('skips own-other-device target when backend returns this same device', async () => {
    installIdentityPinMocks();
    jest.spyOn(keysApi, 'fetchKeyBundle').mockImplementation(async (userId: string) => {
      if (userId === 'peer-1') return fakeBundle('peer-device-A');
      if (userId === 'self-1') return fakeBundle('own-device');
      throw new Error('unexpected');
    });

    const targets = await collectFanoutTargets({
      peerUserId: 'peer-1',
      ownUserId: 'self-1',
      ownDeviceId: 'own-device',
      ownIdentityPublic: FAKE_IDENTITY_KEY,
    });

    expect(targets).toHaveLength(1);
    expect(targets[0].userId).toBe('peer-1');
  });

  it('skips own-other-device target when its identity key does not match our own', async () => {
    installIdentityPinMocks();
    const differentKey = new Uint8Array(32).fill(0xee);
    jest.spyOn(keysApi, 'fetchKeyBundle').mockImplementation(async (userId: string) => {
      if (userId === 'peer-1') return fakeBundle('peer-device-A');
      if (userId === 'self-1') return fakeBundle('primary-device', differentKey);
      throw new Error('unexpected');
    });

    const targets = await collectFanoutTargets({
      peerUserId: 'peer-1',
      ownUserId: 'self-1',
      ownDeviceId: 'secondary-device',
      // Our real identity is FAKE_IDENTITY_KEY, but the server returned
      // a bundle with `differentKey` — refuse to fan out to it.
      ownIdentityPublic: FAKE_IDENTITY_KEY,
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

describe('collectFanoutTargets identity pinning (Vuln 1 fix)', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('rejects a bundle whose identity key does not match the pinned hash', async () => {
    const tamperedKey = new Uint8Array(32).fill(0x77);
    jest.spyOn(keysApi, 'fetchKeyBundle').mockResolvedValue(fakeBundle('peer-device-A', tamperedKey));
    // Pinned identity is the canonical FAKE_IDENTITY_KEY — hash it.
    const pinnedHash = await sha256Hex(FAKE_IDENTITY_KEY);
    jest.spyOn(keyStore, 'getVerified').mockResolvedValue({
      peerUserId: 'peer-1',
      identityHash: pinnedHash,
      verifiedAt: 0,
      source: 'tofu',
    });

    await expect(collectFanoutTargets({
      peerUserId: 'peer-1',
      ownUserId: 'self-1',
      ownDeviceId: 'own-device',
    })).rejects.toMatchObject({
      name: 'EncryptedSendError',
      code: 'identity_mismatch',
    });
  });

  it('rejects a bundle whose identity key does not match the server-published key on first contact', async () => {
    const bundleKey = new Uint8Array(32).fill(0x88);
    const serverKey = new Uint8Array(32).fill(0x99);
    jest.spyOn(keysApi, 'fetchKeyBundle').mockResolvedValue(fakeBundle('peer-device-A', bundleKey));
    jest.spyOn(keysApi, 'fetchIdentityKey').mockResolvedValue(serverKey);
    jest.spyOn(keyStore, 'getVerified').mockResolvedValue(undefined);
    const pinSpy = jest.spyOn(keyStore, 'pinIdentityTofu').mockResolvedValue(undefined);

    await expect(collectFanoutTargets({
      peerUserId: 'peer-1',
      ownUserId: 'self-1',
      ownDeviceId: 'own-device',
    })).rejects.toMatchObject({
      name: 'EncryptedSendError',
      code: 'identity_mismatch',
    });
    expect(pinSpy).not.toHaveBeenCalled();
  });

  it('pins a fresh identity on first contact when server-published key matches the bundle', async () => {
    jest.spyOn(keysApi, 'fetchKeyBundle').mockResolvedValue(fakeBundle('peer-device-A'));
    jest.spyOn(keysApi, 'fetchIdentityKey').mockResolvedValue(FAKE_IDENTITY_KEY);
    jest.spyOn(keyStore, 'getVerified').mockResolvedValue(undefined);
    const pinSpy = jest.spyOn(keyStore, 'pinIdentityTofu').mockResolvedValue(undefined);
    const expectedHash = await sha256Hex(FAKE_IDENTITY_KEY);

    const targets = await collectFanoutTargets({
      peerUserId: 'peer-1',
      ownUserId: 'self-1',
      ownDeviceId: 'own-device',
    });

    expect(targets).toHaveLength(1);
    expect(pinSpy).toHaveBeenCalledWith('peer-1', expectedHash);
  });
});

// Local hash helper (matches the implementation in encryptedMessages.ts).
async function sha256Hex(bytes: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', bytes as unknown as BufferSource);
  const out = new Uint8Array(digest);
  let hex = '';
  for (let i = 0; i < out.length; i++) hex += out[i].toString(16).padStart(2, '0');
  return hex;
}
