// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package model

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors
var (
	ErrPushSubscriptionLimitReached = errors.New("maximum of 10 push subscriptions per user")
	ErrMediaNotOwned                = errors.New("media file does not belong to the sender")
)

type User struct {
	ID                uuid.UUID  `json:"id"`
	Email             string     `json:"email"`
	Username          *string    `json:"username,omitempty"`
	DisplayName       string     `json:"display_name"`
	AvatarURL         *string    `json:"avatar_url,omitempty"`
	Bio               *string    `json:"bio,omitempty"`
	Phone             *string    `json:"phone,omitempty"`
	Status            string     `json:"status"`
	CustomStatus      *string    `json:"custom_status,omitempty"`
	CustomStatusEmoji *string    `json:"custom_status_emoji,omitempty"`
	Role              string     `json:"role"`
	AccountType       string     `json:"account_type"`
	IsActive          bool       `json:"is_active"`
	DeactivatedAt     *time.Time `json:"deactivated_at,omitempty"`
	DeactivatedBy     *uuid.UUID `json:"deactivated_by,omitempty"`
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
	IsProtected        bool       `json:"is_protected"`
	MaxMembers         int        `json:"max_members"`
	DefaultPermissions int64      `json:"default_permissions"`
	SlowModeSeconds    int        `json:"slow_mode_seconds"`
	// Welcome flow (mig 069). Always returned so the chat-settings UI can
	// render the "Default for new users" Switcher without a follow-up fetch.
	// Default false / 0 for chats created before mig 069 ran.
	IsDefaultForNewUsers bool `json:"is_default_for_new_users"`
	DefaultJoinOrder     int  `json:"default_join_order"`
	IsPinned             bool `json:"is_pinned"`
	IsMuted              bool `json:"is_muted"`
	IsArchived           bool `json:"is_archived"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type ChatListItem struct {
	Chat
	LastMessage *Message   `json:"last_message,omitempty"`
	MemberCount int        `json:"member_count"`
	UnreadCount int        `json:"unread_count"`
	OtherUser   *User      `json:"other_user,omitempty"`
	DraftText   *string    `json:"draft_text,omitempty"`
	DraftDate   *time.Time `json:"draft_date,omitempty"`
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
	IsPinned          bool       `json:"is_pinned"`
	IsMuted           bool       `json:"is_muted"`
	IsArchived        bool       `json:"is_archived"`
	// Joined user data
	DisplayName string  `json:"display_name,omitempty"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
}

type ChatMemberPreferences struct {
	IsPinned   *bool `json:"is_pinned,omitempty"`
	IsMuted    *bool `json:"is_muted,omitempty"`
	IsArchived *bool `json:"is_archived,omitempty"`
}

type Message struct {
	ID               uuid.UUID       `json:"id"`
	ChatID           uuid.UUID       `json:"chat_id"`
	SenderID         *uuid.UUID      `json:"sender_id,omitempty"`
	Type             string          `json:"type"`
	Content          *string         `json:"content,omitempty"`
	Entities         json.RawMessage `json:"entities,omitempty"`
	ReplyToID        *uuid.UUID      `json:"reply_to_id,omitempty"`
	ReplyToSeqNum    *int64          `json:"reply_to_sequence_number,omitempty"`
	IsEdited         bool            `json:"is_edited"`
	IsDeleted        bool            `json:"is_deleted"`
	IsPinned         bool            `json:"is_pinned"`
	IsForwarded      bool            `json:"is_forwarded"`
	ForwardedFrom    *uuid.UUID      `json:"forwarded_from,omitempty"`
	GroupedID        *string         `json:"grouped_id,omitempty"`
	IsOneTime        bool            `json:"is_one_time"`
	SequenceNumber   int64           `json:"sequence_number"`
	CreatedAt        time.Time       `json:"created_at"`
	EditedAt         *time.Time      `json:"edited_at,omitempty"`
	ViewedAt         *time.Time      `json:"viewed_at,omitempty"`
	ViewedBy         *uuid.UUID      `json:"viewed_by,omitempty"`
	// Bot extensions (migration 044)
	ReplyMarkup json.RawMessage `json:"reply_markup,omitempty"`
	ViaBotID    *uuid.UUID      `json:"via_bot_id,omitempty"`
	// Joined sender data
	SenderName      string  `json:"sender_name,omitempty"`
	SenderAvatarURL *string `json:"sender_avatar_url,omitempty"`
	// Media attachments (populated via message_media JOIN)
	MediaAttachments []MediaAttachment `json:"media_attachments,omitempty"`
	Reactions        []ReactionSummary `json:"reactions,omitempty"`
	Poll             *Poll             `json:"poll,omitempty"`
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
	WaveformData     []int    `json:"waveform_data,omitempty"`
	Position         int      `json:"position"`
	IsSpoiler        bool     `json:"is_spoiler"`
	IsOneTime        bool     `json:"is_one_time"`
	ProcessingStatus string   `json:"processing_status"`
}

// SharedMediaItem wraps a MediaAttachment with the parent message context,
// so the frontend can build full ApiMessage objects for the shared media gallery.
type SharedMediaItem struct {
	MessageID      string          `json:"message_id"`
	SequenceNumber int64           `json:"sequence_number"`
	ChatID         string          `json:"chat_id"`
	SenderID       string          `json:"sender_id"`
	Content        string          `json:"content,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	Attachment     MediaAttachment `json:"attachment"`
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

// Phase 4: Settings & Privacy

type PrivacySettings struct {
	UserID    uuid.UUID `json:"user_id"`
	LastSeen  string    `json:"last_seen"`
	Avatar    string    `json:"avatar"`
	Phone     string    `json:"phone"`
	Calls     string    `json:"calls"`
	Groups    string    `json:"groups"`
	Forwarded string    `json:"forwarded"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BlockedUser struct {
	UserID        uuid.UUID `json:"user_id"`
	BlockedUserID uuid.UUID `json:"blocked_user_id"`
	CreatedAt     time.Time `json:"created_at"`
	// Joined user data
	DisplayName string  `json:"display_name,omitempty"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
}

type UserSettings struct {
	UserID      uuid.UUID `json:"user_id"`
	Theme       string    `json:"theme"`
	Language    string    `json:"language"`
	FontSize    int       `json:"font_size"`
	SendByEnter bool      `json:"send_by_enter"`
	DNDFrom     *string   `json:"dnd_from,omitempty"`
	DNDUntil    *string   `json:"dnd_until,omitempty"`
	DefaultTranslateLang *string `json:"default_translate_lang,omitempty"`
	CanTranslate         bool    `json:"can_translate"`
	CanTranslateChats    bool    `json:"can_translate_chats"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// Global notification defaults per chat type
	NotifyUsersMuted    bool `json:"notify_users_muted"`
	NotifyGroupsMuted   bool `json:"notify_groups_muted"`
	NotifyUsersPreview  bool `json:"notify_users_preview"`
	NotifyGroupsPreview bool `json:"notify_groups_preview"`
}


type MessageTranslation struct {
	MessageID uuid.UUID `json:"message_id"`
	Lang      string    `json:"lang"`
	Text      string    `json:"translated_text"`
	CreatedAt time.Time `json:"created_at"`
}
type GlobalNotifySettings struct {
	UsersMuted    bool `json:"users_muted"`
	GroupsMuted   bool `json:"groups_muted"`
	UsersPreview  bool `json:"users_preview"`
	GroupsPreview bool `json:"groups_preview"`
}

type SearchHistoryEntry struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	Query     string    `json:"query"`
	Scope     string    `json:"scope"`
	CreatedAt time.Time `json:"created_at"`
}

type NotificationSettings struct {
	UserID      uuid.UUID  `json:"user_id"`
	ChatID      uuid.UUID  `json:"chat_id"`
	MutedUntil  *time.Time `json:"muted_until,omitempty"`
	Sound       string     `json:"sound"`
	ShowPreview bool       `json:"show_preview"`
}

type PushSubscription struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	Endpoint  string    `json:"endpoint"`
	P256DH    string    `json:"p256dh"`
	Auth      string    `json:"auth"`
	UserAgent *string   `json:"user_agent,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type EncryptedEnvelope struct {
	Version        int                         `json:"v"`
	SenderDeviceID string                      `json:"sender_device_id"`
	Devices        map[string]DeviceCiphertext `json:"devices"`
}

type DeviceCiphertext struct {
	Type int    `json:"type"` // 1=prekey, 2=message
	Body string `json:"body"` // base64url encoded ciphertext
}

// SearchResult represents a unified search result item.
type SearchResult struct {
	Type   string      `json:"type"` // message, user, chat
	Data   interface{} `json:"data"`
	ChatID *string     `json:"chat_id,omitempty"`
	Score  float64     `json:"score,omitempty"`
}

// ValidPrivacyValues are the allowed values for privacy settings fields.
var ValidPrivacyValues = map[string]bool{
	"everyone": true,
	"contacts": true,
	"nobody":   true,
}

var ValidThemes = map[string]bool{
	"auto": true, "light": true, "dark": true,
}

// Phase 5: Rich Messaging

// Reaction represents a user's emoji reaction on a message.
type Reaction struct {
	MessageID uuid.UUID `json:"message_id"`
	UserID    uuid.UUID `json:"user_id"`
	Emoji     string    `json:"emoji"`
	CreatedAt time.Time `json:"created_at"`
	// Joined user data
	DisplayName string  `json:"display_name,omitempty"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
}

// ReactionSummary groups a reaction emoji with its count and sample user IDs.
type ReactionSummary struct {
	Emoji   string   `json:"emoji"`
	Count   int      `json:"count"`
	UserIDs []string `json:"user_ids"`
}

// ChatAvailableReactions controls which emoji reactions are allowed in a chat.
type ChatAvailableReactions struct {
	ChatID        uuid.UUID `json:"chat_id"`
	Mode          string    `json:"mode"` // all, selected, none
	AllowedEmojis []string  `json:"allowed_emojis,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// StickerPack represents a collection of stickers.
type StickerPack struct {
	ID           uuid.UUID  `json:"id"`
	Title        string     `json:"title"`
	ShortName    string     `json:"short_name"`
	Description  *string    `json:"description,omitempty"`
	AuthorID     *uuid.UUID `json:"author_id,omitempty"`
	ThumbnailURL *string    `json:"thumbnail_url,omitempty"`
	IsOfficial   bool       `json:"is_official"`
	IsFeatured   bool       `json:"is_featured"`
	IsAnimated   bool       `json:"is_animated"`
	StickerCount int        `json:"sticker_count"`
	Stickers     []Sticker  `json:"stickers,omitempty"`
	IsInstalled  bool       `json:"is_installed,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// FillPreviewURLs rewrites internal storage URLs to gateway-relative paths on StickerPack.
func (p *StickerPack) FillPreviewURLs() {
	if p.ThumbnailURL != nil {
		rewritten := toGatewayMediaURL(*p.ThumbnailURL)
		p.ThumbnailURL = &rewritten
	}
	for i := range p.Stickers {
		p.Stickers[i].FillPreviewURLs()
	}
}

// Sticker represents a single sticker in a pack.
type Sticker struct {
	ID           uuid.UUID `json:"id"`
	PackID       uuid.UUID `json:"pack_id"`
	Emoji        *string   `json:"emoji,omitempty"`
	FileURL      string    `json:"file_url"`
	FileType     string    `json:"file_type"` // webp, tgs, webm, svg
	Width        *int      `json:"width,omitempty"`
	Height       *int      `json:"height,omitempty"`
	Position     int       `json:"position"`
	ThumbnailURL *string   `json:"thumbnail_url,omitempty"`
	PreviewURL   *string   `json:"preview_url,omitempty"`
}

// FillPreviewURLs rewrites internal storage URLs to gateway-relative paths
// and populates ThumbnailURL and PreviewURL from FileURL when not explicitly set.
func (s *Sticker) FillPreviewURLs() {
	s.FileURL = toGatewayMediaURL(s.FileURL)
	if s.ThumbnailURL == nil {
		s.ThumbnailURL = &s.FileURL
	} else {
		rewritten := toGatewayMediaURL(*s.ThumbnailURL)
		s.ThumbnailURL = &rewritten
	}
	if s.PreviewURL == nil {
		s.PreviewURL = &s.FileURL
	} else {
		rewritten := toGatewayMediaURL(*s.PreviewURL)
		s.PreviewURL = &rewritten
	}
}

// toGatewayMediaURL converts internal S3/MinIO URLs to gateway-relative paths.
// Input:  http://minio:9000/orbit-media/file/{media_id}/original.octet-stream
// Output: /media/{media_id}
func toGatewayMediaURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	for _, part := range parts {
		if _, err := uuid.Parse(part); err == nil {
			return "/media/" + part
		}
	}
	// If this is an internal URL (has scheme+host) but no UUID was found,
	// do NOT return the raw URL — it would leak internal infrastructure to the client.
	if parsed.Scheme != "" && parsed.Host != "" {
		return ""
	}
	// Already a relative path (e.g. /media/...) — return as-is.
	return rawURL
}

// SavedGIF represents a user's saved GIF from Tenor.
type SavedGIF struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"user_id"`
	TenorID    string    `json:"tenor_id"`
	URL        string    `json:"url"`
	PreviewURL *string   `json:"preview_url,omitempty"`
	Width      *int      `json:"width,omitempty"`
	Height     *int      `json:"height,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// TenorGIF represents a GIF result from the Tenor API.
type TenorGIF struct {
	TenorID    string `json:"tenor_id"`
	URL        string `json:"url"`
	PreviewURL string `json:"preview_url"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Title      string `json:"title,omitempty"`
}

// Poll represents a poll attached to a message.
type Poll struct {
	ID               uuid.UUID       `json:"id"`
	MessageID        uuid.UUID       `json:"message_id"`
	Question         string          `json:"question"`
	IsAnonymous      bool            `json:"is_anonymous"`
	IsMultiple       bool            `json:"is_multiple"`
	IsQuiz           bool            `json:"is_quiz"`
	CorrectOption    *int            `json:"correct_option,omitempty"`
	Solution         *string         `json:"solution,omitempty"`
	SolutionEntities json.RawMessage `json:"solution_entities,omitempty"`
	IsClosed         bool            `json:"is_closed"`
	CloseAt          *time.Time      `json:"close_at,omitempty"`
	Options          []PollOption    `json:"options"`
	TotalVoters      int             `json:"total_voters"`
	CreatedAt        time.Time       `json:"created_at"`
}

// PollOption represents a single option in a poll.
type PollOption struct {
	ID        uuid.UUID `json:"id"`
	PollID    uuid.UUID `json:"poll_id"`
	Text      string    `json:"text"`
	Position  int       `json:"position"`
	Voters    int       `json:"voters"`
	IsChosen  bool      `json:"is_chosen,omitempty"`
	IsCorrect bool      `json:"is_correct,omitempty"`
}

// PollVote represents a user's vote on a poll option.
type PollVote struct {
	PollID   uuid.UUID `json:"poll_id"`
	OptionID uuid.UUID `json:"option_id"`
	UserID   uuid.UUID `json:"user_id"`
	VotedAt  time.Time `json:"voted_at"`
}

type ScheduledPollPayload struct {
	Question         string          `json:"question"`
	Options          []string        `json:"options"`
	IsAnonymous      bool            `json:"is_anonymous"`
	IsMultiple       bool            `json:"is_multiple"`
	IsQuiz           bool            `json:"is_quiz"`
	CorrectOption    *int            `json:"correct_option,omitempty"`
	Solution         *string         `json:"solution,omitempty"`
	SolutionEntities json.RawMessage `json:"solution_entities,omitempty"`
}

// ScheduledMessage represents a message scheduled for future delivery.
type ScheduledMessage struct {
	ID               uuid.UUID             `json:"id"`
	ChatID           uuid.UUID             `json:"chat_id"`
	SenderID         uuid.UUID             `json:"sender_id"`
	Content          *string               `json:"content,omitempty"`
	Entities         json.RawMessage       `json:"entities,omitempty"`
	ReplyToID        *uuid.UUID            `json:"reply_to_id,omitempty"`
	ReplyToSeqNum    *int64                `json:"reply_to_sequence_number,omitempty"`
	Type             string                `json:"type"`
	MediaIDs         []uuid.UUID           `json:"media_ids,omitempty"`
	MediaAttachments []MediaAttachment     `json:"media_attachments,omitempty"`
	IsSpoiler        bool                  `json:"is_spoiler"`
	PollPayload      *ScheduledPollPayload `json:"poll,omitempty"`
	ScheduledAt      time.Time             `json:"scheduled_at"`
	IsSent           bool                  `json:"is_sent"`
	SentAt           *time.Time            `json:"sent_at,omitempty"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
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
