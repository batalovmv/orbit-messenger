// Serialization for the Orbit E2E message envelope.
//
// Wire format (docs/SIGNAL_PROTOCOL.md §"Encrypted Message Envelope"):
//   {
//     "v": 1,
//     "sender_device_id": "uuid",
//     "devices": {
//       "<recipient-device-id>": { "type": 1|2, "body": "base64url" },
//       ...
//     }
//   }
//
// The `body` field embeds a RatchetMessage (header + ciphertext) that we
// serialize ourselves into a binary format below. This binary format is
// opaque to the server.

import { decodeBase64Url, encodeBase64Url } from './base64url';
import type {
  BootstrapHeader,
  EnvelopeEntry,
  MessageEnvelope,
  RatchetHeader,
  RatchetMessage,
} from './types';
import { EnvelopeType } from './types';

// ────────────────────────────────────────────────────────────────────────────
// Top-level envelope JSON (server-visible)
// ────────────────────────────────────────────────────────────────────────────

export function serializeEnvelope(envelope: MessageEnvelope): string {
  return JSON.stringify(envelope);
}

export function parseEnvelope(json: string | MessageEnvelope): MessageEnvelope {
  const raw = typeof json === 'string' ? JSON.parse(json) : json;
  if (!raw || raw.v !== 1) {
    throw new Error('envelope: unsupported version');
  }
  if (typeof raw.sender_device_id !== 'string') {
    throw new Error('envelope: missing sender_device_id');
  }
  if (!raw.devices || typeof raw.devices !== 'object') {
    throw new Error('envelope: missing devices map');
  }
  for (const [deviceId, entry] of Object.entries(raw.devices)) {
    const e = entry as EnvelopeEntry;
    if (typeof deviceId !== 'string' || deviceId.length === 0) {
      throw new Error('envelope: empty device id');
    }
    if (e.type !== EnvelopeType.PreKey && e.type !== EnvelopeType.Normal) {
      throw new Error(`envelope: invalid entry type ${e.type}`);
    }
    if (typeof e.body !== 'string') {
      throw new Error('envelope: entry.body must be base64url string');
    }
  }
  return raw as MessageEnvelope;
}

// ────────────────────────────────────────────────────────────────────────────
// RatchetMessage binary serialization (client-opaque to server)
// ────────────────────────────────────────────────────────────────────────────
//
// Layout (all big-endian u32 for counters):
//
//   u8  version                (0x01)
//   u8  flags                  (bit 0 = has bootstrap)
//   u32 message_number
//   u32 previous_chain_length
//   32B dh_public_key
//   [if bootstrap]
//     32B alice_identity_key
//     32B alice_ephemeral_key
//     u32 bob_signed_prekey_id
//     u8  has_one_time_prekey
//     [u32 bob_one_time_prekey_id]
//   u32 ciphertext_length
//   NB  ciphertext

const RATCHET_MSG_VERSION = 1;
const FLAG_HAS_BOOTSTRAP = 0x01;

function writeU32BE(view: DataView, offset: number, value: number): void {
  view.setUint32(offset, value >>> 0, false);
}

function readU32BE(view: DataView, offset: number): number {
  return view.getUint32(offset, false);
}

export function serializeRatchetMessage(msg: RatchetMessage): Uint8Array {
  const hasBootstrap = !!msg.header.bootstrap;
  const hasOneTime = hasBootstrap && msg.header.bootstrap!.bobOneTimePreKeyId !== undefined;

  let size = 1 + 1 + 4 + 4 + 32; // version + flags + msgNum + prevLen + dhPub
  if (hasBootstrap) {
    size += 32 + 32 + 4 + 1;
    if (hasOneTime) size += 4;
  }
  size += 4 + msg.ciphertext.length;

  const buf = new Uint8Array(size);
  const view = new DataView(buf.buffer);
  let o = 0;

  buf[o++] = RATCHET_MSG_VERSION;
  buf[o++] = hasBootstrap ? FLAG_HAS_BOOTSTRAP : 0;
  writeU32BE(view, o, msg.header.messageNumber); o += 4;
  writeU32BE(view, o, msg.header.previousChainLength); o += 4;
  buf.set(msg.header.dhPublicKey, o); o += 32;

  if (hasBootstrap) {
    const b = msg.header.bootstrap!;
    buf.set(b.aliceIdentityKey, o); o += 32;
    buf.set(b.aliceEphemeralKey, o); o += 32;
    writeU32BE(view, o, b.bobSignedPreKeyId); o += 4;
    buf[o++] = hasOneTime ? 1 : 0;
    if (hasOneTime) {
      writeU32BE(view, o, b.bobOneTimePreKeyId!); o += 4;
    }
  }

  writeU32BE(view, o, msg.ciphertext.length); o += 4;
  buf.set(msg.ciphertext, o); o += msg.ciphertext.length;

  return buf;
}

export function deserializeRatchetMessage(bytes: Uint8Array): RatchetMessage {
  if (bytes.length < 1 + 1 + 4 + 4 + 32 + 4) {
    throw new Error('ratchet message: input too short');
  }
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  let o = 0;

  const version = bytes[o++];
  if (version !== RATCHET_MSG_VERSION) {
    throw new Error(`ratchet message: unsupported version ${version}`);
  }
  const flags = bytes[o++];
  const messageNumber = readU32BE(view, o); o += 4;
  const previousChainLength = readU32BE(view, o); o += 4;
  const dhPublicKey = bytes.slice(o, o + 32); o += 32;

  let bootstrap: BootstrapHeader | undefined;
  if ((flags & FLAG_HAS_BOOTSTRAP) !== 0) {
    if (bytes.length < o + 32 + 32 + 4 + 1) {
      throw new Error('ratchet message: truncated bootstrap');
    }
    const aliceIdentityKey = bytes.slice(o, o + 32); o += 32;
    const aliceEphemeralKey = bytes.slice(o, o + 32); o += 32;
    const bobSignedPreKeyId = readU32BE(view, o); o += 4;
    const hasOneTime = bytes[o++] === 1;
    let bobOneTimePreKeyId: number | undefined;
    if (hasOneTime) {
      if (bytes.length < o + 4) throw new Error('ratchet message: truncated one-time id');
      bobOneTimePreKeyId = readU32BE(view, o); o += 4;
    }
    bootstrap = {
      aliceIdentityKey,
      aliceEphemeralKey,
      bobSignedPreKeyId,
      bobOneTimePreKeyId,
    };
  }

  if (bytes.length < o + 4) {
    throw new Error('ratchet message: missing ciphertext length');
  }
  const ctLen = readU32BE(view, o); o += 4;
  if (bytes.length < o + ctLen) {
    throw new Error('ratchet message: truncated ciphertext');
  }
  const ciphertext = bytes.slice(o, o + ctLen);

  const type: RatchetMessage['type'] = bootstrap ? EnvelopeType.PreKey : EnvelopeType.Normal;
  const header: RatchetHeader = {
    dhPublicKey,
    messageNumber,
    previousChainLength,
    bootstrap,
  };
  return { type, header, ciphertext };
}

// ────────────────────────────────────────────────────────────────────────────
// Envelope entry = base64url(ratchet message binary)
// ────────────────────────────────────────────────────────────────────────────

export function encodeEntry(msg: RatchetMessage): EnvelopeEntry {
  return {
    type: msg.type,
    body: encodeBase64Url(serializeRatchetMessage(msg)),
  };
}

export function decodeEntry(entry: EnvelopeEntry): RatchetMessage {
  const msg = deserializeRatchetMessage(decodeBase64Url(entry.body));
  // Sanity — entry.type must match the flag we encoded.
  if (msg.type !== entry.type) {
    throw new Error('envelope entry: type mismatch');
  }
  return msg;
}
