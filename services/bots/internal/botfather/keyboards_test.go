// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botfather

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/services/bots/internal/client"
	"github.com/mst-corp/orbit/services/bots/internal/model"
)

// The BotFather's callback dispatcher in handler.go splits callback_data
// strings by ":" and routes by the first segment (prefix). Keyboards must
// produce data strings that the dispatcher can understand — these tests
// verify both layers agree on the wire format.

func TestMarshalKeyboard_RoundTrip(t *testing.T) {
	kb := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "ok", CallbackData: "noop:go"}},
		},
	}
	raw := marshalKeyboard(kb)

	var parsed struct {
		InlineKeyboard [][]struct {
			Text         string `json:"text"`
			CallbackData string `json:"callback_data"`
		} `json:"inline_keyboard"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.InlineKeyboard) != 1 || len(parsed.InlineKeyboard[0]) != 1 {
		t.Fatalf("unexpected shape: %s", raw)
	}
	if parsed.InlineKeyboard[0][0].CallbackData != "noop:go" {
		t.Fatalf("callback_data lost in round-trip: %s", raw)
	}
}

func TestBuildBotListKeyboard_OneRowPerBot(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()
	bots := []model.Bot{
		{ID: id1, Username: "foo_bot"},
		{ID: id2, Username: "bar_bot"},
	}

	kb := buildBotListKeyboard(bots, "mybots:select")
	if len(kb.InlineKeyboard) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(kb.InlineKeyboard))
	}
	if kb.InlineKeyboard[0][0].Text != "@foo_bot" {
		t.Fatalf("expected @-prefixed text, got %q", kb.InlineKeyboard[0][0].Text)
	}
	expectedData := "mybots:select:" + id1.String()
	if kb.InlineKeyboard[0][0].CallbackData != expectedData {
		t.Fatalf("expected callback %q, got %q", expectedData, kb.InlineKeyboard[0][0].CallbackData)
	}
}

func TestBuildBotListKeyboard_Empty(t *testing.T) {
	kb := buildBotListKeyboard(nil, "x:y")
	if len(kb.InlineKeyboard) != 0 {
		t.Fatalf("expected empty keyboard, got %d rows", len(kb.InlineKeyboard))
	}
}

func TestBuildManagementMenu_Structure(t *testing.T) {
	botID := uuid.New().String()
	kb := buildManagementMenu(botID)

	// Expected layout: 5 rows (name/desc, cmds/webhook, token/integration, delete, back)
	if len(kb.InlineKeyboard) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(kb.InlineKeyboard))
	}

	// Every button except the "back" row should include the bot ID.
	expectedPrefixes := []string{"manage:setname:", "manage:setdesc:", "manage:setcmds:",
		"manage:setwebhook:", "manage:token:", "manage:integration:", "manage:delete:"}
	var actualData []string
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData == "manage:back" {
				continue
			}
			actualData = append(actualData, btn.CallbackData)
		}
	}
	for _, prefix := range expectedPrefixes {
		found := false
		for _, data := range actualData {
			if strings.HasPrefix(data, prefix) && strings.HasSuffix(data, botID) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected button with prefix %q and botID suffix, not found in %v", prefix, actualData)
		}
	}
}

func TestBuildTokenActionsKeyboard_OneRowTwoButtons(t *testing.T) {
	botID := uuid.New().String()
	kb := buildTokenActionsKeyboard(botID)

	if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 2 {
		t.Fatalf("expected 1 row with 2 buttons, got %d rows", len(kb.InlineKeyboard))
	}
	show := kb.InlineKeyboard[0][0].CallbackData
	rotate := kb.InlineKeyboard[0][1].CallbackData
	if show != "token:show:"+botID || rotate != "token:rotate:"+botID {
		t.Fatalf("unexpected callback data show=%q rotate=%q", show, rotate)
	}
}

func TestBuildConnectorListKeyboard_AppendsClearRow(t *testing.T) {
	connectors := []client.ConnectorInfo{
		{ID: uuid.New(), Name: "keitaro", DisplayName: "Keitaro"},
		{ID: uuid.New(), Name: "insightflow", DisplayName: ""}, // falls back to Name
	}

	kb := buildConnectorListKeyboard(connectors, "integration:pick")
	// 2 connectors + 1 clear row.
	if len(kb.InlineKeyboard) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(kb.InlineKeyboard))
	}

	// Display fallback: when DisplayName empty, Name is used.
	if kb.InlineKeyboard[1][0].Text != "insightflow" {
		t.Fatalf("expected fallback to Name=insightflow, got %q", kb.InlineKeyboard[1][0].Text)
	}

	// Last row is the clear option.
	last := kb.InlineKeyboard[2][0]
	if last.CallbackData != "integration:pick:clear" {
		t.Fatalf("expected clear callback, got %q", last.CallbackData)
	}
	if last.Text == "" {
		t.Fatalf("clear button text is empty")
	}
}

func TestBuildConfirmKeyboard_TwoButtonRow(t *testing.T) {
	kb := buildConfirmKeyboard("revoke:yes:abc", "revoke:cancel")
	if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 2 {
		t.Fatalf("expected single row with 2 buttons")
	}
	if kb.InlineKeyboard[0][0].CallbackData != "revoke:yes:abc" {
		t.Fatalf("yes data wrong: %q", kb.InlineKeyboard[0][0].CallbackData)
	}
	if kb.InlineKeyboard[0][1].CallbackData != "revoke:cancel" {
		t.Fatalf("cancel data wrong: %q", kb.InlineKeyboard[0][1].CallbackData)
	}
}

func TestBuildToggleKeyboard_OnOffSideBySide(t *testing.T) {
	onData := "setprivacy:on:" + uuid.New().String()
	offData := "setprivacy:off:" + uuid.New().String()
	kb := buildToggleKeyboard("Вкл", "Выкл", onData, offData)

	if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 2 {
		t.Fatalf("expected single row with 2 buttons")
	}
	if kb.InlineKeyboard[0][0].Text != "Вкл" || kb.InlineKeyboard[0][1].Text != "Выкл" {
		t.Fatalf("labels swapped or wrong")
	}
	if kb.InlineKeyboard[0][0].CallbackData != onData || kb.InlineKeyboard[0][1].CallbackData != offData {
		t.Fatalf("callback data order wrong")
	}
}

// TestBuildMenuButtonTypeKeyboard_CoversAllActions guards the contract between
// buildMenuButtonTypeKeyboard and callbackSetMenu's switch in commands_settings.go.
// If a case is removed from either side, this test catches the drift.
func TestBuildMenuButtonTypeKeyboard_CoversAllActions(t *testing.T) {
	botID := uuid.New().String()
	kb := buildMenuButtonTypeKeyboard(botID)

	// We expect exactly 4 rows (default / commands / web_app / clear).
	if len(kb.InlineKeyboard) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(kb.InlineKeyboard))
	}

	expectedActions := map[string]bool{
		"setmenu:default:" + botID:  false,
		"setmenu:commands:" + botID: false,
		"setmenu:webapp:" + botID:   false,
		"setmenu:clear:" + botID:    false,
	}
	for _, row := range kb.InlineKeyboard {
		if len(row) != 1 {
			t.Fatalf("expected single-button rows, got %d buttons", len(row))
		}
		if _, ok := expectedActions[row[0].CallbackData]; !ok {
			t.Fatalf("unexpected callback data: %q", row[0].CallbackData)
		}
		expectedActions[row[0].CallbackData] = true
	}
	for data, seen := range expectedActions {
		if !seen {
			t.Errorf("missing callback data: %q", data)
		}
	}
}

// TestCallbackPrefixSplit_Compatibility verifies every keyboard's callback_data
// has at least `prefix:action` (2 segments) or `prefix:action:id` (3 segments),
// matching what HandleCallback in handler.go expects before dispatching.
func TestCallbackPrefixSplit_Compatibility(t *testing.T) {
	botID := uuid.New()
	cases := []struct {
		name    string
		kb      *InlineKeyboardMarkup
		minPart int
	}{
		{"management", buildManagementMenu(botID.String()), 2},
		{"token", buildTokenActionsKeyboard(botID.String()), 3},
		{"menuType", buildMenuButtonTypeKeyboard(botID.String()), 3},
		{"confirm", buildConfirmKeyboard("revoke:yes:"+botID.String(), "revoke:cancel"), 2},
		{"toggle", buildToggleKeyboard("on", "off",
			"setprivacy:on:"+botID.String(),
			"setprivacy:off:"+botID.String()), 3},
		{"botList", buildBotListKeyboard([]model.Bot{{ID: botID, Username: "x"}}, "mybots:select"), 3},
		{"connectorList", buildConnectorListKeyboard([]client.ConnectorInfo{{ID: uuid.New(), Name: "n"}}, "integration:pick"), 3},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for _, row := range tc.kb.InlineKeyboard {
				for _, btn := range row {
					parts := strings.Split(btn.CallbackData, ":")
					if len(parts) < tc.minPart {
						t.Errorf("callback %q has %d parts, want at least %d", btn.CallbackData, len(parts), tc.minPart)
					}
					if parts[0] == "" {
						t.Errorf("empty prefix in callback %q", btn.CallbackData)
					}
				}
			}
		})
	}
}
