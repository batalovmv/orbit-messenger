// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mst-corp/orbit/services/ai/internal/model"
)

// MessagingClient is a thin HTTP wrapper around the messaging service's
// internal endpoints. The AI service needs to read chat messages to build
// prompts for summarise/translate/suggest-reply, and it must pass the
// caller's X-User-ID so the messaging service's RBAC checks still apply.
type MessagingClient struct {
	baseURL       string
	internalToken string
	http          *http.Client
}

func NewMessagingClient(baseURL, internalToken string) *MessagingClient {
	return &MessagingClient{
		baseURL:       strings.TrimRight(baseURL, "/"),
		internalToken: internalToken,
		http:          &http.Client{Timeout: 15 * time.Second},
	}
}

// messageDTO is the wire shape returned by the messaging service for a
// single message. We only decode fields we care about — the messaging
// response carries many more (reactions, reply_markup, media, etc).
type messageDTO struct {
	ID         string    `json:"id"`
	SenderID   string    `json:"sender_id"`
	SenderName string    `json:"sender_name,omitempty"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

type messagesResponse struct {
	Data []messageDTO `json:"data"`
}

// FetchRecentMessages pulls the last `limit` messages from the given chat.
// Returns them in chronological order (oldest first) which is what Claude
// prompts expect for a summariseable transcript.
func (c *MessagingClient) FetchRecentMessages(
	ctx context.Context,
	userID string,
	chatID string,
	limit int,
) ([]model.Message, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	reqURL := fmt.Sprintf("%s/chats/%s/messages?limit=%d", c.baseURL, url.PathEscape(chatID), limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build messages request: %w", err)
	}
	c.setInternalHeaders(req, userID)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch messages: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("messaging returned %d: %s", resp.StatusCode, string(body))
	}

	var decoded messagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode messages: %w", err)
	}

	// Messaging returns messages newest-first (cursor pagination). Reverse
	// so the transcript reads chronologically for Claude.
	result := make([]model.Message, 0, len(decoded.Data))
	for i := len(decoded.Data) - 1; i >= 0; i-- {
		dto := decoded.Data[i]
		if strings.TrimSpace(dto.Content) == "" {
			continue
		}
		result = append(result, model.Message{
			ID:         dto.ID,
			SenderID:   dto.SenderID,
			SenderName: dto.SenderName,
			Content:    dto.Content,
			CreatedAt:  dto.CreatedAt,
		})
	}
	return result, nil
}

// FetchMessagesByIDs loads specific messages by ID — used by the translate
// endpoint where the user selects exact messages to translate.
func (c *MessagingClient) FetchMessagesByIDs(
	ctx context.Context,
	userID string,
	messageIDs []string,
) ([]model.Message, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	if len(messageIDs) > 100 {
		return nil, fmt.Errorf("too many message ids (max 100)")
	}

	payload, err := json.Marshal(map[string]any{"message_ids": messageIDs})
	if err != nil {
		return nil, fmt.Errorf("marshal ids: %w", err)
	}

	reqURL := fmt.Sprintf("%s/messages/batch", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("build batch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setInternalHeaders(req, userID)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("batch fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("messaging batch returned %d: %s", resp.StatusCode, string(body))
	}

	var decoded messagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode batch: %w", err)
	}

	result := make([]model.Message, 0, len(decoded.Data))
	for _, dto := range decoded.Data {
		if strings.TrimSpace(dto.Content) == "" {
			continue
		}
		result = append(result, model.Message{
			ID:         dto.ID,
			SenderID:   dto.SenderID,
			SenderName: dto.SenderName,
			Content:    dto.Content,
			CreatedAt:  dto.CreatedAt,
		})
	}
	return result, nil
}

func (c *MessagingClient) setInternalHeaders(req *http.Request, userID string) {
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	if c.internalToken != "" {
		req.Header.Set("X-Internal-Token", c.internalToken)
	}
	req.Header.Set("User-Agent", "OrbitMessenger-AI/1.0")
}
