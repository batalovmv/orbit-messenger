import type { CryptoWorker } from './worker-proxy';
import { ensureEnrollment, enrollmentDeps } from './enrollment';

type KeysApi = typeof import('../../api/saturn/methods/keys');

const DEVICE_ID = 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee';

function makeIdentity() {
  return {
    publicKey: new Uint8Array(32).fill(0x11),
    secretKey: new Uint8Array(32).fill(0x22),
  };
}

function makeSignedPreKey(id = 1) {
  return {
    keyId: id,
    keyPair: {
      publicKey: new Uint8Array(32).fill(0x33),
      secretKey: new Uint8Array(32).fill(0x44),
    },
    signature: new Uint8Array(64).fill(0x55),
    createdAt: Date.now(),
  };
}

function makeOneTimePreKeys(count: number) {
  return Array.from({ length: count }, (_, i) => ({
    keyId: 1000 + i,
    keyPair: {
      publicKey: new Uint8Array(32).fill(i & 0xff),
      secretKey: new Uint8Array(32).fill((i + 1) & 0xff),
    },
  }));
}

function makeCryptoWorkerMock(overrides: Partial<CryptoWorker>): CryptoWorker {
  return {
    getOrCreateIdentity: jest.fn(),
    buildEnrollmentBundle: jest.fn(),
    rotateSignedPreKey: jest.fn(),
    replenishOneTimePreKeys: jest.fn(),
    needsSignedPreKeyRotation: jest.fn().mockResolvedValue(false),
    needsOneTimePreKeyReplenishment: jest.fn().mockResolvedValue(false),
    encryptForPeers: jest.fn(),
    decryptIncoming: jest.fn(),
    computeSafetyNumber: jest.fn(),
    ...overrides,
  } as CryptoWorker;
}

function makeKeysApiMock(overrides: Partial<KeysApi> = {}): KeysApi {
  return {
    uploadIdentityKey: jest.fn().mockResolvedValue(undefined),
    uploadSignedPreKey: jest.fn().mockResolvedValue(undefined),
    uploadOneTimePreKeys: jest.fn().mockResolvedValue(100),
    fetchKeyBundle: jest.fn(),
    fetchIdentityKey: jest.fn(),
    fetchUserDevices: jest.fn(),
    fetchPreKeyCount: jest.fn().mockResolvedValue(100),
    fetchKeyTransparencyLog: jest.fn(),
    revokeDevice: jest.fn(),
    sendEncryptedMessage: jest.fn(),
    setDisappearingTimer: jest.fn(),
    ...overrides,
  } as unknown as KeysApi;
}

describe('ensureEnrollment', () => {
  let originalWarn: typeof console.warn;

  beforeEach(() => {
    originalWarn = console.warn;
    // Silence the "enrollment failed" warning in tests that exercise the
    // error path. Individual tests can still inspect `console.warn` if
    // they spy on it directly.
    // eslint-disable-next-line no-console
    console.warn = jest.fn();
  });

  afterEach(() => {
    // eslint-disable-next-line no-console
    console.warn = originalWarn;
    jest.restoreAllMocks();
  });

  it('runs first-time enrollment when no identity exists locally', async () => {
    const bundle = {
      deviceId: DEVICE_ID,
      identityPublic: new Uint8Array(32).fill(0xaa),
      signedPreKey: {
        keyId: 7,
        publicKey: new Uint8Array(32).fill(0xbb),
        signature: new Uint8Array(64).fill(0xcc),
      },
      oneTimePreKeys: [
        { keyId: 1, publicKey: new Uint8Array(32).fill(0x01) },
        { keyId: 2, publicKey: new Uint8Array(32).fill(0x02) },
      ],
    };

    const crypto = makeCryptoWorkerMock({
      getOrCreateIdentity: jest.fn().mockResolvedValue({
        deviceId: DEVICE_ID,
        identityKeyPair: makeIdentity(),
        isNew: true,
      }),
      buildEnrollmentBundle: jest.fn().mockResolvedValue(bundle),
    });
    const keysApi = makeKeysApiMock();

    jest.spyOn(enrollmentDeps, 'loadWorker').mockResolvedValue(crypto);
    jest.spyOn(enrollmentDeps, 'loadKeysApi').mockResolvedValue(keysApi);

    await ensureEnrollment();

    expect(crypto.buildEnrollmentBundle).toHaveBeenCalledTimes(1);
    expect(keysApi.uploadIdentityKey).toHaveBeenCalledWith({
      identityKey: bundle.identityPublic,
      signedPreKey: bundle.signedPreKey.publicKey,
      signedPreKeySignature: bundle.signedPreKey.signature,
      signedPreKeyId: 7,
      deviceId: DEVICE_ID,
    });
    expect(keysApi.uploadOneTimePreKeys).toHaveBeenCalledWith({
      keys: [
        { keyId: 1, publicKey: bundle.oneTimePreKeys[0].publicKey },
        { keyId: 2, publicKey: bundle.oneTimePreKeys[1].publicKey },
      ],
      deviceId: DEVICE_ID,
    });
    // Rotate/replenish paths must not run on first-time flow.
    expect(crypto.needsSignedPreKeyRotation).not.toHaveBeenCalled();
    expect(keysApi.fetchPreKeyCount).not.toHaveBeenCalled();
  });

  it('skips first-time upload when signed pre-key is fresh and server has enough one-time keys', async () => {
    const crypto = makeCryptoWorkerMock({
      getOrCreateIdentity: jest.fn().mockResolvedValue({
        deviceId: DEVICE_ID,
        identityKeyPair: makeIdentity(),
        isNew: false,
      }),
      needsSignedPreKeyRotation: jest.fn().mockResolvedValue(false),
    });
    const keysApi = makeKeysApiMock({
      fetchPreKeyCount: jest.fn().mockResolvedValue(99),
    });

    jest.spyOn(enrollmentDeps, 'loadWorker').mockResolvedValue(crypto);
    jest.spyOn(enrollmentDeps, 'loadKeysApi').mockResolvedValue(keysApi);

    await ensureEnrollment();

    expect(crypto.buildEnrollmentBundle).not.toHaveBeenCalled();
    expect(keysApi.uploadIdentityKey).not.toHaveBeenCalled();
    expect(keysApi.uploadOneTimePreKeys).not.toHaveBeenCalled();
    expect(keysApi.uploadSignedPreKey).not.toHaveBeenCalled();
  });

  it('rotates the signed pre-key when it is stale', async () => {
    const fresh = makeSignedPreKey(42);
    const crypto = makeCryptoWorkerMock({
      getOrCreateIdentity: jest.fn().mockResolvedValue({
        deviceId: DEVICE_ID,
        identityKeyPair: makeIdentity(),
        isNew: false,
      }),
      needsSignedPreKeyRotation: jest.fn().mockResolvedValue(true),
      rotateSignedPreKey: jest.fn().mockResolvedValue(fresh),
    });
    const keysApi = makeKeysApiMock({
      fetchPreKeyCount: jest.fn().mockResolvedValue(100),
    });

    jest.spyOn(enrollmentDeps, 'loadWorker').mockResolvedValue(crypto);
    jest.spyOn(enrollmentDeps, 'loadKeysApi').mockResolvedValue(keysApi);

    await ensureEnrollment();

    expect(crypto.rotateSignedPreKey).toHaveBeenCalledTimes(1);
    expect(keysApi.uploadSignedPreKey).toHaveBeenCalledWith({
      signedPreKey: fresh.keyPair.publicKey,
      signedPreKeySignature: fresh.signature,
      signedPreKeyId: 42,
      deviceId: DEVICE_ID,
    });
  });

  it('replenishes one-time pre-keys when the server count drops below 20', async () => {
    const batch = makeOneTimePreKeys(100);
    const crypto = makeCryptoWorkerMock({
      getOrCreateIdentity: jest.fn().mockResolvedValue({
        deviceId: DEVICE_ID,
        identityKeyPair: makeIdentity(),
        isNew: false,
      }),
      needsSignedPreKeyRotation: jest.fn().mockResolvedValue(false),
      replenishOneTimePreKeys: jest.fn().mockResolvedValue(batch),
    });
    const keysApi = makeKeysApiMock({
      fetchPreKeyCount: jest.fn().mockResolvedValue(5),
    });

    jest.spyOn(enrollmentDeps, 'loadWorker').mockResolvedValue(crypto);
    jest.spyOn(enrollmentDeps, 'loadKeysApi').mockResolvedValue(keysApi);

    await ensureEnrollment();

    expect(crypto.replenishOneTimePreKeys).toHaveBeenCalledTimes(1);
    expect(keysApi.uploadOneTimePreKeys).toHaveBeenCalledTimes(1);
    const call = (keysApi.uploadOneTimePreKeys as jest.Mock).mock.calls[0][0];
    expect(call.keys).toHaveLength(100);
    expect(call.deviceId).toBe(DEVICE_ID);
    expect(call.keys[0]).toEqual({
      keyId: batch[0].keyId,
      publicKey: batch[0].keyPair.publicKey,
    });
  });

  it('swallows errors from uploadIdentityKey and logs a warning', async () => {
    const bundle = {
      deviceId: DEVICE_ID,
      identityPublic: new Uint8Array(32).fill(0xaa),
      signedPreKey: {
        keyId: 1,
        publicKey: new Uint8Array(32).fill(0xbb),
        signature: new Uint8Array(64).fill(0xcc),
      },
      oneTimePreKeys: [],
    };
    const crypto = makeCryptoWorkerMock({
      getOrCreateIdentity: jest.fn().mockResolvedValue({
        deviceId: DEVICE_ID,
        identityKeyPair: makeIdentity(),
        isNew: true,
      }),
      buildEnrollmentBundle: jest.fn().mockResolvedValue(bundle),
    });
    const keysApi = makeKeysApiMock({
      uploadIdentityKey: jest.fn().mockRejectedValue(new Error('network down')),
    });

    jest.spyOn(enrollmentDeps, 'loadWorker').mockResolvedValue(crypto);
    jest.spyOn(enrollmentDeps, 'loadKeysApi').mockResolvedValue(keysApi);

    await expect(ensureEnrollment()).resolves.toBeUndefined();
    expect(console.warn).toHaveBeenCalled();
  });

  it('deduplicates concurrent runs — two callers share one in-flight enrollment', async () => {
    let resolveIdentity: (value: {
      deviceId: string;
      identityKeyPair: ReturnType<typeof makeIdentity>;
      isNew: boolean;
    }) => void = () => { /* noop */ };
    const identityPromise = new Promise<{
      deviceId: string;
      identityKeyPair: ReturnType<typeof makeIdentity>;
      isNew: boolean;
    }>((r) => { resolveIdentity = r; });

    const crypto = makeCryptoWorkerMock({
      getOrCreateIdentity: jest.fn().mockReturnValue(identityPromise),
      needsSignedPreKeyRotation: jest.fn().mockResolvedValue(false),
    });
    const keysApi = makeKeysApiMock({
      fetchPreKeyCount: jest.fn().mockResolvedValue(100),
    });

    jest.spyOn(enrollmentDeps, 'loadWorker').mockResolvedValue(crypto);
    jest.spyOn(enrollmentDeps, 'loadKeysApi').mockResolvedValue(keysApi);

    const first = ensureEnrollment();
    const second = ensureEnrollment();

    // Before identity resolves, both calls must return the same pending
    // promise so no double work happens.
    expect(first).toBe(second);

    resolveIdentity({
      deviceId: DEVICE_ID,
      identityKeyPair: makeIdentity(),
      isNew: false,
    });
    await Promise.all([first, second]);

    expect(crypto.getOrCreateIdentity).toHaveBeenCalledTimes(1);
  });
});
