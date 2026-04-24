// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/search"
)

type mockSearchClient struct {
	searchCalls int
	lastIndex   string
	lastQuery   string
	lastFilter  string
}

func (m *mockSearchClient) Search(index string, query string, opts *search.SearchOptions) (*search.SearchResponse, error) {
	m.searchCalls++
	m.lastIndex = index
	m.lastQuery = query
	if opts != nil {
		m.lastFilter = opts.Filter
	}
	return &search.SearchResponse{
		Hits:               []map[string]interface{}{},
		EstimatedTotalHits: 0,
	}, nil
}

func (m *mockSearchClient) IndexDocuments(index string, docs []interface{}) error { return nil }
func (m *mockSearchClient) DeleteDocument(index string, id string) error          { return nil }

func TestSearchMessages_UsesGetUserChatIDsOnce(t *testing.T) {
	userID := uuid.New()
	chatA := uuid.New()
	chatB := uuid.New()
	client := &mockSearchClient{}
	getUserChatIDsCalls := 0

	chatStore := &mockChatStore{
		getUserChatIDsFn: func(_ context.Context, gotUserID uuid.UUID) ([]uuid.UUID, error) {
			getUserChatIDsCalls++
			if gotUserID != userID {
				t.Fatalf("unexpected userID: got %s want %s", gotUserID, userID)
			}
			return []uuid.UUID{chatA, chatB}, nil
		},
		listByUserFn: func(_ context.Context, _ uuid.UUID, _ string, _ int) ([]model.ChatListItem, string, bool, error) {
			t.Fatal("ListByUser should not be called for search ACL")
			return nil, "", false, nil
		},
	}

	svc := NewSearchService(client, chatStore)
	hits, total, err := svc.SearchMessages(context.Background(), userID, "orbit", nil, nil, nil, nil, nil, nil, 20, 0)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(hits) != 0 || total != 0 {
		t.Fatalf("unexpected search response: hits=%d total=%d", len(hits), total)
	}
	if getUserChatIDsCalls != 1 {
		t.Fatalf("expected GetUserChatIDs to be called once, got %d", getUserChatIDsCalls)
	}
	if client.searchCalls != 1 {
		t.Fatalf("expected one search call, got %d", client.searchCalls)
	}
	if client.lastIndex != "messages" || client.lastQuery != "orbit" {
		t.Fatalf("unexpected search call: index=%s query=%s", client.lastIndex, client.lastQuery)
	}
	if !strings.Contains(client.lastFilter, chatA.String()) || !strings.Contains(client.lastFilter, chatB.String()) {
		t.Fatalf("expected chat IDs in filter, got %q", client.lastFilter)
	}
}
