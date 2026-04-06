package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/search"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// SearchClient is the interface satisfied by a Meilisearch client wrapper.
// Re-exported from the search package to avoid circular imports for callers.
type SearchClient = search.SearchClient

const (
	indexMessages = "messages"
	indexUsers    = "users"
	indexChats    = "chats"
)

// SearchService provides full-text search over messages, users, and chats
// via a Meilisearch backend with per-user ACL enforcement.
type SearchService struct {
	client    SearchClient
	chatStore store.ChatStore
	userStore store.UserStore
}

// NewSearchService creates a new SearchService.
func NewSearchService(client SearchClient, chatStore store.ChatStore, userStore ...store.UserStore) *SearchService {
	svc := &SearchService{
		client:    client,
		chatStore: chatStore,
	}
	if len(userStore) > 0 {
		svc.userStore = userStore[0]
	}
	return svc
}

// SearchMessages searches messages visible to userID, applying optional filters.
// Returns the matching hits, the estimated total count, and any error.
func (s *SearchService) SearchMessages(
	ctx context.Context,
	userID uuid.UUID,
	query string,
	chatID *uuid.UUID,
	fromUserID *uuid.UUID,
	dateFrom, dateTo *time.Time,
	msgType *string,
	hasMedia *bool,
	limit, offset int,
) ([]map[string]interface{}, int, error) {
	// Collect all chat IDs the user belongs to for ACL enforcement.
	chatIDs, err := s.userChatIDs(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch user chat ids: %w", err)
	}
	if len(chatIDs) == 0 {
		return []map[string]interface{}{}, 0, nil
	}

	// Build base ACL filter.
	filters := []string{fmt.Sprintf("chat_id IN [%s]", strings.Join(chatIDs, ", "))}

	if chatID != nil {
		// Extra scoping: only show messages from this specific chat (must still be in ACL list).
		filters = append(filters, fmt.Sprintf("chat_id = '%s'", chatID.String()))
	}
	if fromUserID != nil {
		filters = append(filters, fmt.Sprintf("sender_id = '%s'", fromUserID.String()))
	}
	if dateFrom != nil {
		filters = append(filters, fmt.Sprintf("created_at_ts >= %d", dateFrom.Unix()))
	}
	if dateTo != nil {
		filters = append(filters, fmt.Sprintf("created_at_ts <= %d", dateTo.Unix()))
	}
	if msgType != nil {
		if *msgType == "link" {
			filters = append(filters, "has_links = true")
		} else {
			filters = append(filters, fmt.Sprintf("type = '%s'", *msgType))
		}
	}
	if hasMedia != nil {
		if *hasMedia {
			filters = append(filters, "has_media = true")
		} else {
			filters = append(filters, "has_media = false")
		}
	}

	resp, err := s.client.Search(indexMessages, query, &search.SearchOptions{
		Filter: strings.Join(filters, " AND "),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("search messages: %w", err)
	}
	return resp.Hits, resp.EstimatedTotalHits, nil
}

// SearchUsers searches users by query string.
func (s *SearchService) SearchUsers(ctx context.Context, query string, limit int) ([]map[string]interface{}, int, error) {
	resp, err := s.client.Search(indexUsers, query, &search.SearchOptions{
		Limit: limit,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("search users: %w", err)
	}
	return resp.Hits, resp.EstimatedTotalHits, nil
}

// SearchChats searches chats that the given user is a member of.
func (s *SearchService) SearchChats(ctx context.Context, userID uuid.UUID, query string, limit int) ([]map[string]interface{}, int, error) {
	chatIDs, err := s.userChatIDs(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch user chat ids: %w", err)
	}
	if len(chatIDs) == 0 {
		return []map[string]interface{}{}, 0, nil
	}

	filter := fmt.Sprintf("id IN [%s]", strings.Join(chatIDs, ", "))
	resp, err := s.client.Search(indexChats, query, &search.SearchOptions{
		Filter: filter,
		Limit:  limit,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("search chats: %w", err)
	}
	return resp.Hits, resp.EstimatedTotalHits, nil
}

// IndexMessage adds or updates a message document in the search index.
func (s *SearchService) IndexMessage(msg *model.Message) error {
	senderID := ""
	if msg.SenderID != nil {
		senderID = msg.SenderID.String()
	}
	content := ""
	if msg.Content != nil {
		content = *msg.Content
	}
	doc := search.BuildMessageDocument(
		msg.ID.String(),
		msg.ChatID.String(),
		senderID,
		content,
		msg.Type,
		len(msg.MediaAttachments) > 0,
		search.HasLinks(content, msg.Entities),
		msg.CreatedAt,
		msg.SequenceNumber,
	)
	if err := s.client.IndexDocuments(indexMessages, []interface{}{doc}); err != nil {
		return fmt.Errorf("index message: %w", err)
	}
	return nil
}

// DeleteMessage removes a message document from the search index.
func (s *SearchService) DeleteMessage(messageID string) error {
	if err := s.client.DeleteDocument(indexMessages, messageID); err != nil {
		return fmt.Errorf("delete message from index: %w", err)
	}
	return nil
}

// userChatIDs fetches all chat IDs the user belongs to (used for ACL filtering).
// It pages through ListByUser until all chats are collected, capped at maxChats
// to prevent unbounded loops and oversized Meilisearch filter expressions.
func (s *SearchService) userChatIDs(ctx context.Context, userID uuid.UUID) ([]string, error) {
	const pageSize = 200
	const maxChats = 2000
	var ids []string
	cursor := ""

	for len(ids) < maxChats {
		chats, next, hasMore, err := s.chatStore.ListByUser(ctx, userID, cursor, pageSize)
		if err != nil {
			return nil, err
		}
		for _, c := range chats {
			ids = append(ids, fmt.Sprintf("'%s'", c.ID.String()))
		}
		if !hasMore || len(ids) >= maxChats {
			break
		}
		cursor = next
	}
	return ids, nil
}

// validateSearchQuery returns an error when the query is empty.
func validateSearchQuery(query string) error {
	if strings.TrimSpace(query) == "" {
		return apperror.BadRequest("search query must not be empty")
	}
	return nil
}

// IndexUser adds or updates a user document in the search index.
func (s *SearchService) IndexUser(userID, displayName, email, role string) error {
	doc := search.BuildUserDocument(userID, displayName, email, role)
	if err := s.client.IndexDocuments(indexUsers, []interface{}{doc}); err != nil {
		return fmt.Errorf("index user: %w", err)
	}
	return nil
}

// DeleteUser removes a user document from the search index.
func (s *SearchService) DeleteUser(userID string) error {
	if err := s.client.DeleteDocument(indexUsers, userID); err != nil {
		return fmt.Errorf("delete user from index: %w", err)
	}
	return nil
}

// IndexChat adds or updates a chat document in the search index.
// Only group and channel chats are indexed; direct chats are skipped.
func (s *SearchService) IndexChat(chatID, chatType, name, description string) error {
	if chatType == "direct" {
		return nil
	}
	doc := search.BuildChatDocument(chatID, chatType, name, description)
	if err := s.client.IndexDocuments(indexChats, []interface{}{doc}); err != nil {
		return fmt.Errorf("index chat: %w", err)
	}
	return nil
}

// DeleteChat removes a chat document from the search index.
func (s *SearchService) DeleteChat(chatID string) error {
	if err := s.client.DeleteDocument(indexChats, chatID); err != nil {
		return fmt.Errorf("delete chat from index: %w", err)
	}
	return nil
}

// BootstrapIndices populates the Meilisearch users and chats indices from the
// database on service startup. Errors are non-fatal — logged but not returned.
func (s *SearchService) BootstrapIndices(ctx context.Context) {
	if s.userStore != nil {
		if err := s.bootstrapUsers(ctx); err != nil {
			// Non-fatal: search works without bootstrap, just may miss old data.
			slog.WarnContext(ctx, "search bootstrap: index users failed", "error", err)
		}
	}
	if err := s.bootstrapChats(ctx); err != nil {
		slog.WarnContext(ctx, "search bootstrap: index chats failed", "error", err)
	}
}

func (s *SearchService) bootstrapUsers(ctx context.Context) error {
	users, err := s.userStore.ListAll(ctx, 0)
	if err != nil {
		return fmt.Errorf("list all users: %w", err)
	}
	if len(users) == 0 {
		return nil
	}
	docs := make([]interface{}, 0, len(users))
	for _, u := range users {
		docs = append(docs, search.BuildUserDocument(u.ID.String(), u.DisplayName, u.Email, u.Role))
	}
	if err := s.client.IndexDocuments(indexUsers, docs); err != nil {
		return fmt.Errorf("index users: %w", err)
	}
	return nil
}

func (s *SearchService) bootstrapChats(ctx context.Context) error {
	chats, err := s.chatStore.ListAll(ctx, 0)
	if err != nil {
		return fmt.Errorf("list all chats: %w", err)
	}
	if len(chats) == 0 {
		return nil
	}
	docs := make([]interface{}, 0, len(chats))
	for _, c := range chats {
		if c.Type == "direct" {
			continue
		}
		name := ""
		if c.Name != nil {
			name = *c.Name
		}
		description := ""
		if c.Description != nil {
			description = *c.Description
		}
		docs = append(docs, search.BuildChatDocument(c.ID.String(), c.Type, name, description))
	}
	if len(docs) == 0 {
		return nil
	}
	if err := s.client.IndexDocuments(indexChats, docs); err != nil {
		return fmt.Errorf("index chats: %w", err)
	}
	return nil
}
