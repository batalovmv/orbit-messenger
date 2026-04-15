// Phase 7.1: structured plaintext payload for encrypted messages.
//
// Before Phase 7.1 the encrypted plaintext carried in the ratchet ciphertext
// was a raw UTF-8 text string. To support media messages we now wrap text +
// media references into a small JSON document. The format is:
//
//   {
//     "v": 1,
//     "text": "optional caption",
//     "media": [
//       {
//         "id":       "<server media uuid>",
//         "key":      "<base64url AES-256-GCM key>",
//         "nonce":    "<base64url 12-byte GCM nonce>",
//         "mime":     "image/jpeg",
//         "filename": "photo.jpg",
//         "size":     123456,
//         "width":    1920,
//         "height":   1080,
//         "duration": 0,
//         "type":     "photo" | "video" | "voice" | "file"
//       }
//     ]
//   }
//
// Backward compatibility: Phase 7.0 messages ship raw text bytes. When
// decoding, if the decrypted plaintext does not start with `{` or fails
// JSON validation, callers fall back to treating it as plain text.
//
// The whole document is placed INSIDE the ratchet ciphertext, so the
// server never sees the media keys, filenames, or dimensions — only the
// ciphertext-opaque blob at `/media/{id}` remains visible to operators.

import { decodeBase64Url, encodeBase64Url } from './base64url';

export type EncryptedMediaKind = 'photo' | 'video' | 'voice' | 'file' | 'gif';

export type EncryptedMediaRef = {
  id: string;
  key: Uint8Array;
  nonce: Uint8Array;
  mime: string;
  filename?: string;
  size: number;
  width?: number;
  height?: number;
  duration?: number;
  type: EncryptedMediaKind;
};

export type EncryptedPayload = {
  text?: string;
  media: EncryptedMediaRef[];
};

const PAYLOAD_VERSION = 1;

export function serializeEncryptedPayload(payload: EncryptedPayload): Uint8Array {
  const json = {
    v: PAYLOAD_VERSION,
    text: payload.text ?? '',
    media: payload.media.map((m) => ({
      id: m.id,
      key: encodeBase64Url(m.key),
      nonce: encodeBase64Url(m.nonce),
      mime: m.mime,
      filename: m.filename ?? '',
      size: m.size,
      width: m.width ?? 0,
      height: m.height ?? 0,
      duration: m.duration ?? 0,
      type: m.type,
    })),
  };
  return new TextEncoder().encode(JSON.stringify(json));
}

/**
 * Decode a plaintext payload. Returns `undefined` when the bytes are not a
 * v1 payload — callers treat that as a Phase 7.0 raw-text message.
 */
export function tryParseEncryptedPayload(bytes: Uint8Array): EncryptedPayload | undefined {
  if (bytes.length === 0) return undefined;
  if (bytes[0] !== 0x7b /* '{' */) return undefined;

  let json: unknown;
  try {
    json = JSON.parse(new TextDecoder().decode(bytes));
  } catch {
    return undefined;
  }
  if (!json || typeof json !== 'object') return undefined;
  const raw = json as { v?: number; text?: unknown; media?: unknown };
  if (raw.v !== PAYLOAD_VERSION) return undefined;
  if (!Array.isArray(raw.media)) return undefined;

  const media: EncryptedMediaRef[] = [];
  for (const entry of raw.media) {
    if (!entry || typeof entry !== 'object') return undefined;
    const e = entry as Record<string, unknown>;
    if (typeof e.id !== 'string' || typeof e.key !== 'string' || typeof e.nonce !== 'string') {
      return undefined;
    }
    if (typeof e.mime !== 'string' || typeof e.size !== 'number' || typeof e.type !== 'string') {
      return undefined;
    }
    if (!isKnownKind(e.type)) return undefined;

    let key: Uint8Array;
    let nonce: Uint8Array;
    try {
      key = decodeBase64Url(e.key);
      nonce = decodeBase64Url(e.nonce);
    } catch {
      return undefined;
    }
    if (key.length !== 32 || nonce.length !== 12) return undefined;

    media.push({
      id: e.id,
      key,
      nonce,
      mime: e.mime,
      filename: typeof e.filename === 'string' && e.filename.length > 0 ? e.filename : undefined,
      size: e.size,
      width: typeof e.width === 'number' && e.width > 0 ? e.width : undefined,
      height: typeof e.height === 'number' && e.height > 0 ? e.height : undefined,
      duration: typeof e.duration === 'number' && e.duration > 0 ? e.duration : undefined,
      type: e.type,
    });
  }

  return {
    text: typeof raw.text === 'string' && raw.text.length > 0 ? raw.text : undefined,
    media,
  };
}

function isKnownKind(value: string): value is EncryptedMediaKind {
  return value === 'photo' || value === 'video' || value === 'voice' || value === 'file' || value === 'gif';
}
