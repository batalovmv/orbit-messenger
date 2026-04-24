// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateReplyMarkup_AcceptsEmpty(t *testing.T) {
	if err := ValidateReplyMarkup(nil); err != nil {
		t.Fatalf("expected nil for empty payload, got %v", err)
	}
	if err := ValidateReplyMarkup(json.RawMessage("")); err != nil {
		t.Fatalf("expected nil for zero-length payload, got %v", err)
	}
}

func TestValidateReplyMarkup_AcceptsValidCallback(t *testing.T) {
	payload := []byte(`{"inline_keyboard":[[{"text":"Approve","callback_data":"approve:1"}]]}`)
	if err := ValidateReplyMarkup(payload); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateReplyMarkup_AcceptsValidURL(t *testing.T) {
	payload := []byte(`{"inline_keyboard":[[{"text":"Open","url":"https://example.com/path"}]]}`)
	if err := ValidateReplyMarkup(payload); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateReplyMarkup_AcceptsStringWrappedPayload(t *testing.T) {
	inner := `{"inline_keyboard":[[{"text":"Ok","callback_data":"ok"}]]}`
	wrapped, _ := json.Marshal(inner)
	if err := ValidateReplyMarkup(wrapped); err != nil {
		t.Fatalf("expected nil for string-wrapped payload, got %v", err)
	}
}

func TestValidateReplyMarkup_RejectsMalformedJSON(t *testing.T) {
	payload := []byte(`{not json}`)
	if err := ValidateReplyMarkup(payload); err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestValidateReplyMarkup_RejectsUnknownFields(t *testing.T) {
	payload := []byte(`{"inline_keyboard":[[{"text":"x","callback_data":"y","switch_inline_query":"z"}]]}`)
	err := ValidateReplyMarkup(payload)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestValidateReplyMarkup_RejectsEmptyKeyboard(t *testing.T) {
	payload := []byte(`{"inline_keyboard":[]}`)
	if err := ValidateReplyMarkup(payload); err == nil {
		t.Fatal("expected error for empty keyboard")
	}
}

func TestValidateReplyMarkup_RejectsEmptyRow(t *testing.T) {
	payload := []byte(`{"inline_keyboard":[[]]}`)
	if err := ValidateReplyMarkup(payload); err == nil {
		t.Fatal("expected error for empty row")
	}
}

func TestValidateReplyMarkup_RejectsTooManyRows(t *testing.T) {
	var rows []string
	for i := 0; i < 9; i++ {
		rows = append(rows, `[{"text":"x","callback_data":"y"}]`)
	}
	payload := []byte(`{"inline_keyboard":[` + strings.Join(rows, ",") + `]}`)
	if err := ValidateReplyMarkup(payload); err == nil {
		t.Fatal("expected error for too many rows")
	}
}

func TestValidateReplyMarkup_RejectsTooManyButtonsPerRow(t *testing.T) {
	var btns []string
	for i := 0; i < 9; i++ {
		btns = append(btns, `{"text":"x","callback_data":"y"}`)
	}
	payload := []byte(`{"inline_keyboard":[[` + strings.Join(btns, ",") + `]]}`)
	if err := ValidateReplyMarkup(payload); err == nil {
		t.Fatal("expected error for too many buttons per row")
	}
}

func TestValidateReplyMarkup_RejectsTooManyTotal(t *testing.T) {
	// 8 rows × 8 buttons = 64 (OK), 8 rows × 8 buttons per row max. Force >100 via many rows
	// is capped by row limit first, so instead: create scenario that passes row/col but exceeds
	// total — currently impossible given constraints (8×8=64 < 100). Verify boundary: 8×8 passes.
	var rows []string
	for i := 0; i < 8; i++ {
		rows = append(rows,
			`[{"text":"a","callback_data":"1"},{"text":"b","callback_data":"2"},{"text":"c","callback_data":"3"},{"text":"d","callback_data":"4"},{"text":"e","callback_data":"5"},{"text":"f","callback_data":"6"},{"text":"g","callback_data":"7"},{"text":"h","callback_data":"8"}]`)
	}
	payload := []byte(`{"inline_keyboard":[` + strings.Join(rows, ",") + `]}`)
	if err := ValidateReplyMarkup(payload); err != nil {
		t.Fatalf("expected nil at 8x8 boundary, got %v", err)
	}
}

func TestValidateReplyMarkup_RejectsEmptyText(t *testing.T) {
	payload := []byte(`{"inline_keyboard":[[{"text":"   ","callback_data":"x"}]]}`)
	if err := ValidateReplyMarkup(payload); err == nil {
		t.Fatal("expected error for whitespace-only text")
	}
}

func TestValidateReplyMarkup_RejectsLongText(t *testing.T) {
	payload := []byte(`{"inline_keyboard":[[{"text":"` + strings.Repeat("a", 65) + `","callback_data":"x"}]]}`)
	if err := ValidateReplyMarkup(payload); err == nil {
		t.Fatal("expected error for text >64 runes")
	}
}

func TestValidateReplyMarkup_RejectsBothURLAndCallback(t *testing.T) {
	payload := []byte(`{"inline_keyboard":[[{"text":"x","url":"https://a.com","callback_data":"y"}]]}`)
	if err := ValidateReplyMarkup(payload); err == nil {
		t.Fatal("expected error for both url and callback_data")
	}
}

func TestValidateReplyMarkup_RejectsNoAction(t *testing.T) {
	payload := []byte(`{"inline_keyboard":[[{"text":"x"}]]}`)
	if err := ValidateReplyMarkup(payload); err == nil {
		t.Fatal("expected error for missing url and callback_data")
	}
}

func TestValidateReplyMarkup_RejectsLongCallbackData(t *testing.T) {
	payload := []byte(`{"inline_keyboard":[[{"text":"x","callback_data":"` + strings.Repeat("a", 65) + `"}]]}`)
	if err := ValidateReplyMarkup(payload); err == nil {
		t.Fatal("expected error for callback_data >64 bytes")
	}
}

func TestValidateReplyMarkup_RejectsInvalidURL(t *testing.T) {
	cases := map[string]string{
		"missing_scheme": "example.com",
		"ftp":            "ftp://example.com",
		"javascript":     "javascript:alert(1)",
		"empty_host":     "https://",
	}
	for name, u := range cases {
		t.Run(name, func(t *testing.T) {
			payload := []byte(`{"inline_keyboard":[[{"text":"x","url":"` + u + `"}]]}`)
			if err := ValidateReplyMarkup(payload); err == nil {
				t.Fatalf("expected error for url=%q", u)
			}
		})
	}
}

func TestValidateReplyMarkup_RejectsOversizedPayload(t *testing.T) {
	big := make([]byte, maxReplyMarkupBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	if err := ValidateReplyMarkup(big); err == nil {
		t.Fatal("expected error for oversized payload")
	}
}
