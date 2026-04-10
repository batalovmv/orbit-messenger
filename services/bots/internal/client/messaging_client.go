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
) (*MessageResponse, error) {
	payload := map[string]any{
		"content": content,
		"type":    msgType,
	}
	if len(replyMarkup) > 0 {
		payload["reply_markup"] = json.RawMessage(replyMarkup)
	}
	if replyToID != nil {
		payload["reply_to_id"] = replyToID.String()
	}

	var result MessageResponse
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("%s/api/v1/chats/%s/messages", c.baseURL, chatID), botUserID, payload, &result); err != nil {
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
		payload["reply_markup"] = json.RawMessage(replyMarkup)
	}

	var result MessageResponse
	if err := c.doJSON(ctx, http.MethodPatch, fmt.Sprintf("%s/api/v1/messages/%s", c.baseURL, messageID), botUserID, payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *MessagingClient) DeleteMessage(ctx context.Context, botUserID, messageID uuid.UUID) error {
	return c.doJSON(ctx, http.MethodDelete, fmt.Sprintf("%s/api/v1/messages/%s", c.baseURL, messageID), botUserID, nil, nil)
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
