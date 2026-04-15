package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mst-corp/orbit/services/ai/internal/model"
)

const (
	anthropicBaseURL        = "https://api.anthropic.com/v1"
	anthropicAPIVersion     = "2023-06-01"
	anthropicDefaultModel   = "claude-sonnet-4-6"
	anthropicRequestTimeout = 120 * time.Second
)

// AnthropicClient is a minimal HTTP wrapper around Anthropic's Messages API.
// Intentionally does NOT use anthropic-sdk-go — we want zero third-party deps
// so security audit stays on the direct HTTP traffic.
//
// Supports two modes:
//   - CreateMessage: non-streaming, used for /ai/reply-suggest
//   - CreateMessageStream: Server-Sent Events streaming, used for /ai/summarize
//     and /ai/translate so the UI can show progressive output.
//
// When apiKey is empty, all methods return model.ErrAIUnavailable without
// making a network call. The handler layer translates that into 503 so the
// service stays callable on Saturn.ac even before real keys are provisioned.
type AnthropicClient struct {
	apiKey string
	model  string
	http   *http.Client
	logger *slog.Logger
}

func NewAnthropicClient(apiKey, modelName string, logger *slog.Logger) *AnthropicClient {
	if logger == nil {
		logger = slog.Default()
	}
	if modelName == "" {
		modelName = anthropicDefaultModel
	}
	return &AnthropicClient{
		apiKey: strings.TrimSpace(apiKey),
		model:  modelName,
		http:   &http.Client{Timeout: anthropicRequestTimeout},
		logger: logger,
	}
}

func NewAnthropicClientFromEnv(logger *slog.Logger) *AnthropicClient {
	return NewAnthropicClient(
		os.Getenv("ANTHROPIC_API_KEY"),
		os.Getenv("ANTHROPIC_MODEL"),
		logger,
	)
}

// Configured reports whether the client has an API key. Handlers call this
// before even building prompt data so they can early-return 503 without
// spending work serialising messages.
func (c *AnthropicClient) Configured() bool {
	return c.apiKey != "" && c.apiKey != "placeholder"
}

// Model returns the resolved model name (used for usage accounting).
func (c *AnthropicClient) Model() string {
	return c.model
}

// AnthropicMessage is a single conversation turn.
type AnthropicMessage struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []AnthropicMessage `json:"messages"`
	Stream    bool               `json:"stream,omitempty"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// MessageResult is the non-streaming response we expose to the service layer.
type MessageResult struct {
	Text         string
	InputTokens  int
	OutputTokens int
}

// CreateMessage sends a non-streaming request and returns the full response.
func (c *AnthropicClient) CreateMessage(
	ctx context.Context,
	systemPrompt string,
	messages []AnthropicMessage,
	maxTokens int,
) (*MessageResult, error) {
	if !c.Configured() {
		return nil, model.ErrAIUnavailable
	}
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	body, err := json.Marshal(anthropicRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages:  messages,
		Stream:    false,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicBaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build anthropic request: %w", err)
	}
	c.setCommonHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		c.logger.Warn("anthropic non-2xx response",
			"status", resp.StatusCode,
			"body", string(errBody),
		)
		return nil, fmt.Errorf("anthropic returned %d", resp.StatusCode)
	}

	var decoded anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}

	var text strings.Builder
	for _, part := range decoded.Content {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}

	return &MessageResult{
		Text:         text.String(),
		InputTokens:  decoded.Usage.InputTokens,
		OutputTokens: decoded.Usage.OutputTokens,
	}, nil
}

// StreamEvent is the unit delivered over the channel returned by
// CreateMessageStream. Exactly one of Delta/Done/Err is non-zero per event.
type StreamEvent struct {
	// Delta contains a text chunk to append to the running output.
	Delta string
	// Done is set on the final event, carrying usage stats for accounting.
	Done *StreamDoneInfo
	// Err is set if the stream terminated with an error.
	Err error
}

type StreamDoneInfo struct {
	InputTokens  int
	OutputTokens int
}

// CreateMessageStream opens a streaming request to Anthropic and returns a
// read-only channel of StreamEvent. The channel is closed when the stream
// finishes (either a "message_stop" event, an error, or context cancellation).
//
// Anthropic streams Server-Sent Events in the form:
//
//	event: content_block_delta
//	data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hel"}}
//
//	event: message_delta
//	data: {"type":"message_delta","usage":{"output_tokens":42}}
//
//	event: message_stop
//	data: {"type":"message_stop"}
//
// We only care about content_block_delta (for text chunks) and the final
// usage field from message_delta/message_start.
func (c *AnthropicClient) CreateMessageStream(
	ctx context.Context,
	systemPrompt string,
	messages []AnthropicMessage,
	maxTokens int,
) (<-chan StreamEvent, error) {
	if !c.Configured() {
		return nil, model.ErrAIUnavailable
	}
	if maxTokens <= 0 {
		maxTokens = 2048
	}

	body, err := json.Marshal(anthropicRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages:  messages,
		Stream:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal stream request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicBaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build stream request: %w", err)
	}
	c.setCommonHeaders(req)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic stream request: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		c.logger.Warn("anthropic stream non-2xx",
			"status", resp.StatusCode,
			"body", string(errBody),
		)
		return nil, fmt.Errorf("anthropic stream returned %d", resp.StatusCode)
	}

	out := make(chan StreamEvent, 16)
	go c.consumeStream(ctx, resp.Body, out)
	return out, nil
}

// consumeStream reads Anthropic SSE lines from body and publishes StreamEvent
// values onto out. It always closes out and body before returning.
func (c *AnthropicClient) consumeStream(ctx context.Context, body io.ReadCloser, out chan<- StreamEvent) {
	defer body.Close()
	defer close(out)

	scanner := bufio.NewScanner(body)
	// SSE payloads are bounded but default Scanner max is 64KB — bump for safety.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var usage StreamDoneInfo

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		var chunk struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
			Message struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			c.logger.Warn("failed to decode anthropic stream chunk", "error", err)
			continue
		}

		switch chunk.Type {
		case "message_start":
			if chunk.Message.Usage.InputTokens > 0 {
				usage.InputTokens = chunk.Message.Usage.InputTokens
			}
		case "content_block_delta":
			if chunk.Delta.Type == "text_delta" && chunk.Delta.Text != "" {
				select {
				case out <- StreamEvent{Delta: chunk.Delta.Text}:
				case <-ctx.Done():
					return
				}
			}
		case "message_delta":
			if chunk.Usage.OutputTokens > 0 {
				usage.OutputTokens = chunk.Usage.OutputTokens
			}
		case "message_stop":
			finalUsage := usage
			select {
			case out <- StreamEvent{Done: &finalUsage}:
			case <-ctx.Done():
			}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		select {
		case out <- StreamEvent{Err: fmt.Errorf("anthropic stream read: %w", err)}:
		case <-ctx.Done():
		}
	}
}

func (c *AnthropicClient) setCommonHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)
	req.Header.Set("User-Agent", "OrbitMessenger/1.0")
}
