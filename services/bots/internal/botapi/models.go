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
	MessageID      string      `json:"message_id"`
	ChatID         string      `json:"chat_id"`
	FromID         string      `json:"from_id"`
	FromName       string      `json:"from_name"`
	Text           string      `json:"text"`
	Date           int64       `json:"date"`
	ReplyToMessage *APIMessage `json:"reply_to_message,omitempty"`
}

type CallbackQuery struct {
	ID      string      `json:"id"`
	FromID  string      `json:"from_id"`
	Message *APIMessage `json:"message,omitempty"`
	Data    string      `json:"data"`
}
