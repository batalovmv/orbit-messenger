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

// SendMessageOptions carries optional fields for SendMessage to keep the
// signature manageable as the Bot API surface grows.
type SendMessageOptions struct {
	ReplyMarkup json.RawMessage
	ReplyToID   *uuid.UUID
	Entities    json.RawMessage
	MediaIDs    []string
}

func (c *MessagingClient) SendMessage(
	ctx context.Context,
	botUserID, chatID uuid.UUID,
	content string,
	msgType string,
	opts SendMessageOptions,
) (*MessageResponse, error) {
	payload := map[string]any{
		"content":    content,
		"type":       msgType,
		"via_bot_id": botUserID.String(),
	}
	if len(opts.MediaIDs) > 0 {
		payload["media_ids"] = opts.MediaIDs
	}
	if len(opts.ReplyMarkup) > 0 {
		// Bot API clients send reply_markup as a JSON-encoded string (Telegram convention).
		// If it's a JSON string (starts with '"'), unwrap it so messaging stores a JSON object.
		rm := opts.ReplyMarkup
		if rm[0] == '"' {
			var unwrapped string
			if err := json.Unmarshal(rm, &unwrapped); err == nil {
				rm = json.RawMessage(unwrapped)
			}
		}
		payload["reply_markup"] = rm
	}
	if len(opts.Entities) > 0 {
		payload["entities"] = opts.Entities
	}
	if opts.ReplyToID != nil {
		payload["reply_to_id"] = opts.ReplyToID.String()
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
	entities json.RawMessage,
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
	if len(entities) > 0 {
		payload["entities"] = entities
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
func (c *MessagingClient) EditCaption(ctx context.Context, botUserID, messageID uuid.UUID, caption string, replyMarkup json.RawMessage, entities json.RawMessage) (*MessageResponse, error) {
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
	if len(entities) > 0 {
		payload["entities"] = entities
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

// GetChat fetches chat metadata.
func (c *MessagingClient) GetChat(ctx context.Context, botUserID, chatID uuid.UUID) (map[string]any, error) {
	var result map[string]any
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("%s/chats/%s", c.baseURL, chatID), botUserID, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetChatMember fetches a single chat member.
func (c *MessagingClient) GetChatMember(ctx context.Context, botUserID, chatID, userID uuid.UUID) (map[string]any, error) {
	var result map[string]any
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("%s/chats/%s/members/%s", c.baseURL, chatID, userID), botUserID, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetChatAdministrators fetches the list of chat admins.
func (c *MessagingClient) GetChatAdministrators(ctx context.Context, botUserID, chatID uuid.UUID) ([]map[string]any, error) {
	var result []map[string]any
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("%s/chats/%s/admins", c.baseURL, chatID), botUserID, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// paginatedMembersResponse is the shape returned by GET /chats/:id/members.
type paginatedMembersResponse struct {
	Items []map[string]any `json:"items"`
}

// GetChatMemberCount fetches members and returns the count.
func (c *MessagingClient) GetChatMemberCount(ctx context.Context, botUserID, chatID uuid.UUID) (int, error) {
	var result paginatedMembersResponse
	url := fmt.Sprintf("%s/chats/%s/members?limit=1000", c.baseURL, chatID)
	if err := c.doJSON(ctx, http.MethodGet, url, botUserID, nil, &result); err != nil {
		return 0, err
	}
	return len(result.Items), nil
}

// PinMessage pins a message in a chat.
func (c *MessagingClient) PinMessage(ctx context.Context, botUserID, chatID, messageID uuid.UUID) error {
	return c.doJSON(ctx, http.MethodPost, fmt.Sprintf("%s/chats/%s/pin/%s", c.baseURL, chatID, messageID), botUserID, nil, nil)
}

// UnpinMessage unpins a message in a chat.
func (c *MessagingClient) UnpinMessage(ctx context.Context, botUserID, chatID, messageID uuid.UUID) error {
	return c.doJSON(ctx, http.MethodDelete, fmt.Sprintf("%s/chats/%s/pin/%s", c.baseURL, chatID, messageID), botUserID, nil, nil)
}

// BanMember removes a member from a chat.
func (c *MessagingClient) BanMember(ctx context.Context, botUserID, chatID, userID uuid.UUID) error {
	return c.doJSON(ctx, http.MethodDelete, fmt.Sprintf("%s/chats/%s/members/%s", c.baseURL, chatID, userID), botUserID, nil, nil)
}

// RestrictMember updates member permissions using a bitmask.
func (c *MessagingClient) RestrictMember(ctx context.Context, botUserID, chatID, userID uuid.UUID, permissions int64) error {
	payload := map[string]any{"permissions": permissions}
	return c.doJSON(ctx, http.MethodPut, fmt.Sprintf("%s/chats/%s/members/%s/permissions", c.baseURL, chatID, userID), botUserID, payload, nil)
}

// SendChatAction sends a typing/upload action (fire-and-forget).
func (c *MessagingClient) SendChatAction(ctx context.Context, botUserID, chatID uuid.UUID, action string) error {
	payload := map[string]any{"action": action}
	return c.doJSON(ctx, http.MethodPost, fmt.Sprintf("%s/chats/%s/typing", c.baseURL, chatID), botUserID, payload, nil)
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
