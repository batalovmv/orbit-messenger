package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mst-corp/orbit/services/ai/internal/model"
)

const (
	whisperBaseURL        = "https://api.openai.com/v1"
	whisperDefaultModel   = "whisper-1"
	whisperMaxFileSize    = 25 * 1024 * 1024 // 25MB — OpenAI hard limit
	whisperRequestTimeout = 180 * time.Second
)

// WhisperClient transcribes audio via OpenAI's Whisper API. Same design
// principle as AnthropicClient: plain net/http, no SDK.
//
// We only use non-streaming transcription (Whisper's streaming variant is
// effectively verbose JSON responses, not SSE, and offers no UX advantage
// for short voice messages).
type WhisperClient struct {
	apiKey string
	model  string
	http   *http.Client
	logger *slog.Logger
}

func NewWhisperClient(apiKey, modelName string, logger *slog.Logger) *WhisperClient {
	if logger == nil {
		logger = slog.Default()
	}
	if modelName == "" {
		modelName = whisperDefaultModel
	}
	return &WhisperClient{
		apiKey: strings.TrimSpace(apiKey),
		model:  modelName,
		http:   &http.Client{Timeout: whisperRequestTimeout},
		logger: logger,
	}
}

func NewWhisperClientFromEnv(logger *slog.Logger) *WhisperClient {
	return NewWhisperClient(
		os.Getenv("OPENAI_API_KEY"),
		os.Getenv("WHISPER_MODEL"),
		logger,
	)
}

func (c *WhisperClient) Configured() bool {
	return c.apiKey != "" && c.apiKey != "placeholder"
}

func (c *WhisperClient) Model() string {
	return c.model
}

// TranscribeResult contains the transcription plus the detected language
// (Whisper returns an ISO-639-1 code in verbose_json mode).
type TranscribeResult struct {
	Text     string
	Language string
}

// TranscribeAudio sends the audio bytes to Whisper as a multipart POST.
// filename is only used for MIME-type detection by OpenAI and should reflect
// the original extension (e.g. "voice.ogg", "voice.mp4"). The content type
// does not need to match — Whisper sniffs from filename.
func (c *WhisperClient) TranscribeAudio(
	ctx context.Context,
	audio []byte,
	filename string,
	language string,
) (*TranscribeResult, error) {
	if !c.Configured() {
		return nil, model.ErrAIUnavailable
	}
	if len(audio) == 0 {
		return nil, fmt.Errorf("empty audio body")
	}
	if len(audio) > whisperMaxFileSize {
		return nil, fmt.Errorf("audio file too large (%d bytes, max %d)", len(audio), whisperMaxFileSize)
	}
	if strings.TrimSpace(filename) == "" {
		filename = "audio.ogg"
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("whisper multipart file: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return nil, fmt.Errorf("whisper multipart write: %w", err)
	}

	if err := writer.WriteField("model", c.model); err != nil {
		return nil, fmt.Errorf("whisper multipart model field: %w", err)
	}
	if err := writer.WriteField("response_format", "verbose_json"); err != nil {
		return nil, fmt.Errorf("whisper multipart format field: %w", err)
	}
	if strings.TrimSpace(language) != "" {
		if err := writer.WriteField("language", language); err != nil {
			return nil, fmt.Errorf("whisper multipart language field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("whisper multipart close: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, whisperBaseURL+"/audio/transcriptions", &body)
	if err != nil {
		return nil, fmt.Errorf("build whisper request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "OrbitMessenger/1.0")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("whisper request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		c.logger.Warn("whisper non-2xx response",
			"status", resp.StatusCode,
			"body", string(errBody),
		)
		return nil, fmt.Errorf("whisper returned %d", resp.StatusCode)
	}

	var decoded struct {
		Text     string `json:"text"`
		Language string `json:"language"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode whisper response: %w", err)
	}

	return &TranscribeResult{
		Text:     strings.TrimSpace(decoded.Text),
		Language: decoded.Language,
	}, nil
}

// FetchMediaBytes is a helper for the service layer: downloads media from the
// internal media service with proper X-User-ID and X-Internal-Token headers
// (required by the media service). Returns the raw bytes plus content-type.
//
// Stays in the client package because it's tightly coupled to the Whisper
// flow (transcribe needs audio bytes, and this is the only place that fetches
// them).
func FetchMediaBytes(
	ctx context.Context,
	mediaServiceURL string,
	mediaID string,
	userID string,
	internalToken string,
) ([]byte, string, error) {
	url := fmt.Sprintf("%s/media/%s", strings.TrimRight(mediaServiceURL, "/"), mediaID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build media request: %w", err)
	}
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-Internal-Token", internalToken)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("media fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("media service returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, whisperMaxFileSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("read media body: %w", err)
	}
	if len(data) > whisperMaxFileSize {
		return nil, "", fmt.Errorf("media too large (max %d bytes)", whisperMaxFileSize)
	}
	return data, resp.Header.Get("Content-Type"), nil
}
