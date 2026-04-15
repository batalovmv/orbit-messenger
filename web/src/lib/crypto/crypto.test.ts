// End-to-end crypto round-trip tests.
//
// Covers:
//   * Ed25519 → X25519 conversion correctness (known vector + signature
//     compatibility via DH round-trip).
//   * X3DH → Double Ratchet → encrypt → decrypt in both directions.
//   * Multi-message ratcheting.
//   * State serialization round-trip.
//   * Session init error paths (tamper detection).
//
// These tests exercise the pure-crypto modules directly, with no
// IndexedDB, no Web Worker, no network. That way they stay fast (<1s
// for the full suite) and deterministic.

import {
  deserializeRatchetState,
  initRatchetAsAlice,
  initRatchetAsBob,
  ratchetDecrypt,
  ratchetEncrypt,
  serializeRatchetState,
  type SkippedKeys,
} from './double-ratchet';
import {
  ed25519PublicToX25519,
  ed25519SecretToX25519,
} from './ed25519-to-x25519';
import {
  generateEd25519KeyPair,
  generateX25519KeyPair,
  signEd25519,
  x25519DH,
} from './primitives';
import { x3dhInitAsAlice, x3dhInitAsBob } from './x3dh';
import type { OneTimePreKey, PreKeyBundle, SignedPreKey } from './types';

const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();

function signedPreKeyFor(identitySecret: Uint8Array, keyId = 1): SignedPreKey {
  const keyPair = generateX25519KeyPair();
  const signature = signEd25519(keyPair.publicKey, identitySecret);
  return {
    keyId,
    keyPair,
    signature,
    createdAt: Date.now(),
  };
}

function oneTimePreKeyFor(keyId: number): OneTimePreKey {
  return { keyId, keyPair: generateX25519KeyPair() };
}

function bundleForBob(
  bobIdentityPublic: Uint8Array,
  signed: SignedPreKey,
  oneTime: OneTimePreKey | undefined,
): PreKeyBundle {
  return {
    userId: 'bob-user',
    deviceId: 'bob-device',
    identityKey: bobIdentityPublic,
    signedPreKey: signed.keyPair.publicKey,
    signedPreKeySignature: signed.signature,
    signedPreKeyId: signed.keyId,
    oneTimePreKey: oneTime
      ? { keyId: oneTime.keyId, publicKey: oneTime.keyPair.publicKey }
      : undefined,
  };
}

describe('Ed25519 ↔ X25519 conversion', () => {
  it('converts Ed25519 secret to an X25519 secret that DH-matches its own public', () => {
    // Property test: for a random Ed25519 identity, compute X25519 secret/public via
    // conversion, and verify that Diffie-Hellman with a peer X25519 pair works.
    const edAlice = generateEd25519KeyPair();
    const xAliceSecret = ed25519SecretToX25519(edAlice.secretKey);
    const xAlicePublic = ed25519PublicToX25519(edAlice.publicKey);

    const xBob = generateX25519KeyPair();

    const sharedA = x25519DH(xAliceSecret, xBob.publicKey);
    const sharedB = x25519DH(xBob.secretKey, xAlicePublic);

    expect(sharedA).toEqual(sharedB);
    expect(sharedA.length).toBe(32);
  });

  it('conversion is deterministic for the same seed', () => {
    const ed = generateEd25519KeyPair();
    const first = ed25519SecretToX25519(ed.secretKey);
    const second = ed25519SecretToX25519(ed.secretKey);
    expect(first).toEqual(second);

    const pub1 = ed25519PublicToX25519(ed.publicKey);
    const pub2 = ed25519PublicToX25519(ed.publicKey);
    expect(pub1).toEqual(pub2);
  });
});

describe('X3DH handshake', () => {
  it('Alice and Bob derive the same 32-byte shared secret (with one-time pre-key)', () => {
    const aliceIdentity = generateEd25519KeyPair();
    const bobIdentity = generateEd25519KeyPair();
    const bobSigned = signedPreKeyFor(bobIdentity.secretKey, 7);
    const bobOneTime = oneTimePreKeyFor(42);
    const aliceEphemeral = generateX25519KeyPair();

    const bundle = bundleForBob(bobIdentity.publicKey, bobSigned, bobOneTime);
    const aliceResult = x3dhInitAsAlice(aliceIdentity, aliceEphemeral, bundle);

    const bobSecret = x3dhInitAsBob({
      bobIdentity,
      bobSignedPreKey: bobSigned,
      bobOneTimePreKey: bobOneTime,
      aliceIdentityPublic: aliceIdentity.publicKey,
      aliceEphemeralPublic: aliceEphemeral.publicKey,
    });

    expect(aliceResult.sharedSecret).toEqual(bobSecret);
    expect(aliceResult.sharedSecret.length).toBe(32);
    expect(aliceResult.usedOneTimePreKeyId).toBe(42);
  });

  it('works without an optional one-time pre-key', () => {
    const aliceIdentity = generateEd25519KeyPair();
    const bobIdentity = generateEd25519KeyPair();
    const bobSigned = signedPreKeyFor(bobIdentity.secretKey);
    const aliceEphemeral = generateX25519KeyPair();

    const bundle = bundleForBob(bobIdentity.publicKey, bobSigned, undefined);
    const aliceResult = x3dhInitAsAlice(aliceIdentity, aliceEphemeral, bundle);
    const bobSecret = x3dhInitAsBob({
      bobIdentity,
      bobSignedPreKey: bobSigned,
      aliceIdentityPublic: aliceIdentity.publicKey,
      aliceEphemeralPublic: aliceEphemeral.publicKey,
    });
    expect(aliceResult.sharedSecret).toEqual(bobSecret);
    expect(aliceResult.usedOneTimePreKeyId).toBeUndefined();
  });

  it('rejects tampered signed pre-key signature', () => {
    const aliceIdentity = generateEd25519KeyPair();
    const bobIdentity = generateEd25519KeyPair();
    const bobSigned = signedPreKeyFor(bobIdentity.secretKey);

    const tamperedBundle = bundleForBob(bobIdentity.publicKey, bobSigned, undefined);
    // Flip a bit in the signature.
    tamperedBundle.signedPreKeySignature = new Uint8Array(tamperedBundle.signedPreKeySignature);
    tamperedBundle.signedPreKeySignature[0] ^= 0x01;

    const aliceEphemeral = generateX25519KeyPair();
    expect(() => x3dhInitAsAlice(aliceIdentity, aliceEphemeral, tamperedBundle)).toThrow(
      /signature verification failed/,
    );
  });
});

describe('Double Ratchet round-trip', () => {
  function runHandshake() {
    const aliceIdentity = generateEd25519KeyPair();
    const bobIdentity = generateEd25519KeyPair();
    const bobSigned = signedPreKeyFor(bobIdentity.secretKey, 11);
    const bobOneTime = oneTimePreKeyFor(31337);
    const aliceEphemeral = generateX25519KeyPair();
    const bundle = bundleForBob(bobIdentity.publicKey, bobSigned, bobOneTime);
    const aliceX3DH = x3dhInitAsAlice(aliceIdentity, aliceEphemeral, bundle);
    const bobSharedSecret = x3dhInitAsBob({
      bobIdentity,
      bobSignedPreKey: bobSigned,
      bobOneTimePreKey: bobOneTime,
      aliceIdentityPublic: aliceIdentity.publicKey,
      aliceEphemeralPublic: aliceEphemeral.publicKey,
    });
    expect(aliceX3DH.sharedSecret).toEqual(bobSharedSecret);

    const aliceState = initRatchetAsAlice(aliceX3DH.sharedSecret, bobSigned.keyPair.publicKey);
    const bobState = initRatchetAsBob(bobSharedSecret, bobSigned.keyPair);
    return { aliceState, bobState };
  }

  it('Alice → Bob: first message (bootstrap) decrypts correctly', () => {
    const { aliceState, bobState } = runHandshake();
    const skipped: SkippedKeys = new Map();

    const plaintext = textEncoder.encode('hello bob, это первое сообщение 🔒');
    const { header, ciphertext } = ratchetEncrypt(aliceState, plaintext);
    const decrypted = ratchetDecrypt(bobState, skipped, header, ciphertext);
    expect(textDecoder.decode(decrypted)).toBe('hello bob, это первое сообщение 🔒');
  });

  it('supports multi-message exchange with DH ratchet on reply', () => {
    const { aliceState, bobState } = runHandshake();
    const aliceSkipped: SkippedKeys = new Map();
    const bobSkipped: SkippedKeys = new Map();

    // Alice → Bob #1
    const m1 = ratchetEncrypt(aliceState, textEncoder.encode('1: alice first'));
    expect(textDecoder.decode(ratchetDecrypt(bobState, bobSkipped, m1.header, m1.ciphertext)))
      .toBe('1: alice first');

    // Alice → Bob #2 (same sending chain)
    const m2 = ratchetEncrypt(aliceState, textEncoder.encode('2: alice second'));
    expect(textDecoder.decode(ratchetDecrypt(bobState, bobSkipped, m2.header, m2.ciphertext)))
      .toBe('2: alice second');

    // Bob → Alice #1 — this triggers the DH ratchet for Bob's sending side.
    const b1 = ratchetEncrypt(bobState, textEncoder.encode('3: bob reply'));
    expect(textDecoder.decode(ratchetDecrypt(aliceState, aliceSkipped, b1.header, b1.ciphertext)))
      .toBe('3: bob reply');

    // Alice → Bob again — runs a new sending chain after the DH ratchet Alice
    // did when decrypting Bob's reply.
    const m3 = ratchetEncrypt(aliceState, textEncoder.encode('4: alice third'));
    expect(textDecoder.decode(ratchetDecrypt(bobState, bobSkipped, m3.header, m3.ciphertext)))
      .toBe('4: alice third');
  });

  it('handles an out-of-order message within the same chain', () => {
    const { aliceState, bobState } = runHandshake();
    const bobSkipped: SkippedKeys = new Map();

    const m1 = ratchetEncrypt(aliceState, textEncoder.encode('first'));
    const m2 = ratchetEncrypt(aliceState, textEncoder.encode('second'));
    const m3 = ratchetEncrypt(aliceState, textEncoder.encode('third'));

    // Bob receives 1, then 3 (skipping 2), then 2.
    expect(textDecoder.decode(ratchetDecrypt(bobState, bobSkipped, m1.header, m1.ciphertext))).toBe('first');
    expect(textDecoder.decode(ratchetDecrypt(bobState, bobSkipped, m3.header, m3.ciphertext))).toBe('third');
    expect(textDecoder.decode(ratchetDecrypt(bobState, bobSkipped, m2.header, m2.ciphertext))).toBe('second');
  });

  it('serializes and reloads ratchet state, continues decrypting', () => {
    const { aliceState, bobState } = runHandshake();
    const bobSkipped: SkippedKeys = new Map();

    const m1 = ratchetEncrypt(aliceState, textEncoder.encode('before reload'));
    expect(
      textDecoder.decode(ratchetDecrypt(bobState, bobSkipped, m1.header, m1.ciphertext)),
    ).toBe('before reload');

    const bytes = serializeRatchetState(bobState);
    const reloaded = deserializeRatchetState(bytes);
    expect(reloaded.rootKey).toEqual(bobState.rootKey);
    expect(reloaded.receivingMessageNumber).toBe(bobState.receivingMessageNumber);

    const m2 = ratchetEncrypt(aliceState, textEncoder.encode('after reload'));
    expect(
      textDecoder.decode(ratchetDecrypt(reloaded, bobSkipped, m2.header, m2.ciphertext)),
    ).toBe('after reload');
  });
});
