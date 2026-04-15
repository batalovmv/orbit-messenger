import type { MessageEnvelope } from '../../../lib/crypto/types';
import { decodeEncryptedContentField } from './encryptedMessages';

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
