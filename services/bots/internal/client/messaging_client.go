// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type MessagingClient struct {
	baseURL       string
	internalToken string
	httpClient    *http.Client
}

type MessageResponse struct {
	ID             uuid.UUID `json:"id"`
	ChatID         uuid.UUID `json:"chat_id"`
	Content        string    `json:"content"`
	Type           string    `json:"type"`
	SequenceNumber int64     `json:"sequence_number"`
	CreatedAt      time.Time `json:"created_at"`
}

type ClientError struct {
	StatusCode int
	Message    string
}

func (e *ClientError) Error() string {
	return fmt.Sprintf("messaging client error (%d): %s", e.StatusCode, e.Message)
}

func NewMessagingClient(baseURL, internalToken string) *MessagingClient {
	return &MessagingClient{
		baseURL:       strings.TrimRight(baseURL, "/"),
		internalToken: internalToken,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *MessagingClient) SendMessage(
	ctx context.Context,
	botUserID, chatID uuid.UUID,
	content string,
	msgType string,
	replyMarkup json.RawMessage,
	replyToID *uuid.UUID,
	mediaIDs ...string,
) (*MessageResponse, error) {
	payload := map[string]any{
		"content":    content,
		"type":       msgType,
		"via_bot_id": botUserID.String(),
	}
	if len(mediaIDs) > 0 {
		payload["media_ids"] = mediaIDs
	}
	if len(replyMarkup) > 0 {
		// Bot API clients send reply_markup as a JSON-encoded string (Telegram convention).
		// If it's a JSON string (starts with '"'), unwrap it so messaging stores a JSON object.
		rm := json.RawMessage(replyMarkup)
		if len(rm) > 0 && rm[0] == '"' {
			var unwrapped string
			if err := json.Unmarshal(rm, &unwrapped); err == nil {
				rm = json.RawMessage(unwrapped)
			}
		}
		payload["reply_markup"] = rm
	}
	if replyToID != nil {
		payload["reply_to_id"] = replyToID.String()
	}

	var result MessageResponse
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("%s/chats/%s/messages", c.baseURL, chatID), botUserID, payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *MessagingClient) EditMessage(
	ctx context.Context,
	botUserID, messageID uuid.UUID,
	content string,
	replyMarkup json.RawMessage,
) (*MessageResponse, error) {
	payload := map[string]any{
		"content": content,
	}
	if len(replyMarkup) > 0 {
		rm := json.RawMessage(replyMarkup)
		if len(rm) > 0 && rm[0] == '"' {
			var unwrapped string
			if err := json.Unmarshal(rm, &unwrapped); err == nil {
				rm = json.RawMessage(unwrapped)
			}
		}
		payload["reply_markup"] = rm
	}

	var result MessageResponse
	if err := c.doJSON(ctx, http.MethodPatch, fmt.Sprintf("%s/messages/%s", c.baseURL, messageID), botUserID, payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetMessage fetches a single message by ID.
func (c *MessagingClient) GetMessage(ctx context.Context, botUserID, messageID uuid.UUID) (*MessageResponse, error) {
	var result MessageResponse
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("%s/messages/%s", c.baseURL, messageID), botUserID, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ForwardMessage forwards a message to another chat.
func (c *MessagingClient) ForwardMessage(ctx context.Context, botUserID, messageID, toChatID uuid.UUID) (*MessageResponse, error) {
	payload := map[string]any{
		"message_ids": []string{messageID.String()},
		"to_chat_id":  toChatID.String(),
	}
	var results []MessageResponse
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("%s/messages/forward", c.baseURL), botUserID, payload, &results); err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, &ClientError{StatusCode: http.StatusInternalServerError, Message: "forward returned empty result"}
	}
	return &results[0], nil
}

// editReplyMarkupPayload is used to PATCH only reply_markup without sending a content field.
type editReplyMarkupPayload struct {
	ReplyMarkup json.RawMessage `json:"reply_markup"`
}

// EditReplyMarkup updates only the reply_markup of a message (content stays unchanged).
func (c *MessagingClient) EditReplyMarkup(ctx context.Context, botUserID, messageID uuid.UUID, replyMarkup json.RawMessage) (*MessageResponse, error) {
	rm := replyMarkup
	if len(rm) > 0 && rm[0] == '"' {
		var unwrapped string
		if err := json.Unmarshal(rm, &unwrapped); err == nil {
			rm = json.RawMessage(unwrapped)
		}
	}
	payload := editReplyMarkupPayload{ReplyMarkup: rm}
	var result MessageResponse
	if err := c.doJSON(ctx, http.MethodPatch, fmt.Sprintf("%s/messages/%s", c.baseURL, messageID), botUserID, payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// EditCaption updates the caption (content) and optionally reply_markup of a media message.
func (c *MessagingClient) EditCaption(ctx context.Context, botUserID, messageID uuid.UUID, caption string, replyMarkup json.RawMessage) (*MessageResponse, error) {
	payload := map[string]any{
		"content": caption,
	}
	if len(replyMarkup) > 0 {
		rm := replyMarkup
		if rm[0] == '"' {
			var unwrapped string
			if err := json.Unmarshal(rm, &unwrapped); err == nil {
				rm = json.RawMessage(unwrapped)
			}
		}
		payload["reply_markup"] = rm
	}
	var result MessageResponse
	if err := c.doJSON(ctx, http.MethodPatch, fmt.Sprintf("%s/messages/%s", c.baseURL, messageID), botUserID, payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *MessagingClient) DeleteMessage(ctx context.Context, botUserID, messageID uuid.UUID) error {
	return c.doJSON(ctx, http.MethodDelete, fmt.Sprintf("%s/messages/%s", c.baseURL, messageID), botUserID, nil, nil)
}

func (c *MessagingClient) doJSON(ctx context.Context, method, url string, botUserID uuid.UUID, payload any, target any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal messaging request: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("create messaging request: %w", err)
	}

	req.Header.Set("X-Internal-Token", c.internalToken)
	req.Header.Set("X-User-ID", botUserID.String())
	req.Header.Set("X-User-Role", "member")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("messaging request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read messaging response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		var apiErr struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(respBody, &apiErr) != nil || strings.TrimSpace(apiErr.Message) == "" {
			apiErr.Message = strings.TrimSpace(string(respBody))
		}
		if apiErr.Message == "" {
			apiErr.Message = http.StatusText(resp.StatusCode)
		}
		return &ClientError{StatusCode: resp.StatusCode, Message: apiErr.Message}
	}

	if target == nil || len(respBody) == 0 {
		return nil
	}

	if err := json.Unmarshal(respBody, target); err != nil {
		return fmt.Errorf("decode messaging response: %w", err)
	}

	return nil
}
