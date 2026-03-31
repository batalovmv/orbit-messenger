package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID                uuid.UUID  `json:"id"`
	Email             string     `json:"email"`
	DisplayName       string     `json:"display_name"`
	AvatarURL         *string    `json:"avatar_url,omitempty"`
	Bio               *string    `json:"bio,omitempty"`
	Phone             *string    `json:"phone,omitempty"`
	Status            string     `json:"status"`
	CustomStatus      *string    `json:"custom_status,omitempty"`
	CustomStatusEmoji *string    `json:"custom_status_emoji,omitempty"`
	Role              string     `json:"role"`
	LastSeenAt        *time.Time `json:"last_seen_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Chat struct {
	ID                 uuid.UUID  `json:"id"`
	Type               string     `json:"type"`
	Name               *string    `json:"name,omitempty"`
	Description        *string    `json:"description,omitempty"`
	AvatarURL          *string    `json:"avatar_url,omitempty"`
	CreatedBy          *uuid.UUID `json:"created_by,omitempty"`
	IsEncrypted        bool       `json:"is_encrypted"`
	MaxMembers         int        `json:"max_members"`
	DefaultPermissions int64      `json:"default_permissions"`
	SlowModeSeconds    int        `json:"slow_mode_seconds"`
	IsSignatures       bool       `json:"is_signatures"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type ChatListItem struct {
	Chat
	LastMessage  *Message `json:"last_message,omitempty"`
	MemberCount  int      `json:"member_count"`
	UnreadCount  int      `json:"unread_count"`
	OtherUser    *User    `json:"other_user,omitempty"`
}

type ChatMember struct {
	ChatID            uuid.UUID  `json:"chat_id"`
	UserID            uuid.UUID  `json:"user_id"`
	Role              string     `json:"role"`
	Permissions       int64      `json:"permissions"`
	CustomTitle       *string    `json:"custom_title,omitempty"`
	LastReadMessageID *uuid.UUID `json:"last_read_message_id,omitempty"`
	JoinedAt          time.Time  `json:"joined_at"`
	MutedUntil        *time.Time `json:"muted_until,omitempty"`
	NotificationLevel string     `json:"notification_level"`
	// Joined user data
	DisplayName string  `json:"display_name,omitempty"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
}

type Message struct {
	ID             uuid.UUID  `json:"id"`
	ChatID         uuid.UUID  `json:"chat_id"`
	SenderID       *uuid.UUID `json:"sender_id,omitempty"`
	Type           string     `json:"type"`
	Content        *string         `json:"content,omitempty"`
	Entities       json.RawMessage `json:"entities,omitempty"`
	ReplyToID        *uuid.UUID `json:"reply_to_id,omitempty"`
	ReplyToSeqNum    *int64     `json:"reply_to_sequence_number,omitempty"`
	IsEdited       bool       `json:"is_edited"`
	IsDeleted      bool       `json:"is_deleted"`
	IsPinned       bool       `json:"is_pinned"`
	IsForwarded    bool       `json:"is_forwarded"`
	ForwardedFrom  *uuid.UUID `json:"forwarded_from,omitempty"`
	SequenceNumber int64      `json:"sequence_number"`
	CreatedAt      time.Time  `json:"created_at"`
	EditedAt       *time.Time `json:"edited_at,omitempty"`
	// Joined sender data
	SenderName      string  `json:"sender_name,omitempty"`
	SenderAvatarURL *string `json:"sender_avatar_url,omitempty"`
	// Media attachments (populated via message_media JOIN)
	MediaAttachments []MediaAttachment `json:"media_attachments,omitempty"`
}

// MediaAttachment represents a media file attached to a message.
type MediaAttachment struct {
	MediaID          string   `json:"media_id"`
	Type             string   `json:"type"`
	MimeType         string   `json:"mime_type"`
	URL              string   `json:"url,omitempty"`
	ThumbnailURL     string   `json:"thumbnail_url,omitempty"`
	MediumURL        string   `json:"medium_url,omitempty"`
	OriginalFilename string   `json:"original_filename,omitempty"`
	SizeBytes        int64    `json:"size_bytes"`
	Width            *int     `json:"width,omitempty"`
	Height           *int     `json:"height,omitempty"`
	DurationSeconds  *float64 `json:"duration_seconds,omitempty"`
	WaveformData     []byte   `json:"waveform_data,omitempty"`
	Position         int      `json:"position"`
	IsSpoiler        bool     `json:"is_spoiler"`
	ProcessingStatus string   `json:"processing_status"`
}

// SharedMediaItem wraps a MediaAttachment with the parent message context,
// so the frontend can build full ApiMessage objects for the shared media gallery.
type SharedMediaItem struct {
	MessageID       string          `json:"message_id"`
	SequenceNumber  int64           `json:"sequence_number"`
	ChatID          string          `json:"chat_id"`
	SenderID        string          `json:"sender_id"`
	Content         string          `json:"content,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	Attachment      MediaAttachment `json:"attachment"`
}

type InviteLink struct {
	ID               uuid.UUID  `json:"id"`
	ChatID           uuid.UUID  `json:"chat_id"`
	CreatorID        uuid.UUID  `json:"creator_id"`
	Hash             string     `json:"hash"`
	Title            *string    `json:"title,omitempty"`
	ExpireAt         *time.Time `json:"expire_at,omitempty"`
	UsageLimit       int        `json:"usage_limit"`
	UsageCount       int        `json:"usage_count"`
	RequiresApproval bool       `json:"requires_approval"`
	IsRevoked        bool       `json:"is_revoked"`
	CreatedAt        time.Time  `json:"created_at"`
}

type JoinRequest struct {
	ChatID     uuid.UUID  `json:"chat_id"`
	UserID     uuid.UUID  `json:"user_id"`
	Message    *string    `json:"message,omitempty"`
	Status     string     `json:"status"`
	ReviewedBy *uuid.UUID `json:"reviewed_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	// Joined user data
	DisplayName string  `json:"display_name,omitempty"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
}
