// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"encoding/json"
)

type SendMessageRequest struct {
	ChatID           string          `json:"chat_id"`
	Text             string          `json:"text"`
	ParseMode        string          `json:"parse_mode,omitempty"`
	Entities         []MessageEntity `json:"entities,omitempty"`
	ReplyMarkup      json.RawMessage `json:"reply_markup,omitempty"`
	ReplyToMessageID *string         `json:"reply_to_message_id,omitempty"`
}

type EditMessageRequest struct {
	ChatID      string          `json:"chat_id"`
	MessageID   string          `json:"message_id"`
	Text        string          `json:"text"`
	ParseMode   string          `json:"parse_mode,omitempty"`
	Entities    []MessageEntity `json:"entities,omitempty"`
	ReplyMarkup json.RawMessage `json:"reply_markup,omitempty"`
}

type DeleteMessageRequest struct {
	ChatID    string `json:"chat_id"`
	MessageID string `json:"message_id"`
}

type SendPhotoRequest struct {
	ChatID           string          `json:"chat_id"`
	Caption          string          `json:"caption,omitempty"`
	ReplyMarkup      json.RawMessage `json:"reply_markup,omitempty"`
	ReplyToMessageID *string         `json:"reply_to_message_id,omitempty"`
}

type SendDocumentRequest struct {
	ChatID           string          `json:"chat_id"`
	Caption          string          `json:"caption,omitempty"`
	ReplyMarkup      json.RawMessage `json:"reply_markup,omitempty"`
	ReplyToMessageID *string         `json:"reply_to_message_id,omitempty"`
}

type SendVideoRequest struct {
	ChatID           string          `json:"chat_id"`
	Caption          string          `json:"caption,omitempty"`
	Duration         int             `json:"duration,omitempty"`
	Width            int             `json:"width,omitempty"`
	Height           int             `json:"height,omitempty"`
	ReplyMarkup      json.RawMessage `json:"reply_markup,omitempty"`
	ReplyToMessageID *string         `json:"reply_to_message_id,omitempty"`
}

type SendAudioRequest struct {
	ChatID           string          `json:"chat_id"`
	Caption          string          `json:"caption,omitempty"`
	Duration         int             `json:"duration,omitempty"`
	Performer        string          `json:"performer,omitempty"`
	Title            string          `json:"title,omitempty"`
	ReplyMarkup      json.RawMessage `json:"reply_markup,omitempty"`
	ReplyToMessageID *string         `json:"reply_to_message_id,omitempty"`
}

type SendVoiceRequest struct {
	ChatID           string          `json:"chat_id"`
	Caption          string          `json:"caption,omitempty"`
	Duration         int             `json:"duration,omitempty"`
	ReplyMarkup      json.RawMessage `json:"reply_markup,omitempty"`
	ReplyToMessageID *string         `json:"reply_to_message_id,omitempty"`
}

type CopyMessageRequest struct {
	ChatID           string          `json:"chat_id"`
	FromChatID       string          `json:"from_chat_id"`
	MessageID        string          `json:"message_id"`
	Caption          *string         `json:"caption,omitempty"`
	ParseMode        string          `json:"parse_mode,omitempty"`
	CaptionEntities  []MessageEntity `json:"caption_entities,omitempty"`
	ReplyMarkup      json.RawMessage `json:"reply_markup,omitempty"`
	ReplyToMessageID *string         `json:"reply_to_message_id,omitempty"`
}

type ForwardMessageRequest struct {
	ChatID     string `json:"chat_id"`
	FromChatID string `json:"from_chat_id"`
	MessageID  string `json:"message_id"`
}

type EditReplyMarkupRequest struct {
	ChatID      string          `json:"chat_id"`
	MessageID   string          `json:"message_id"`
	ReplyMarkup json.RawMessage `json:"reply_markup"`
}

type EditCaptionRequest struct {
	ChatID          string          `json:"chat_id"`
	MessageID       string          `json:"message_id"`
	Caption         string          `json:"caption"`
	ParseMode       string          `json:"parse_mode,omitempty"`
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	ReplyMarkup     json.RawMessage `json:"reply_markup,omitempty"`
}

type SendChatActionRequest struct {
	ChatID string `json:"chat_id"`
	Action string `json:"action"` // typing|upload_photo|upload_document|upload_video|upload_voice
}

type PinChatMessageRequest struct {
	ChatID              string `json:"chat_id"`
	MessageID           string `json:"message_id"`
	DisableNotification bool   `json:"disable_notification,omitempty"`
}

type UnpinChatMessageRequest struct {
	ChatID    string `json:"chat_id"`
	MessageID string `json:"message_id"`
}

type BotCommandItem struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

type SetMyCommandsRequest struct {
	Commands []BotCommandItem `json:"commands"`
}

type GetChatMemberRequest struct {
	ChatID string `json:"chat_id"`
	UserID string `json:"user_id"`
}

type BanChatMemberRequest struct {
	ChatID    string `json:"chat_id"`
	UserID    string `json:"user_id"`
	UntilDate *int64 `json:"until_date,omitempty"`
}

type RestrictChatMemberRequest struct {
	ChatID          string `json:"chat_id"`
	UserID          string `json:"user_id"`
	PermissionsMask int64  `json:"permissions_mask"`
	UntilDate       *int64 `json:"until_date,omitempty"`
}

type AnswerCallbackQueryRequest struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
	ShowAlert       bool   `json:"show_alert,omitempty"`
}

type SetWebhookRequest struct {
	URL    string `json:"url"`
	Secret string `json:"secret,omitempty"`
}

type BotAPIResponse struct {
	OK          bool   `json:"ok"`
	Result      any    `json:"result,omitempty"`
	Description string `json:"description,omitempty"`
	ErrorCode   int    `json:"error_code,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *APIMessage    `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

type APIMessage struct {
	MessageID      string          `json:"message_id"`
	ChatID         string          `json:"chat_id"`
	FromID         string          `json:"from_id"`
	FromName       string          `json:"from_name"`
	From           *APIUser        `json:"from,omitempty"`
	Chat           *APIChat        `json:"chat,omitempty"`
	Text           string          `json:"text,omitempty"`
	Caption        string          `json:"caption,omitempty"`
	Entities       []OrbitEntity   `json:"entities,omitempty"`
	CaptionEntities []OrbitEntity  `json:"caption_entities,omitempty"`
	Document       *APIDocument    `json:"document,omitempty"`
	Photo          []APIPhotoSize  `json:"photo,omitempty"`
	Video          *APIVideo       `json:"video,omitempty"`
	Audio          *APIAudio       `json:"audio,omitempty"`
	Voice          *APIVoice       `json:"voice,omitempty"`
	Date           int64           `json:"date"`
	ReplyToMessage *APIMessage     `json:"reply_to_message,omitempty"`
}

// APIUser is the Bot API user payload exposed inside Update.message.from.
type APIUser struct {
	ID           string `json:"id"`
	IsBot        bool   `json:"is_bot,omitempty"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name,omitempty"`
	Username     string `json:"username,omitempty"`
	LanguageCode string `json:"language_code,omitempty"`
	// Corporate identity fields, populated only when the bot opted-in via
	// share_user_emails (migration 065). Defaults to empty for safety.
	Email  string `json:"email,omitempty"`
	LDAPDN string `json:"ldap_dn,omitempty"`
}

// APIChat is the Bot API chat payload.
type APIChat struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
}

// APIPhotoSize represents one size of a photo (TG sends an array; we always
// include the original size and one optional thumbnail).
type APIPhotoSize struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FileSize     int64  `json:"file_size,omitempty"`
}

// APIDocument is the file attachment payload.
type APIDocument struct {
	FileID       string        `json:"file_id"`
	FileUniqueID string        `json:"file_unique_id"`
	FileName     string        `json:"file_name,omitempty"`
	MimeType     string        `json:"mime_type,omitempty"`
	FileSize     int64         `json:"file_size,omitempty"`
	Thumbnail    *APIPhotoSize `json:"thumbnail,omitempty"`
}

// APIVideo is the video attachment payload.
type APIVideo struct {
	FileID       string        `json:"file_id"`
	FileUniqueID string        `json:"file_unique_id"`
	Width        int           `json:"width,omitempty"`
	Height       int           `json:"height,omitempty"`
	Duration     int           `json:"duration,omitempty"`
	MimeType     string        `json:"mime_type,omitempty"`
	FileSize     int64         `json:"file_size,omitempty"`
	Thumbnail    *APIPhotoSize `json:"thumbnail,omitempty"`
}

// APIAudio is the audio attachment payload.
type APIAudio struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration,omitempty"`
	Performer    string `json:"performer,omitempty"`
	Title        string `json:"title,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
}

// APIVoice is the voice-note attachment payload.
type APIVoice struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
}

// APIFile is the response of getFile.
type APIFile struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileSize     int64  `json:"file_size,omitempty"`
	FilePath     string `json:"file_path,omitempty"`
}

// SendDocumentByFileIDRequest accepts a previously-issued file_id (or a
// publicly-reachable URL) so bots can re-share files without uploading the
// bytes again. It is also the JSON shape for sendPhoto/sendVideo/sendAudio/
// sendVoice JSON variants.
type SendDocumentByFileIDRequest struct {
	ChatID           string          `json:"chat_id"`
	Document         string          `json:"document,omitempty"`
	Photo            string          `json:"photo,omitempty"`
	Video            string          `json:"video,omitempty"`
	Audio            string          `json:"audio,omitempty"`
	Voice            string          `json:"voice,omitempty"`
	Caption          string          `json:"caption,omitempty"`
	ParseMode        string          `json:"parse_mode,omitempty"`
	CaptionEntities  []MessageEntity `json:"caption_entities,omitempty"`
	ReplyMarkup      json.RawMessage `json:"reply_markup,omitempty"`
	ReplyToMessageID *string         `json:"reply_to_message_id,omitempty"`
}

type CallbackQuery struct {
	ID      string      `json:"id"`
	FromID  string      `json:"from_id"`
	Message *APIMessage `json:"message,omitempty"`
	Data    string      `json:"data"`
}
