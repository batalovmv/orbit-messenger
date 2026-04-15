import { decodeBase64Url, encodeBase64Url } from './base64url';

describe('base64url', () => {
  it('encodes empty input to empty string', () => {
    expect(encodeBase64Url(new Uint8Array())).toBe('');
    expect(decodeBase64Url('')).toEqual(new Uint8Array());
  });

  it('round-trips known vectors from RFC 4648 §10', () => {
    const cases: Array<[string, string]> = [
      ['f', 'Zg'],
      ['fo', 'Zm8'],
      ['foo', 'Zm9v'],
      ['foob', 'Zm9vYg'],
      ['fooba', 'Zm9vYmE'],
      ['foobar', 'Zm9vYmFy'],
    ];
    const enc = new TextEncoder();
    const dec = new TextDecoder();
    for (const [plain, b64] of cases) {
      const bytes = enc.encode(plain);
      expect(encodeBase64Url(bytes)).toBe(b64);
      expect(dec.decode(decodeBase64Url(b64))).toBe(plain);
    }
  });

  it('uses url-safe alphabet (- and _ instead of + and /)', () => {
    // Bytes 0xfb 0xff 0xbf → standard base64 "+/+/", base64url "-_-/" etc.
    const bytes = new Uint8Array([0xfb, 0xff, 0xbf]);
    const encoded = encodeBase64Url(bytes);
    expect(encoded).not.toContain('+');
    expect(encoded).not.toContain('/');
    expect(encoded).not.toContain('=');
    expect(decodeBase64Url(encoded)).toEqual(bytes);
  });

  it('round-trips random byte buffers of varying lengths', () => {
    for (let len = 1; len <= 64; len++) {
      const bytes = new Uint8Array(len);
      crypto.getRandomValues(bytes);
      const encoded = encodeBase64Url(bytes);
      const decoded = decodeBase64Url(encoded);
      expect(decoded).toEqual(bytes);
    }
  });

  it('rejects invalid characters', () => {
    expect(() => decodeBase64Url('abc!')).toThrow(/invalid character/);
  });

  it('rejects invalid length (rem === 1)', () => {
    // Length mod 4 == 1 is impossible in base64url.
    expect(() => decodeBase64Url('a')).toThrow(/invalid input length/);
  });
});
