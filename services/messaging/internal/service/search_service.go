package service

import (
	"context"
	"fmt"
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
}

// NewSearchService creates a new SearchService.
func NewSearchService(client SearchClient, chatStore store.ChatStore) *SearchService {
	return &SearchService{
		client:    client,
		chatStore: chatStore,
	}
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
		filters = append(filters, fmt.Sprintf("chat_id = %q", chatID.String()))
	}
	if fromUserID != nil {
		filters = append(filters, fmt.Sprintf("sender_id = %q", fromUserID.String()))
	}
	if dateFrom != nil {
		filters = append(filters, fmt.Sprintf("created_at_ts >= %d", dateFrom.Unix()))
	}
	if dateTo != nil {
		filters = append(filters, fmt.Sprintf("created_at_ts <= %d", dateTo.Unix()))
	}
	if msgType != nil {
		filters = append(filters, fmt.Sprintf("type = %q", *msgType))
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
	doc := map[string]interface{}{
		"id":            msg.ID.String(),
		"chat_id":       msg.ChatID.String(),
		"sender_id":     senderID,
		"content":       content,
		"type":          msg.Type,
		"has_media":     len(msg.MediaAttachments) > 0,
		"created_at_ts": msg.CreatedAt.Unix(),
	}
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
// It pages through ListByUser until all chats are collected.
func (s *SearchService) userChatIDs(ctx context.Context, userID uuid.UUID) ([]string, error) {
	const pageSize = 200
	var ids []string
	cursor := ""

	for {
		chats, next, hasMore, err := s.chatStore.ListByUser(ctx, userID, cursor, pageSize)
		if err != nil {
			return nil, err
		}
		for _, c := range chats {
			ids = append(ids, fmt.Sprintf("%q", c.ID.String()))
		}
		if !hasMore {
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
