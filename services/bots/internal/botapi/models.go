package botapi

import (
	"encoding/json"
)

type SendMessageRequest struct {
	ChatID           string          `json:"chat_id"`
	Text             string          `json:"text"`
	ReplyMarkup      json.RawMessage `json:"reply_markup,omitempty"`
	ReplyToMessageID *string         `json:"reply_to_message_id,omitempty"`
}

type EditMessageRequest struct {
	ChatID      string          `json:"chat_id"`
	MessageID   string          `json:"message_id"`
	Text        string          `json:"text"`
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
