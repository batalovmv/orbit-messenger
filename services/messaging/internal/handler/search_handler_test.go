package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/services/messaging/internal/model"
	searchpkg "github.com/mst-corp/orbit/services/messaging/internal/search"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

type mockSearchClient struct {
	lastIndex string
	lastQuery string
	lastOpts  *searchpkg.SearchOptions
	response  *searchpkg.SearchResponse
}

func (m *mockSearchClient) Search(index string, query string, opts *searchpkg.SearchOptions) (*searchpkg.SearchResponse, error) {
	m.lastIndex = index
	m.lastQuery = query
	if opts != nil {
		copied := *opts
		m.lastOpts = &copied
	}

	if m.response != nil {
		return m.response, nil
	}

	return &searchpkg.SearchResponse{
		Hits:               []map[string]interface{}{},
		EstimatedTotalHits: 0,
	}, nil
}

func (m *mockSearchClient) IndexDocuments(_ string, _ []interface{}) error {
	return nil
}

func (m *mockSearchClient) DeleteDocument(_ string, _ string) error {
	return nil
}

func newSearchApp(client service.SearchClient, chats *mockChatStore) *fiber.App {
	app := fiber.New()
	searchSvc := service.NewSearchService(client, chats)
	handler := NewSearchHandler(searchSvc, slog.Default())
	handler.Register(app)
	return app
}

func TestSearch_AcceptsAliasFiltersAndPassesThemToMeilisearch(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	fromUserID := uuid.New()

	client := &mockSearchClient{}
	chats := &mockChatStore{
		listByUserFn: func(_ context.Context, gotUserID uuid.UUID, cursor string, limit int) ([]model.ChatListItem, string, bool, error) {
			if gotUserID != userID {
				t.Fatalf("unexpected user id: %s", gotUserID)
			}
			if cursor != "" || limit != 200 {
				t.Fatalf("unexpected paging arguments: cursor=%q limit=%d", cursor, limit)
			}

			return []model.ChatListItem{{
				Chat: model.Chat{ID: chatID},
			}}, "", false, nil
		},
	}

	app := newSearchApp(client, chats)
	req, _ := http.NewRequest(
		http.MethodGet,
		"/search?q=invoice&type=links&from="+fromUserID.String()+"&after=2024-01-01&before=2024-01-31",
		nil,
	)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	if client.lastIndex != "messages" {
		t.Fatalf("expected messages index, got %q", client.lastIndex)
	}
	if client.lastQuery != "invoice" {
		t.Fatalf("expected query invoice, got %q", client.lastQuery)
	}
	if client.lastOpts == nil {
		t.Fatal("expected search options to be passed")
	}

	expectedAfter := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
	expectedBefore := time.Date(2024, time.January, 31, 23, 59, 59, 999000000, time.UTC).Unix()
	filter := client.lastOpts.Filter

	for _, expectedPart := range []string{
		"chat_id IN ['" + chatID.String() + "']",
		"sender_id = '" + fromUserID.String() + "'",
		"has_links = true",
		"created_at_ts >= " + strconv.FormatInt(expectedAfter, 10),
		"created_at_ts <= " + strconv.FormatInt(expectedBefore, 10),
	} {
		if !strings.Contains(filter, expectedPart) {
			t.Fatalf("expected filter %q to contain %q", filter, expectedPart)
		}
	}
}

func TestSearch_AuthFail(t *testing.T) {
	app := newSearchApp(&mockSearchClient{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodGet, "/search?q=test", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 401, got %d: %s", resp.StatusCode, raw)
	}
}

func TestSearch_ValidationFail(t *testing.T) {
	app := newSearchApp(&mockSearchClient{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodGet, "/search?q=test&type=unknown", nil)
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}

	var body map[string]interface{}
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body["message"] == "" {
		t.Fatalf("expected validation message, got %#v", body)
	}
}
