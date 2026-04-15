package model

import (
	"errors"
	"time"
)

// ErrAIUnavailable is returned by provider clients (Anthropic, OpenAI) when
// the corresponding API key is not configured. Handlers translate this into a
// 503 "ai_unavailable" response so the frontend can show a deterministic
// "AI is not configured" banner. This is the whole mechanism that lets us
// deploy the service with placeholder env vars and swap in real keys later
// on Saturn.ac without rebuilding the image.
var ErrAIUnavailable = errors.New("ai provider not configured")

// ErrRateLimited is returned by the service layer when the per-user per-
// endpoint rate limiter has rejected the call.
var ErrRateLimited = errors.New("ai rate limit exceeded")

// Message is the minimal chat-message shape the AI service cares about —
// enough to render a transcript for summarise/translate/suggest-reply. The
// upstream messaging service returns richer structures but we deliberately
// strip everything we do not need so AI prompts stay lean.
type Message struct {
	ID         string    `json:"id"`
	SenderID   string    `json:"sender_id"`
	SenderName string    `json:"sender_name,omitempty"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

// SummarizeRequest / TranslateRequest / ReplySuggestRequest / TranscribeRequest
// are the JSON payloads the handlers accept. Kept here so both the handler
// layer and the service layer refer to the same types.

type SummarizeRequest struct {
	ChatID    string `json:"chat_id"`
	TimeRange string `json:"time_range"` // "1h" | "6h" | "24h" | "7d"
	Language  string `json:"language"`   // BCP-47 tag, e.g. "ru", "en"
}

type TranslateRequest struct {
	MessageIDs     []string `json:"message_ids"`
	ChatID         string   `json:"chat_id,omitempty"`
	TargetLanguage string   `json:"target_language"`
}

type ReplySuggestRequest struct {
	ChatID string `json:"chat_id"`
}

type ReplySuggestResponse struct {
	Suggestions []string `json:"suggestions"`
}

type TranscribeRequest struct {
	MediaID string `json:"media_id"`
}

type TranscribeResponse struct {
	Text     string `json:"text"`
	Language string `json:"language,omitempty"`
}

type SearchRequest struct {
	Query  string `json:"query"`
	ChatID string `json:"chat_id,omitempty"`
}

// UsageStats is what GET /ai/usage returns for the current user. Totals are
// capped at the configured lookback window (default 30 days) to keep the
// query cheap.
type UsageStats struct {
	TotalRequests int              `json:"total_requests"`
	ByEndpoint    map[string]int   `json:"by_endpoint"`
	InputTokens   int              `json:"input_tokens"`
	OutputTokens  int              `json:"output_tokens"`
	PeriodStart   time.Time        `json:"period_start"`
	Cost          map[string]int   `json:"cost_cents,omitempty"`
	RecentSamples []UsageSample    `json:"recent_samples,omitempty"`
}

type UsageSample struct {
	Endpoint     string    `json:"endpoint"`
	Model        string    `json:"model"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	CostCents    int       `json:"cost_cents"`
	CreatedAt    time.Time `json:"created_at"`
}

// UsageRecord is the row shape used by the store.
type UsageRecord struct {
	UserID       string
	Endpoint     string
	Model        string
	InputTokens  int
	OutputTokens int
	CostCents    int
	CreatedAt    time.Time
}
