package model

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID                       uuid.UUID  `json:"id"`
	Email                    string     `json:"email"`
	PasswordHash             string     `json:"-"`
	NotificationPriorityMode string     `json:"notification_priority_mode,omitempty"`
	Phone             *string    `json:"phone,omitempty"`
	Username          *string    `json:"username,omitempty"`
	DisplayName       string     `json:"display_name"`
	AvatarURL         *string    `json:"avatar_url,omitempty"`
	Bio               *string    `json:"bio,omitempty"`
	Status            string     `json:"status"`
	CustomStatus      *string    `json:"custom_status,omitempty"`
	CustomStatusEmoji *string    `json:"custom_status_emoji,omitempty"`
	Role              string     `json:"role"`
	AccountType       string     `json:"account_type"`
	IsActive          bool       `json:"is_active"`
	DeactivatedAt     *time.Time `json:"deactivated_at,omitempty"`
	DeactivatedBy     *uuid.UUID `json:"deactivated_by,omitempty"`
	TOTPSecret        *string    `json:"-"`
	TOTPEnabled       bool       `json:"totp_enabled"`
	InvitedBy         *uuid.UUID `json:"invited_by,omitempty"`
	InviteCode        *string    `json:"-"`
	LastSeenAt        *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Session struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	DeviceID  *uuid.UUID `json:"device_id,omitempty"`
	TokenHash string     `json:"-"`
	IPAddress *string    `json:"ip_address,omitempty"`
	UserAgent *string    `json:"user_agent,omitempty"`
	ExpiresAt time.Time  `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

type Invite struct {
	ID        uuid.UUID  `json:"id"`
	Code      string     `json:"code"`
	CreatedBy *uuid.UUID `json:"created_by,omitempty"`
	Email     *string    `json:"email,omitempty"`
	Role      string     `json:"role"`
	MaxUses   int        `json:"max_uses"`
	UseCount  int        `json:"use_count"`
	UsedBy    *uuid.UUID `json:"used_by,omitempty"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	IsActive  bool       `json:"is_active"`
	CreatedAt time.Time  `json:"created_at"`
}

// E2E Key Management

type UserDeviceKeys struct {
	UserID                uuid.UUID `json:"user_id"`
	DeviceID              uuid.UUID `json:"device_id"`
	IdentityKey           []byte    `json:"identity_key"`             // Ed25519 public, 32 bytes
	SignedPreKey          []byte    `json:"signed_prekey"`            // X25519 public, 32 bytes
	SignedPreKeySignature []byte    `json:"signed_prekey_signature"`  // Ed25519 sig, 64 bytes
	SignedPreKeyID        int       `json:"signed_prekey_id"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type OneTimePreKey struct {
	ID        int       `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	DeviceID  uuid.UUID `json:"device_id"`
	KeyID     int       `json:"key_id"`
	PublicKey []byte    `json:"public_key"` // X25519 public, 32 bytes
	Used      bool      `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

type KeyBundle struct {
	IdentityKey           []byte    `json:"identity_key"`
	SignedPreKey          []byte    `json:"signed_prekey"`
	SignedPreKeySignature []byte    `json:"signed_prekey_signature"`
	SignedPreKeyID        int       `json:"signed_prekey_id"`
	OneTimePreKey         []byte    `json:"one_time_prekey,omitempty"` // nil if exhausted
	OneTimePreKeyID       *int      `json:"one_time_prekey_id,omitempty"`
	DeviceID              uuid.UUID `json:"device_id"`
}

type KeyTransparencyEntry struct {
	ID            int       `json:"id"`
	UserID        uuid.UUID `json:"user_id"`
	DeviceID      uuid.UUID `json:"device_id"`
	EventType     string    `json:"event_type"`
	PublicKeyHash string    `json:"public_key_hash"`
	CreatedAt     time.Time `json:"created_at"`
}
