import {
  decodeEntry,
  deserializeRatchetMessage,
  encodeEntry,
  parseEnvelope,
  serializeEnvelope,
  serializeRatchetMessage,
} from './envelope';
import type { MessageEnvelope, RatchetMessage } from './types';
import { EnvelopeType } from './types';

const dhPub = new Uint8Array(32).fill(0x42);
const ciphertext = new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);

const normalMessage: RatchetMessage = {
  type: EnvelopeType.Normal,
  header: {
    dhPublicKey: dhPub,
    messageNumber: 7,
    previousChainLength: 3,
  },
  ciphertext,
};

const bootstrapMessage: RatchetMessage = {
  type: EnvelopeType.PreKey,
  header: {
    dhPublicKey: dhPub,
    messageNumber: 0,
    previousChainLength: 0,
    bootstrap: {
      aliceIdentityKey: new Uint8Array(32).fill(0x11),
      aliceEphemeralKey: new Uint8Array(32).fill(0x22),
      bobSignedPreKeyId: 42,
      bobOneTimePreKeyId: 9_999,
    },
  },
  ciphertext,
};

describe('ratchet message binary serialization', () => {
  it('round-trips a normal message', () => {
    const bytes = serializeRatchetMessage(normalMessage);
    const parsed = deserializeRatchetMessage(bytes);
    expect(parsed.type).toBe(EnvelopeType.Normal);
    expect(parsed.header.messageNumber).toBe(7);
    expect(parsed.header.previousChainLength).toBe(3);
    expect(parsed.header.dhPublicKey).toEqual(dhPub);
    expect(parsed.ciphertext).toEqual(ciphertext);
    expect(parsed.header.bootstrap).toBeUndefined();
  });

  it('round-trips a bootstrap (PreKey) message with one-time pre-key id', () => {
    const bytes = serializeRatchetMessage(bootstrapMessage);
    const parsed = deserializeRatchetMessage(bytes);
    expect(parsed.type).toBe(EnvelopeType.PreKey);
    expect(parsed.header.bootstrap).toBeDefined();
    expect(parsed.header.bootstrap!.bobSignedPreKeyId).toBe(42);
    expect(parsed.header.bootstrap!.bobOneTimePreKeyId).toBe(9_999);
    expect(parsed.header.bootstrap!.aliceIdentityKey).toEqual(bootstrapMessage.header.bootstrap!.aliceIdentityKey);
    expect(parsed.header.bootstrap!.aliceEphemeralKey).toEqual(bootstrapMessage.header.bootstrap!.aliceEphemeralKey);
    expect(parsed.ciphertext).toEqual(ciphertext);
  });

  it('round-trips a bootstrap message without one-time pre-key id', () => {
    const withoutOtp: RatchetMessage = {
      ...bootstrapMessage,
      header: {
        ...bootstrapMessage.header,
        bootstrap: {
          ...bootstrapMessage.header.bootstrap!,
          bobOneTimePreKeyId: undefined,
        },
      },
    };
    const bytes = serializeRatchetMessage(withoutOtp);
    const parsed = deserializeRatchetMessage(bytes);
    expect(parsed.header.bootstrap!.bobOneTimePreKeyId).toBeUndefined();
  });
});

describe('envelope JSON', () => {
  it('serializes and parses an envelope with a single entry', () => {
    const entry = encodeEntry(normalMessage);
    const envelope: MessageEnvelope = {
      v: 1,
      sender_device_id: 'device-alice',
      devices: { 'device-bob': entry },
    };
    const json = serializeEnvelope(envelope);
    const parsed = parseEnvelope(json);
    expect(parsed.v).toBe(1);
    expect(parsed.sender_device_id).toBe('device-alice');
    expect(parsed.devices['device-bob']).toEqual(entry);

    const decoded = decodeEntry(parsed.devices['device-bob']);
    expect(decoded.ciphertext).toEqual(ciphertext);
    expect(decoded.header.messageNumber).toBe(7);
  });

  it('rejects unsupported envelope versions', () => {
    const envelope = {
      v: 99,
      sender_device_id: 'x',
      devices: {},
    } as unknown as MessageEnvelope;
    expect(() => parseEnvelope(envelope as unknown as string)).toThrow(/unsupported version/);
  });

  it('rejects entries with unknown type', () => {
    const json = JSON.stringify({
      v: 1,
      sender_device_id: 'alice',
      devices: { bob: { type: 7, body: 'AA' } },
    });
    expect(() => parseEnvelope(json)).toThrow(/invalid entry type/);
  });
});
