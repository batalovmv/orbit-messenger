// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetMyName_UpdatesDisplayName(t *testing.T) {
	setup := newFileTestSetup(t, 100, nil)
	setup.bot.DisplayName = "Old Name"

	body, _ := json.Marshal(map[string]any{"name": "New Bot Name"})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-file/setMyName", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := setup.app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if setup.bot.DisplayName != "New Bot Name" {
		t.Fatalf("display_name=%q, want 'New Bot Name'", setup.bot.DisplayName)
	}
}

func TestSetMyName_RejectsEmpty(t *testing.T) {
	setup := newFileTestSetup(t, 100, nil)
	body, _ := json.Marshal(map[string]any{"name": "   "})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-file/setMyName", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestSetMyName_RejectsTooLong(t *testing.T) {
	setup := newFileTestSetup(t, 100, nil)
	long := strings.Repeat("x", 65)
	body, _ := json.Marshal(map[string]any{"name": long})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-file/setMyName", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestGetMyName_ReturnsCurrentDisplayName(t *testing.T) {
	setup := newFileTestSetup(t, 100, nil)
	setup.bot.DisplayName = "My Bot"

	req := httptest.NewRequest(http.MethodGet, "/bot/bot-file/getMyName", nil)
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var decoded struct {
		OK     bool `json:"ok"`
		Result struct {
			Name string `json:"name"`
		} `json:"result"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	if !decoded.OK || decoded.Result.Name != "My Bot" {
		t.Fatalf("got %#v", decoded)
	}
}

func TestSetMyDescription_UpdatesAndClears(t *testing.T) {
	setup := newFileTestSetup(t, 100, nil)
	body, _ := json.Marshal(map[string]any{"description": "I help with onboarding."})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-file/setMyDescription", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if setup.bot.Description == nil || *setup.bot.Description != "I help with onboarding." {
		t.Fatalf("description=%v", setup.bot.Description)
	}

	// Clearing: empty string normalises to nil description.
	body2, _ := json.Marshal(map[string]any{"description": ""})
	req2 := httptest.NewRequest(http.MethodPost, "/bot/bot-file/setMyDescription", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := setup.app.Test(req2, -1)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("clear: status=%d", resp2.StatusCode)
	}
}

func TestSetMyDescription_RejectsTooLong(t *testing.T) {
	setup := newFileTestSetup(t, 100, nil)
	long := strings.Repeat("x", 513)
	body, _ := json.Marshal(map[string]any{"description": long})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-file/setMyDescription", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestSetMyShortDescription_UpdatesAndRejectsTooLong(t *testing.T) {
	setup := newFileTestSetup(t, 100, nil)
	body, _ := json.Marshal(map[string]any{"short_description": "Onboarding bot"})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-file/setMyShortDescription", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if setup.bot.ShortDescription == nil || *setup.bot.ShortDescription != "Onboarding bot" {
		t.Fatalf("short_description=%v", setup.bot.ShortDescription)
	}

	// Too long.
	long := strings.Repeat("x", 121)
	body2, _ := json.Marshal(map[string]any{"short_description": long})
	req2 := httptest.NewRequest(http.MethodPost, "/bot/bot-file/setMyShortDescription", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := setup.app.Test(req2, -1)
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp2.StatusCode)
	}
}

func TestGetMyShortDescription_ReturnsCurrent(t *testing.T) {
	setup := newFileTestSetup(t, 100, nil)
	short := "tldr"
	setup.bot.ShortDescription = &short

	req := httptest.NewRequest(http.MethodGet, "/bot/bot-file/getMyShortDescription", nil)
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var decoded struct {
		OK     bool `json:"ok"`
		Result struct {
			ShortDescription string `json:"short_description"`
		} `json:"result"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	if !decoded.OK || decoded.Result.ShortDescription != "tldr" {
		t.Fatalf("got %#v", decoded)
	}
}
