// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botfather

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func newTestRedisStateStore(t *testing.T) (*RedisStateStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewRedisStateStore(rdb), mr
}

func TestGetState_MissingReturnsStepNone(t *testing.T) {
	store, _ := newTestRedisStateStore(t)
	state, err := store.GetState(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("GetState err: %v", err)
	}
	if state == nil || state.Step != StepNone {
		t.Fatalf("expected StepNone, got %+v", state)
	}
}

func TestSetState_RoundTrip(t *testing.T) {
	store, _ := newTestRedisStateStore(t)
	userID := uuid.New()
	botID := uuid.New()
	input := &ConversationState{Step: StepSetPrivacyChoice, BotID: botID, Data: "payload"}

	if err := store.SetState(context.Background(), userID, input); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	out, err := store.GetState(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if !reflect.DeepEqual(out, input) {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", out, input)
	}
}

func TestSetState_AppliesTTL(t *testing.T) {
	store, mr := newTestRedisStateStore(t)
	userID := uuid.New()
	if err := store.SetState(context.Background(), userID, &ConversationState{Step: StepNewBotAskName}); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	ttl := mr.TTL(stateKey(userID))
	if ttl <= 0 || ttl > stateTTL {
		t.Fatalf("expected TTL in (0, %v], got %v", stateTTL, ttl)
	}

	// Fast-forward past TTL and ensure the state evaporates.
	mr.FastForward(stateTTL + time.Second)
	out, err := store.GetState(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetState after expiry: %v", err)
	}
	if out.Step != StepNone {
		t.Fatalf("expected state to expire, got %+v", out)
	}
}

func TestClearState_RemovesKey(t *testing.T) {
	store, mr := newTestRedisStateStore(t)
	userID := uuid.New()
	if err := store.SetState(context.Background(), userID, &ConversationState{Step: StepNewBotAskName}); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if !mr.Exists(stateKey(userID)) {
		t.Fatalf("precondition: key must exist after SetState")
	}
	if err := store.ClearState(context.Background(), userID); err != nil {
		t.Fatalf("ClearState: %v", err)
	}
	if mr.Exists(stateKey(userID)) {
		t.Fatalf("expected key to be deleted after ClearState")
	}
}

func TestGetState_CorruptJSONReturnsStepNone(t *testing.T) {
	store, mr := newTestRedisStateStore(t)
	userID := uuid.New()
	if err := mr.Set(stateKey(userID), "not-json"); err != nil {
		t.Fatalf("seed corrupt state: %v", err)
	}
	out, err := store.GetState(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if out.Step != StepNone {
		t.Fatalf("expected StepNone on corrupt state, got %+v", out)
	}
}

func TestIsCachedDM_DefaultsFalse(t *testing.T) {
	store, _ := newTestRedisStateStore(t)
	cached, err := store.IsCachedDM(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("IsCachedDM: %v", err)
	}
	if cached {
		t.Fatalf("expected false for missing key")
	}
}

func TestCacheDMChat_RoundTrip(t *testing.T) {
	store, mr := newTestRedisStateStore(t)
	chatID := uuid.New()
	if err := store.CacheDMChat(context.Background(), chatID); err != nil {
		t.Fatalf("CacheDMChat: %v", err)
	}
	cached, err := store.IsCachedDM(context.Background(), chatID)
	if err != nil {
		t.Fatalf("IsCachedDM: %v", err)
	}
	if !cached {
		t.Fatalf("expected cached=true after CacheDMChat")
	}
	// TTL is 24h — sanity-check it's non-zero.
	if ttl := mr.TTL(dmCacheKey(chatID)); ttl <= 0 {
		t.Fatalf("expected positive TTL, got %v", ttl)
	}
}

// TestStepConstants_Unique guards against accidental step-string collisions.
// Two different steps with the same value would silently merge and break
// stateful command flows.
func TestStepConstants_Unique(t *testing.T) {
	steps := []string{
		StepNewBotAskName, StepNewBotAskUsername,
		StepSetNameSelectBot, StepSetNameAwait,
		StepSetDescSelectBot, StepSetDescAwait,
		StepSetWebhookSelectBot, StepSetWebhookAwait, StepSetWebhookAwaitSecret,
		StepSetCmdsSelectBot, StepSetCmdsAwait,
		StepDeleteSelectBot, StepDeleteConfirm,
		StepTokenSelectBot, StepTokenActions, StepTokenConfirm,
		StepIntegrationSelectBot, StepIntegrationSelectConnector,
		StepSetAboutSelectBot, StepSetAboutAwait,
		StepSetPrivacySelectBot, StepSetPrivacyChoice,
		StepSetInlineSelectBot, StepSetInlineChoice, StepSetInlineAwaitPlaceholder,
		StepSetJoinGroupsSelectBot, StepSetJoinGroupsChoice,
		StepSetMenuSelectBot, StepSetMenuChoice, StepSetMenuAwaitText, StepSetMenuAwaitURL,
		StepRevokeSelectBot, StepRevokeConfirm,
		StepSetUserpicSelectBot, StepSetUserpicAwait,
	}
	seen := make(map[string]int, len(steps))
	for i, s := range steps {
		if s == "" {
			t.Errorf("step at index %d is empty string (collision with StepNone)", i)
			continue
		}
		if prev, ok := seen[s]; ok {
			t.Errorf("duplicate step %q at indexes %d and %d", s, prev, i)
		}
		seen[s] = i
	}
}
