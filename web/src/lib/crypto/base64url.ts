// Base64url (RFC 4648 §5, no padding) encoding used for all key/ciphertext
// transport between Orbit frontend and backend.
//
// Backend (Go) uses `base64.RawURLEncoding` — this must match byte-for-byte.

const B64URL_CHARS = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';

export function encodeBase64Url(bytes: Uint8Array): string {
  let output = '';
  let i = 0;
  const len = bytes.length;

  while (i + 3 <= len) {
    const b0 = bytes[i++];
    const b1 = bytes[i++];
    const b2 = bytes[i++];
    output += B64URL_CHARS[b0 >> 2];
    output += B64URL_CHARS[((b0 & 0x03) << 4) | (b1 >> 4)];
    output += B64URL_CHARS[((b1 & 0x0f) << 2) | (b2 >> 6)];
    output += B64URL_CHARS[b2 & 0x3f];
  }

  const rem = len - i;
  if (rem === 1) {
    const b0 = bytes[i];
    output += B64URL_CHARS[b0 >> 2];
    output += B64URL_CHARS[(b0 & 0x03) << 4];
  } else if (rem === 2) {
    const b0 = bytes[i];
    const b1 = bytes[i + 1];
    output += B64URL_CHARS[b0 >> 2];
    output += B64URL_CHARS[((b0 & 0x03) << 4) | (b1 >> 4)];
    output += B64URL_CHARS[(b1 & 0x0f) << 2];
  }

  return output;
}

export function decodeBase64Url(input: string): Uint8Array {
  const len = input.length;
  if (len === 0) return new Uint8Array(0);

  // Output length derivation from base64url without padding.
  let outLen = Math.floor((len * 3) / 4);
  const rem = len & 3;
  if (rem === 1) {
    throw new Error('base64url: invalid input length');
  }

  const out = new Uint8Array(outLen);
  let oi = 0;
  let buf = 0;
  let bits = 0;

  for (let i = 0; i < len; i++) {
    const ch = input.charCodeAt(i);
    let v: number;
    if (ch >= 65 && ch <= 90) v = ch - 65;          // A-Z → 0-25
    else if (ch >= 97 && ch <= 122) v = ch - 71;    // a-z → 26-51
    else if (ch >= 48 && ch <= 57) v = ch + 4;      // 0-9 → 52-61
    else if (ch === 45) v = 62;                     // -
    else if (ch === 95) v = 63;                     // _
    else throw new Error(`base64url: invalid character at ${i}`);

    buf = (buf << 6) | v;
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      out[oi++] = (buf >> bits) & 0xff;
    }
  }

  return out.subarray(0, oi);
}
