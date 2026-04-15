// Shared types for Orbit E2E crypto layer.
//
// Protocol baseline: Signal X3DH + Double Ratchet, custom implementation on
// @noble/* primitives (see docs/phase7-design.md §14.3 for rationale).
//
// Wire-compatible key sizes:
//   Ed25519 public  = 32 bytes
//   Ed25519 private = 64 bytes (seed-expanded)
//   Ed25519 sig     = 64 bytes
//   X25519 public   = 32 bytes
//   X25519 private  = 32 bytes

export type KeyPair = {
  publicKey: Uint8Array;
  secretKey: Uint8Array;
};

// Device-local long-term identity.
// Identity key pair is Ed25519 and lives for the lifetime of the device.
export type IdentityKeyPair = KeyPair;

// Rotating signed pre-key — X25519, rotated weekly, signed by identity.
export type SignedPreKey = {
  keyId: number;
  keyPair: KeyPair;
  signature: Uint8Array; // Ed25519 signature over keyPair.publicKey
  createdAt: number;
};

// One-time pre-key — X25519, consumed on first use.
export type OneTimePreKey = {
  keyId: number;
  keyPair: KeyPair;
};

// Public bundle fetched from server for session bootstrap.
// Server atomically consumes one-time pre-key on fetch.
export type PreKeyBundle = {
  userId: string;
  deviceId: string;
  identityKey: Uint8Array;       // 32B Ed25519 public
  signedPreKey: Uint8Array;      // 32B X25519 public
  signedPreKeySignature: Uint8Array; // 64B Ed25519 signature
  signedPreKeyId: number;
  oneTimePreKey?: {
    keyId: number;
    publicKey: Uint8Array;       // 32B X25519 public
  };
};

// Serialized ratchet state — opaque blob stored in IndexedDB.
export type SerializedSession = Uint8Array;

// Envelope entry type per Signal convention (docs/SIGNAL_PROTOCOL.md:65):
//   1 = PreKeyWhisperMessage (session init, first message)
//   2 = WhisperMessage (normal ratchet message)
export const EnvelopeType = {
  PreKey: 1,
  Normal: 2,
} as const;
export type EnvelopeType = typeof EnvelopeType[keyof typeof EnvelopeType];

// Single device entry in the envelope — ciphertext targeted at one recipient device.
export type EnvelopeEntry = {
  type: EnvelopeType;
  body: string; // base64url(ciphertext || header metadata, opaque to server)
};

// Multi-device envelope — one per chat message, one entry per target device.
// Backend stores as raw BYTEA JSON, never parses the inner structure.
export type MessageEnvelope = {
  v: 1;
  sender_device_id: string;
  devices: Record<string, EnvelopeEntry>;
};

// Ciphertext + header that gets wrapped into EnvelopeEntry.body.
// Header carries the data Bob needs to run the ratchet on his side.
export type RatchetMessage = {
  type: EnvelopeType;
  header: RatchetHeader;
  ciphertext: Uint8Array;
};

export type RatchetHeader = {
  // DH ratchet public key of sender's current sending chain.
  dhPublicKey: Uint8Array; // 32B X25519
  // Previous sending chain length — needed for skipped message key derivation.
  previousChainLength: number;
  // Message index inside the current sending chain.
  messageNumber: number;
  // Bootstrap metadata carried only on type=PreKey.
  bootstrap?: BootstrapHeader;
};

// Data carried by the very first message in a new session so Bob can
// derive the matching root key from his private prekeys.
export type BootstrapHeader = {
  aliceIdentityKey: Uint8Array;   // 32B Ed25519 (Alice's long-term identity, so Bob can verify it)
  aliceEphemeralKey: Uint8Array;  // 32B X25519 ephemeral used by Alice for X3DH
  bobSignedPreKeyId: number;
  bobOneTimePreKeyId?: number;
};

// Double Ratchet state — kept per (peerUserId, peerDeviceId) pair.
// This is the in-memory shape; it is (de)serialized to Uint8Array when
// persisted to IndexedDB.
export type RatchetState = {
  // Root key — seeds new sending/receiving chains on each DH ratchet step.
  rootKey: Uint8Array;             // 32B
  // Current sending DH key pair (ours).
  sendingKeyPair: KeyPair;
  // Current remote DH public key (theirs).
  receivingKey: Uint8Array | undefined;
  // Chain keys — seeded from root on DH ratchet, then hashed forward per message.
  sendingChainKey: Uint8Array | undefined;
  receivingChainKey: Uint8Array | undefined;
  // Counters.
  sendingMessageNumber: number;    // Ns
  receivingMessageNumber: number;  // Nr
  previousSendingChainLength: number; // PN — length of prev sending chain before DH ratchet
  // Bootstrap flag — first outgoing message carries PreKey type.
  hasSentFirstMessage: boolean;
};

// Device record returned by GET /keys/:userId/devices.
export type Device = {
  deviceId: string;
  createdAt: string;
  lastSeenAt?: string;
};
