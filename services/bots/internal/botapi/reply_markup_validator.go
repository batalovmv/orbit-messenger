// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"encoding/json"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/mst-corp/orbit/pkg/apperror"
)

const (
	maxReplyMarkupBytes    = 10 * 1024
	maxInlineKeyboardRows  = 8
	maxButtonsPerRow       = 8
	maxInlineKeyboardTotal = 100
	maxButtonTextRunes     = 64
	maxCallbackDataBytes   = 64
	maxButtonURLBytes      = 256

	// Reply (custom) keyboard limits. TG allows generous bounds, but we cap
	// here so a single message can't dominate the composer.
	maxReplyKeyboardRows      = 12
	maxButtonsPerReplyRow     = 12
	maxReplyKeyboardTotal     = 100
	maxInputFieldPlaceholderLen = 64
)

// ValidateReplyMarkup parses and validates a Bot API reply_markup payload.
// An empty payload is valid (no keyboard). The payload may be either a JSON
// object or a JSON-encoded string wrapping one (Telegram convention).
//
// Accepted forms (mutually exclusive):
//   - {"inline_keyboard": [[...]]}
//   - {"keyboard": [[...]], "resize_keyboard"?, "one_time_keyboard"?, ...}
//   - {"remove_keyboard": true, "selective"?}
//   - {"force_reply": true, "input_field_placeholder"?, "selective"?}
func ValidateReplyMarkup(raw json.RawMessage) *apperror.AppError {
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > maxReplyMarkupBytes {
		return apperror.BadRequest("reply_markup is too large")
	}

	payload := raw
	if payload[0] == '"' {
		var unwrapped string
		if err := json.Unmarshal(payload, &unwrapped); err != nil {
			return apperror.BadRequest("reply_markup is not valid JSON")
		}
		trimmed := strings.TrimSpace(unwrapped)
		if trimmed == "" {
			return nil
		}
		payload = json.RawMessage(trimmed)
	}

	var probe map[string]json.RawMessage
	if err := json.Unmarshal(payload, &probe); err != nil {
		return apperror.BadRequest("reply_markup must be a JSON object")
	}

	hasInline := hasJSONField(probe, "inline_keyboard")
	hasKeyboard := hasJSONField(probe, "keyboard")
	hasRemove := hasJSONField(probe, "remove_keyboard")
	hasForceReply := hasJSONField(probe, "force_reply")

	formsCount := 0
	for _, present := range []bool{hasInline, hasKeyboard, hasRemove, hasForceReply} {
		if present {
			formsCount++
		}
	}

	if formsCount == 0 {
		return apperror.BadRequest(
			"reply_markup must contain inline_keyboard, keyboard, remove_keyboard, or force_reply",
		)
	}
	if formsCount > 1 {
		return apperror.BadRequest(
			"reply_markup must contain only one of inline_keyboard, keyboard, remove_keyboard, or force_reply",
		)
	}

	switch {
	case hasInline:
		return validateInlineKeyboardPayload(payload)
	case hasKeyboard:
		return validateReplyKeyboardPayload(payload)
	case hasRemove:
		return validateRemoveKeyboardPayload(payload)
	case hasForceReply:
		return validateForceReplyPayload(payload)
	}
	return nil
}

func hasJSONField(probe map[string]json.RawMessage, key string) bool {
	v, ok := probe[key]
	return ok && len(v) > 0
}

func validateInlineKeyboardPayload(payload json.RawMessage) *apperror.AppError {
	var markup InlineKeyboardMarkup
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&markup); err != nil {
		return apperror.BadRequest("reply_markup must be an inline keyboard object")
	}

	if len(markup.InlineKeyboard) == 0 {
		return apperror.BadRequest("reply_markup.inline_keyboard cannot be empty")
	}
	if len(markup.InlineKeyboard) > maxInlineKeyboardRows {
		return apperror.BadRequest("reply_markup.inline_keyboard has too many rows")
	}

	total := 0
	for i, row := range markup.InlineKeyboard {
		if len(row) == 0 {
			return apperror.BadRequest(rowPrefix(i) + "cannot be empty")
		}
		if len(row) > maxButtonsPerRow {
			return apperror.BadRequest(rowPrefix(i) + "has too many buttons")
		}
		total += len(row)
		if total > maxInlineKeyboardTotal {
			return apperror.BadRequest("reply_markup has too many buttons in total")
		}
		for j, btn := range row {
			if err := validateInlineButton(btn, i, j); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateReplyKeyboardPayload(payload json.RawMessage) *apperror.AppError {
	var markup ReplyKeyboardMarkup
	if err := json.Unmarshal(payload, &markup); err != nil {
		return apperror.BadRequest("reply_markup keyboard payload is invalid")
	}
	if len(markup.Keyboard) == 0 {
		return apperror.BadRequest("reply_markup.keyboard cannot be empty")
	}
	if len(markup.Keyboard) > maxReplyKeyboardRows {
		return apperror.BadRequest("reply_markup.keyboard has too many rows")
	}

	total := 0
	for i, row := range markup.Keyboard {
		if len(row) == 0 {
			return apperror.BadRequest("reply_markup.keyboard[" + itoa(i) + "] cannot be empty")
		}
		if len(row) > maxButtonsPerReplyRow {
			return apperror.BadRequest("reply_markup.keyboard[" + itoa(i) + "] has too many buttons")
		}
		total += len(row)
		if total > maxReplyKeyboardTotal {
			return apperror.BadRequest("reply_markup.keyboard has too many buttons in total")
		}
		for j, btn := range row {
			text := strings.TrimSpace(btn.Text)
			if text == "" {
				return apperror.BadRequest(replyButtonPrefix(i, j) + "text is required")
			}
			if utf8.RuneCountInString(text) > maxButtonTextRunes {
				return apperror.BadRequest(replyButtonPrefix(i, j) + "text is too long")
			}
		}
	}
	if utf8.RuneCountInString(markup.InputFieldPlaceholder) > maxInputFieldPlaceholderLen {
		return apperror.BadRequest("reply_markup.input_field_placeholder is too long")
	}
	return nil
}

func validateRemoveKeyboardPayload(payload json.RawMessage) *apperror.AppError {
	var rm ReplyKeyboardRemove
	if err := json.Unmarshal(payload, &rm); err != nil {
		return apperror.BadRequest("reply_markup remove_keyboard payload is invalid")
	}
	if !rm.RemoveKeyboard {
		return apperror.BadRequest("reply_markup.remove_keyboard must be true")
	}
	return nil
}

func validateForceReplyPayload(payload json.RawMessage) *apperror.AppError {
	var fr ForceReply
	if err := json.Unmarshal(payload, &fr); err != nil {
		return apperror.BadRequest("reply_markup force_reply payload is invalid")
	}
	if !fr.ForceReply {
		return apperror.BadRequest("reply_markup.force_reply must be true")
	}
	if utf8.RuneCountInString(fr.InputFieldPlaceholder) > maxInputFieldPlaceholderLen {
		return apperror.BadRequest("reply_markup.input_field_placeholder is too long")
	}
	return nil
}

func validateInlineButton(btn InlineKeyboardButton, row, col int) *apperror.AppError {
	prefix := buttonPrefix(row, col)

	text := strings.TrimSpace(btn.Text)
	if text == "" {
		return apperror.BadRequest(prefix + "text is required")
	}
	if utf8.RuneCountInString(text) > maxButtonTextRunes {
		return apperror.BadRequest(prefix + "text is too long")
	}

	hasURL := strings.TrimSpace(btn.URL) != ""
	hasCallback := strings.TrimSpace(btn.CallbackData) != ""

	if hasURL && hasCallback {
		return apperror.BadRequest(prefix + "must have only one of url or callback_data")
	}
	if !hasURL && !hasCallback {
		return apperror.BadRequest(prefix + "must have url or callback_data")
	}

	if hasCallback {
		if len(btn.CallbackData) > maxCallbackDataBytes {
			return apperror.BadRequest(prefix + "callback_data is too long")
		}
	}

	if hasURL {
		if len(btn.URL) > maxButtonURLBytes {
			return apperror.BadRequest(prefix + "url is too long")
		}
		u, err := url.Parse(btn.URL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return apperror.BadRequest(prefix + "url is invalid")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return apperror.BadRequest(prefix + "url must use http or https")
		}
	}

	return nil
}

func rowPrefix(row int) string {
	return "reply_markup.inline_keyboard[" + itoa(row) + "] "
}

func buttonPrefix(row, col int) string {
	return "reply_markup.inline_keyboard[" + itoa(row) + "][" + itoa(col) + "] "
}

func replyButtonPrefix(row, col int) string {
	return "reply_markup.keyboard[" + itoa(row) + "][" + itoa(col) + "] "
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
