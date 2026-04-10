# Orbit Signal Protocol Design

## Trust Model
- Server stores public keys and delivers ciphertext. Server never sees plaintext of E2E messages.
- "Zero-Knowledge" means: server admin cannot read DM content. Server CAN see: chat_id, sender_id, timestamps, sequence_number, delivery state, message size.
- Compliance/Escrow: deferred. Schema ready (compliance_keys table), flow not implemented in Phase 7.
- Sealed Sender: not implemented. sender_id in metadata is acceptable for corporate audit.

## Scope
- Phase 7: E2E for DM only.
- Groups, channels, bots, AI, integrations: non-E2E.
- Old messages: not re-encrypted retroactively.

## Key Types
| Key | Algorithm | Size | Rotation |
|-----|-----------|------|----------|
| Identity Key | Ed25519 | 32B pub / 64B priv | Once per device (permanent) |
| Signed PreKey | X25519 | 32B pub / 32B priv | Weekly |
| One-Time PreKey | X25519 | 32B pub / 32B priv | Consumed on first use, replenished in batches of 100 |

Identity keys authenticate the device and sign rotating prekeys.
Signed prekeys provide asynchronous session bootstrap without exposing private material to the server.
One-time prekeys reduce replay risk during initial X3DH session creation and are deleted after first use.

## Key Lifecycle
1. Device registration: generate Identity Key Pair (Ed25519) + Signed PreKey (X25519) + 100 One-Time PreKeys (X25519)
2. Upload public parts to server via POST /api/v1/keys/*
3. Session init (X3DH): requester fetches bundle, computes shared secret
4. Message exchange: Double Ratchet - new symmetric key per message
5. Signed PreKey rotation: weekly, old signed prekey kept for 48h overlap
6. One-Time PreKey replenishment: when count < 20, client uploads new batch of 100

Clients are responsible for local private key storage in IndexedDB or native secure storage.
Server-side validation checks lengths and bundle completeness, but never derives or stores shared secrets.

## Device Model
- Each browser/app instance = one device with unique device_id (UUID)
- Session (JWT) is bound to device_id
- Device has its own key material (identity key, signed prekey, one-time prekeys)
- On logout: session deleted, device keys optionally preserved for re-login
- On deactivation: all sessions and device keys purged

Multi-device delivery means one logical message fan-outs into multiple encrypted payloads, one per target device.
Sender devices other than the active sender are included so message history stays readable across the sender's own devices.

## Encrypted Message Envelope
Stored in messages.encrypted_content (BYTEA), transmitted as JSON:
```json
{
  "v": 1,
  "sender_device_id": "uuid-string",
  "devices": {
    "recipient-device-id-1": {
      "type": 1,
      "body": "base64url-encoded-ciphertext"
    },
    "recipient-device-id-2": {
      "type": 1,
      "body": "base64url-encoded-ciphertext"
    }
  }
}
```
- `v`: envelope version (always 1)
- `type`: 1 = PreKeyWhisperMessage (session init), 2 = WhisperMessage (normal)
- `body`: opaque ciphertext blob - server doesn't parse this
- Each entry is for one recipient device (includes sender's other devices)

Envelope JSON is serialized as raw bytes before database storage.
The server may validate top-level JSON well-formedness for transport, but it must not inspect or transform ciphertext blobs.

## API Key Format
All key fields in JSON API use base64url encoding (RFC 4648 section 5, no padding).
In database (BYTEA): raw bytes.

Handlers decode base64url input into raw byte slices before passing data to the service layer.
Services validate key lengths and reject malformed uploads with bad-request errors.

## Push Notifications
E2E chats (is_encrypted=true): push body = "Новое сообщение", no content preview.
Non-E2E chats: unchanged (plaintext preview up to 100 chars).

Gateway decides preview suppression based on encrypted message metadata, not by decrypting content.

## Search
E2E chats excluded from Meilisearch indexing.
Client-side search: deferred to frontend implementation.

Search documents for encrypted messages contain no plaintext fallback and should be skipped entirely.

## Disappearing Messages
Server-side: cron job deletes messages where expires_at < NOW().
Client-side: local cleanup on timer (frontend implementation).
Timer options: 24h / 7d / 30d / Off.
Set per-chat via PUT /api/v1/chats/:id/disappearing.

Expiry is attached at message creation time so old non-expiring messages remain untouched.

## Feature Flags
feature_flags table: key TEXT PK, enabled BOOLEAN, metadata JSONB.
Flags: e2e_dm_enabled (default false).
Check in chat creation and message send paths.

Feature flags fail closed: if the flag cannot be read, encrypted DM creation stays disabled.

## Rollout
1. e2e_dm_enabled=false - E2E code deployed but inactive
2. e2e_dm_enabled=true - new DMs can be created as E2E (opt-in by client)
3. Future: default E2E for new DMs, then group E2E

Rollout does not migrate historical plaintext messages and does not backfill missing device keys.
