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
)

// ValidateReplyMarkup parses and validates an inline-keyboard reply_markup payload
// coming from a Bot API client. An empty payload is valid (no keyboard attached).
// The payload may be either a raw JSON object or a JSON-encoded string wrapping one
// (Telegram convention) — both forms are unwrapped before validation.
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
			if err := validateButton(btn, i, j); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateButton(btn InlineKeyboardButton, row, col int) *apperror.AppError {
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
