package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

const (
	classifyTimeout = 2 * time.Second
	defaultPriority = "normal"
)

type classifyRequest struct {
	SenderID    string `json:"sender_id"`
	SenderRole  string `json:"sender_role"`
	ChatType    string `json:"chat_type"`
	MessageText string `json:"message_text"`
	HasMention  bool   `json:"has_mention"`
	ReplyToMe   bool   `json:"reply_to_me"`
}

type classifyResponse struct {
	Priority  string `json:"priority"`
	Reasoning string `json:"reasoning"`
	Source    string `json:"source"`
	Cached   bool   `json:"cached"`
}

// NotificationClassifier calls the AI service to classify push notification priority.
type NotificationClassifier struct {
	aiServiceURL   string
	internalSecret string
	httpClient     *http.Client
	logger         *slog.Logger
}

// NewNotificationClassifier creates a classifier that calls the AI service.
// Returns nil when aiServiceURL is empty (classification disabled).
func NewNotificationClassifier(aiServiceURL, internalSecret string) *NotificationClassifier {
	if aiServiceURL == "" {
		return nil
	}
	return &NotificationClassifier{
		aiServiceURL:   aiServiceURL,
		internalSecret: internalSecret,
		httpClient:     &http.Client{Timeout: classifyTimeout},
		logger:         slog.Default(),
	}
}

// Classify calls POST /ai/classify-notification. Fail-open: returns "normal" on any error.
func (nc *NotificationClassifier) Classify(ctx context.Context, req classifyRequest) string {
	if nc == nil {
		return defaultPriority
	}

	body, err := json.Marshal(req)
	if err != nil {
		nc.logger.Warn("notification classify: marshal error", "error", err)
		return defaultPriority
	}

	reqCtx, cancel := context.WithTimeout(ctx, classifyTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		nc.aiServiceURL+"/ai/classify-notification", bytes.NewReader(body))
	if err != nil {
		return defaultPriority
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Internal-Token", nc.internalSecret)
	httpReq.Header.Set("X-User-ID", req.SenderID)

	resp, err := nc.httpClient.Do(httpReq)
	if err != nil {
		nc.logger.Warn("notification classify: request failed", "error", err)
		return defaultPriority
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		nc.logger.Warn("notification classify: non-200 status", "status", resp.StatusCode)
		return defaultPriority
	}

	var result classifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return defaultPriority
	}

	switch result.Priority {
	case "urgent", "important", "normal", "low":
		return result.Priority
	default:
		return defaultPriority
	}
}
