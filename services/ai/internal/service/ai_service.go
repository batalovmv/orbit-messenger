package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/ai/internal/client"
	"github.com/mst-corp/orbit/services/ai/internal/model"
	"github.com/mst-corp/orbit/services/ai/internal/store"
)

// AIService is the business-logic layer sitting between the HTTP handlers
// and the external LLM/Whisper clients. Responsibilities:
//   - apply per-user per-endpoint rate limit (20 req/min from TZ §11.8)
//   - fetch source messages from messaging service when needed
//   - build system + user prompts for Claude
//   - async-record usage stats after each successful call
//   - translate provider errors into apperror types the handler layer
//     understands (503 ai_unavailable, 429 rate_limited, etc.)
type AIService struct {
	anthropic       *client.AnthropicClient
	whisper         *client.WhisperClient
	messaging       *client.MessagingClient
	usage           store.UsageStore
	redis           *redis.Client
	mediaServiceURL string
	internalToken   string
	logger          *slog.Logger
	rateLimitPerMin int
}

type AIServiceConfig struct {
	Anthropic       *client.AnthropicClient
	Whisper         *client.WhisperClient
	Messaging       *client.MessagingClient
	Usage           store.UsageStore
	Redis           *redis.Client
	MediaServiceURL string
	InternalToken   string
	Logger          *slog.Logger
	// RateLimitPerMin overrides the default 20 req/min/user/endpoint guard.
	RateLimitPerMin int
}

func NewAIService(cfg AIServiceConfig) *AIService {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	rl := cfg.RateLimitPerMin
	if rl <= 0 {
		rl = 20
	}
	return &AIService{
		anthropic:       cfg.Anthropic,
		whisper:         cfg.Whisper,
		messaging:       cfg.Messaging,
		usage:           cfg.Usage,
		redis:           cfg.Redis,
		mediaServiceURL: cfg.MediaServiceURL,
		internalToken:   cfg.InternalToken,
		logger:          logger,
		rateLimitPerMin: rl,
	}
}

// ---------------------------------------------------------------------------
// Rate limiting
// ---------------------------------------------------------------------------

// rateLimitScript enforces a fixed-window counter keyed by user+endpoint.
// Redis fail-closed for write endpoints (summarize/translate/suggest/transcribe)
// but fail-open for read endpoints (/ai/usage) — handled by the caller
// deciding whether to enforce the result.
var rateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return count
`)

func (s *AIService) enforceRateLimit(ctx context.Context, userID, endpoint string) error {
	if s.redis == nil {
		return apperror.Internal("Rate limiting unavailable")
	}
	key := fmt.Sprintf("ratelimit:ai:%s:%s", userID, endpoint)
	result, err := rateLimitScript.Run(ctx, s.redis, []string{key}, 60).Int64()
	if err != nil {
		s.logger.Error("ai rate limiter redis error", "error", err, "user_id", userID, "endpoint", endpoint)
		return apperror.Internal("Rate limiting unavailable")
	}
	if int(result) > s.rateLimitPerMin {
		return apperror.TooManyRequests("AI rate limit exceeded")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Transcript construction
// ---------------------------------------------------------------------------

// renderTranscript turns a list of messages into a plain-text transcript that
// Claude sees as a single user turn. Format matches conventional chat logs:
//
//	[14:32] Alice: Привет!
//	[14:33] Bob: Как дела?
func renderTranscript(messages []model.Message) string {
	var b strings.Builder
	for _, m := range messages {
		name := m.SenderName
		if name == "" {
			name = m.SenderID
		}
		b.WriteString(fmt.Sprintf("[%s] %s: %s\n",
			m.CreatedAt.Format("15:04"),
			name,
			m.Content,
		))
	}
	return b.String()
}

// parseTimeRange maps "1h" / "6h" / "24h" / "7d" into a message-count hint
// (we don't filter by created_at on the messaging side — we just fetch the
// last N and let Claude handle whatever we send).
func parseTimeRange(raw string) int {
	switch strings.TrimSpace(raw) {
	case "1h":
		return 50
	case "6h":
		return 150
	case "24h":
		return 300
	case "7d":
		return 500
	default:
		return 50
	}
}

// ---------------------------------------------------------------------------
// Usage accounting (async, best-effort)
// ---------------------------------------------------------------------------

func (s *AIService) recordUsageAsync(userID, endpoint, modelName string, inTokens, outTokens int) {
	if s.usage == nil {
		return
	}
	rec := model.UsageRecord{
		UserID:       userID,
		Endpoint:     endpoint,
		Model:        modelName,
		InputTokens:  inTokens,
		OutputTokens: outTokens,
		CostCents:    estimateCostCents(modelName, inTokens, outTokens),
		CreatedAt:    time.Now().UTC(),
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.usage.Record(ctx, rec); err != nil {
			s.logger.Warn("failed to record ai usage", "error", err, "endpoint", endpoint)
		}
	}()
}

// estimateCostCents is a rough provider-agnostic estimate in US cents,
// multiplied by 100 so 1.2 cents = 120 cost_cents. Updated manually when
// provider pricing changes — no live price fetching.
func estimateCostCents(modelName string, inTokens, outTokens int) int {
	// Anthropic Sonnet pricing (USD per 1M tokens): input $3, output $15.
	// 1 token ≈ 0.0003 cents input, 0.0015 cents output. Multiply by 100
	// so the integer column carries 2 decimals.
	if strings.Contains(modelName, "sonnet") {
		return (inTokens*30 + outTokens*150) / 10000
	}
	if strings.Contains(modelName, "opus") {
		return (inTokens*150 + outTokens*750) / 10000
	}
	if strings.Contains(modelName, "haiku") {
		return (inTokens*80 + outTokens*400) / 100000
	}
	if strings.Contains(modelName, "whisper") {
		// Whisper: $0.006 per minute. Can't derive from tokens — caller
		// should pre-compute duration-based cost; we fall back to 1 cent.
		return 100
	}
	return 0
}

// ---------------------------------------------------------------------------
// Summarize (SSE streaming)
// ---------------------------------------------------------------------------

// Summarize fetches recent messages from the chat and asks Claude to
// summarize them. Returns a channel of text chunks plus a Done signal.
//
// The caller (HTTP handler) is responsible for plumbing the channel through
// to Server-Sent Events output.
func (s *AIService) Summarize(
	ctx context.Context,
	userID string,
	req model.SummarizeRequest,
) (<-chan client.StreamEvent, error) {
	if !s.anthropic.Configured() {
		return nil, apperror.ServiceUnavailable("AI provider not configured")
	}
	if err := s.enforceRateLimit(ctx, userID, "summarize"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ChatID) == "" {
		return nil, apperror.BadRequest("chat_id is required")
	}

	limit := parseTimeRange(req.TimeRange)
	messages, err := s.messaging.FetchRecentMessages(ctx, userID, req.ChatID, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch chat messages: %w", err)
	}
	if len(messages) == 0 {
		return nil, apperror.BadRequest("No messages to summarize")
	}

	language := strings.TrimSpace(req.Language)
	if language == "" {
		language = "ru"
	}

	systemPrompt := fmt.Sprintf(
		"Ты — AI-ассистент корпоративного мессенджера Orbit. "+
			"Пользователь просит суммаризовать диалог. "+
			"Ответь на языке '%s'. Будь конкретным, выдели ключевые решения, action items и открытые вопросы. "+
			"Если диалог короткий — дай сжатое резюме в 2-3 предложения. "+
			"Не повторяй дословно фразы собеседников.",
		language,
	)

	transcript := renderTranscript(messages)
	claudeMessages := []client.AnthropicMessage{
		{Role: "user", Content: "Вот последние сообщения из чата:\n\n" + transcript + "\nПожалуйста, суммаризуй."},
	}

	streamCh, err := s.anthropic.CreateMessageStream(ctx, systemPrompt, claudeMessages, 1024)
	if err != nil {
		if errors.Is(err, model.ErrAIUnavailable) {
			return nil, apperror.ServiceUnavailable("AI provider not configured")
		}
		return nil, fmt.Errorf("anthropic stream: %w", err)
	}

	// Wrap the raw provider channel to intercept the Done event and
	// record usage stats before forwarding to the caller.
	out := make(chan client.StreamEvent, 16)
	go func() {
		defer close(out)
		for event := range streamCh {
			if event.Done != nil {
				s.recordUsageAsync(userID, "summarize", s.anthropic.Model(),
					event.Done.InputTokens, event.Done.OutputTokens)
			}
			select {
			case out <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// ---------------------------------------------------------------------------
// Translate (SSE streaming)
// ---------------------------------------------------------------------------

func (s *AIService) Translate(
	ctx context.Context,
	userID string,
	req model.TranslateRequest,
) (<-chan client.StreamEvent, error) {
	if !s.anthropic.Configured() {
		return nil, apperror.ServiceUnavailable("AI provider not configured")
	}
	if err := s.enforceRateLimit(ctx, userID, "translate"); err != nil {
		return nil, err
	}
	if len(req.MessageIDs) == 0 {
		return nil, apperror.BadRequest("message_ids is required")
	}
	if strings.TrimSpace(req.TargetLanguage) == "" {
		return nil, apperror.BadRequest("target_language is required")
	}

	messages, err := s.messaging.FetchMessagesByIDs(ctx, userID, req.MessageIDs)
	if err != nil {
		return nil, fmt.Errorf("fetch messages by ids: %w", err)
	}
	if len(messages) == 0 {
		return nil, apperror.BadRequest("No messages found for translation")
	}

	// JSON-map branch: non-streaming, returns {uuid: translated_text} for batch.
	if req.ResponseFormat == "json_map" && len(messages) > 1 {
		return s.translateJSONMap(ctx, userID, req, messages)
	}

	// Different prompts for single-message (inline per-bubble UX) vs batch
	// (modal over a selection). The single path returns ONLY the translated
	// text with no prefixes or meta-commentary, because the UI displays it
	// verbatim under the bubble — any "[time] Name:" leak or
	// "This is a command, nothing to translate" commentary is noise.
	var systemPrompt string
	var userContent string
	if len(messages) == 1 {
		systemPrompt = fmt.Sprintf(
			"Ты — переводчик. Переведи сообщение на язык '%s'. "+
				"Верни ТОЛЬКО переведённый текст — без пояснений, без префиксов вида '[время] Имя:', "+
				"без кавычек, без отметок 'Translation:'. "+
				"Если сообщение уже на целевом языке — верни его БЕЗ изменений, так же без комментариев. "+
				"Команды (начинающиеся с '/'), имена пользователей (@username), URL и код не переводи, "+
				"просто включи их как есть. Никаких мета-комментариев в духе 'нечего переводить'.",
			req.TargetLanguage,
		)
		userContent = messages[0].Content
	} else {
		systemPrompt = fmt.Sprintf(
			"Ты — AI-ассистент корпоративного мессенджера Orbit. "+
				"Пользователь просит перевести сообщения на язык '%s'. "+
				"Переведи КАЖДОЕ сообщение отдельно, сохранив формат вида '[время] Имя: текст'. "+
				"Сохраняй тон и регистр оригинала. Если сообщение уже на целевом языке — оставь как есть.",
			req.TargetLanguage,
		)
		userContent = "Сообщения для перевода:\n\n" + renderTranscript(messages)
	}

	claudeMessages := []client.AnthropicMessage{
		{Role: "user", Content: userContent},
	}

	streamCh, err := s.anthropic.CreateMessageStream(ctx, systemPrompt, claudeMessages, 2048)
	if err != nil {
		if errors.Is(err, model.ErrAIUnavailable) {
			return nil, apperror.ServiceUnavailable("AI provider not configured")
		}
		return nil, fmt.Errorf("anthropic stream: %w", err)
	}

	out := make(chan client.StreamEvent, 16)
	go func() {
		defer close(out)
		for event := range streamCh {
			if event.Done != nil {
				s.recordUsageAsync(userID, "translate", s.anthropic.Model(),
					event.Done.InputTokens, event.Done.OutputTokens)
			}
			select {
			case out <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// ---------------------------------------------------------------------------
// Translate JSON-map (non-streaming batch)
// ---------------------------------------------------------------------------

func (s *AIService) translateJSONMap(
	ctx context.Context,
	userID string,
	req model.TranslateRequest,
	messages []model.Message,
) (<-chan client.StreamEvent, error) {
	systemPrompt := fmt.Sprintf(
		"You are a translator. Translate each message to the target language '%s'. "+
			"Return ONLY a valid JSON object mapping message UUIDs to their translated text. "+
			"No markdown fences, no explanation, no preamble. "+
			`Format: {"uuid1": "translated text 1", "uuid2": "translated text 2"} `+
			"If a message is already in the target language, include it unchanged. "+
			"Do not translate commands (starting with '/'), @mentions, URLs, or code blocks — include them as-is.",
		req.TargetLanguage,
	)

	var userContent strings.Builder
	for _, m := range messages {
		userContent.WriteString(m.ID)
		userContent.WriteString(": ")
		userContent.WriteString(m.Content)
		userContent.WriteByte('\n')
	}

	claudeMessages := []client.AnthropicMessage{
		{Role: "user", Content: userContent.String()},
	}

	result, err := s.anthropic.CreateMessage(ctx, systemPrompt, claudeMessages, 4096)
	if err != nil {
		if errors.Is(err, model.ErrAIUnavailable) {
			return nil, apperror.ServiceUnavailable("AI provider not configured")
		}
		return nil, fmt.Errorf("anthropic create message: %w", err)
	}

	jsonStr, err := extractJSON(result.Text)
	if err != nil {
		return nil, apperror.Internal("Failed to parse translation response: " + err.Error())
	}

	// Validate that it's a proper map.
	var parsed map[string]string
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, apperror.Internal("AI returned invalid JSON map: " + err.Error())
	}

	s.recordUsageAsync(userID, "translate", s.anthropic.Model(),
		result.InputTokens, result.OutputTokens)

	out := make(chan client.StreamEvent, 2)
	go func() {
		defer close(out)
		out <- client.StreamEvent{Delta: jsonStr}
		out <- client.StreamEvent{Done: &client.StreamDoneInfo{
			InputTokens:  result.InputTokens,
			OutputTokens: result.OutputTokens,
		}}
	}()
	return out, nil
}

// extractJSON strips markdown code fences and locates the JSON object in raw
// Claude output.
func extractJSON(raw string) (string, error) {
	stripped := strings.TrimSpace(raw)
	if strings.HasPrefix(stripped, "```") {
		if idx := strings.Index(stripped, "\n"); idx != -1 {
			stripped = stripped[idx+1:]
		}
		if idx := strings.LastIndex(stripped, "```"); idx != -1 {
			stripped = stripped[:idx]
		}
		stripped = strings.TrimSpace(stripped)
	}
	start := strings.Index(stripped, "{")
	end := strings.LastIndex(stripped, "}")
	if start == -1 || end == -1 || end <= start {
		return "", fmt.Errorf("no JSON object found in response")
	}
	return stripped[start : end+1], nil
}

// ---------------------------------------------------------------------------
// Reply suggest (non-streaming, returns 3 variants)
// ---------------------------------------------------------------------------

func (s *AIService) SuggestReply(
	ctx context.Context,
	userID string,
	req model.ReplySuggestRequest,
) ([]string, error) {
	if !s.anthropic.Configured() {
		return nil, apperror.ServiceUnavailable("AI provider not configured")
	}
	if err := s.enforceRateLimit(ctx, userID, "reply-suggest"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ChatID) == "" {
		return nil, apperror.BadRequest("chat_id is required")
	}

	messages, err := s.messaging.FetchRecentMessages(ctx, userID, req.ChatID, 20)
	if err != nil {
		return nil, fmt.Errorf("fetch chat messages: %w", err)
	}
	if len(messages) == 0 {
		return nil, apperror.BadRequest("No messages to analyze")
	}

	systemPrompt := "Ты — AI-ассистент корпоративного мессенджера Orbit. " +
		"Пользователь просит предложить 3 варианта ответа на последние сообщения в чате. " +
		"Верни РОВНО 3 варианта, каждый на отдельной строке, без нумерации и без кавычек. " +
		"Каждый вариант должен быть содержательным законченным сообщением (от 3 до 20 слов), " +
		"различаться тоном: формальный, дружеский, нейтрально-деловой. " +
		"Избегай односложных ответов (\"ок\", \"@username\") — они бесполезны. " +
		"Отвечай на том языке, на котором велась переписка."

	transcript := renderTranscript(messages)
	claudeMessages := []client.AnthropicMessage{
		{Role: "user", Content: "Последние сообщения:\n\n" + transcript + "\nПредложи 3 варианта ответа."},
	}

	result, err := s.anthropic.CreateMessage(ctx, systemPrompt, claudeMessages, 512)
	if err != nil {
		if errors.Is(err, model.ErrAIUnavailable) {
			return nil, apperror.ServiceUnavailable("AI provider not configured")
		}
		return nil, fmt.Errorf("anthropic create message: %w", err)
	}

	s.recordUsageAsync(userID, "reply-suggest", s.anthropic.Model(),
		result.InputTokens, result.OutputTokens)

	suggestions := splitSuggestions(result.Text)
	if len(suggestions) == 0 {
		return nil, apperror.Internal("AI returned empty suggestions")
	}
	return suggestions, nil
}

// Ask powers the @orbit-ai chat mention. The messaging service calls
// this with the text that followed the mention and the chat_id; we
// pull a short transcript for context, ask Claude, and return one
// reply string that messaging will post back as a new message.
func (s *AIService) Ask(
	ctx context.Context,
	userID string,
	req model.AskRequest,
) (string, error) {
	if !s.anthropic.Configured() {
		return "", apperror.ServiceUnavailable("AI provider not configured")
	}
	if err := s.enforceRateLimit(ctx, userID, "ask"); err != nil {
		return "", err
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return "", apperror.BadRequest("prompt is required")
	}
	if len(prompt) > 4096 {
		return "", apperror.BadRequest("prompt is too long")
	}

	var transcript string
	if strings.TrimSpace(req.ChatID) != "" {
		messages, err := s.messaging.FetchRecentMessages(ctx, userID, req.ChatID, 10)
		if err == nil && len(messages) > 0 {
			transcript = renderTranscript(messages)
		}
	}

	systemPrompt := "Ты — @orbit-ai, AI-ассистент внутри корпоративного мессенджера Orbit. " +
		"Пользователь упомянул тебя в чате и задал вопрос. Отвечай кратко, по делу, " +
		"на том же языке, что и вопрос. Если в контексте переписки есть релевантная " +
		"информация — используй её; если нет — отвечай из общих знаний. " +
		"Не используй Markdown-форматирование, отвечай обычным текстом."

	userContent := prompt
	if transcript != "" {
		userContent = "Контекст чата:\n\n" + transcript + "\n\nВопрос: " + prompt
	}

	claudeMessages := []client.AnthropicMessage{
		{Role: "user", Content: userContent},
	}
	result, err := s.anthropic.CreateMessage(ctx, systemPrompt, claudeMessages, 1024)
	if err != nil {
		if errors.Is(err, model.ErrAIUnavailable) {
			return "", apperror.ServiceUnavailable("AI provider not configured")
		}
		return "", fmt.Errorf("anthropic create message: %w", err)
	}
	s.recordUsageAsync(userID, "ask", s.anthropic.Model(),
		result.InputTokens, result.OutputTokens)

	reply := strings.TrimSpace(result.Text)
	if reply == "" {
		return "", apperror.Internal("AI returned empty reply")
	}
	return reply, nil
}

func splitSuggestions(raw string) []string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	out := make([]string, 0, 3)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Strip common list prefixes.
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		if len(line) > 2 && line[1] == '.' && line[0] >= '0' && line[0] <= '9' {
			line = strings.TrimSpace(line[2:])
		}
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) == 3 {
			break
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Transcribe (non-streaming, Whisper)
// ---------------------------------------------------------------------------

func (s *AIService) Transcribe(
	ctx context.Context,
	userID string,
	req model.TranscribeRequest,
) (*model.TranscribeResponse, error) {
	if !s.whisper.Configured() {
		return nil, apperror.ServiceUnavailable("Whisper provider not configured")
	}
	if err := s.enforceRateLimit(ctx, userID, "transcribe"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.MediaID) == "" {
		return nil, apperror.BadRequest("media_id is required")
	}

	audio, contentType, err := client.FetchMediaBytes(
		ctx,
		s.mediaServiceURL,
		req.MediaID,
		userID,
		s.internalToken,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch audio: %w", err)
	}

	filename := "voice.ogg"
	if strings.Contains(contentType, "mp4") {
		filename = "voice.mp4"
	} else if strings.Contains(contentType, "mpeg") {
		filename = "voice.mp3"
	} else if strings.Contains(contentType, "webm") {
		filename = "voice.webm"
	}

	result, err := s.whisper.TranscribeAudio(ctx, audio, filename, "")
	if err != nil {
		if errors.Is(err, model.ErrAIUnavailable) {
			return nil, apperror.ServiceUnavailable("Whisper provider not configured")
		}
		return nil, fmt.Errorf("whisper transcribe: %w", err)
	}

	s.recordUsageAsync(userID, "transcribe", s.whisper.Model(), 0, 0)

	return &model.TranscribeResponse{
		Text:     result.Text,
		Language: result.Language,
	}, nil
}

// ---------------------------------------------------------------------------
// Semantic search — intentionally unimplemented (Phase 8A.2)
// ---------------------------------------------------------------------------

// SemanticSearch is a placeholder — real implementation requires embeddings
// and pgvector, which we defer to a later phase. The handler translates the
// returned error into 501 Not Implemented.
func (s *AIService) SemanticSearch(
	ctx context.Context,
	userID string,
	req model.SearchRequest,
) (any, error) {
	return nil, apperror.NotImplemented("Semantic search is not yet available (Phase 8A.2)")
}

// ---------------------------------------------------------------------------
// Usage stats
// ---------------------------------------------------------------------------

func (s *AIService) GetUsage(ctx context.Context, userID string) (*model.UsageStats, error) {
	if s.usage == nil {
		return &model.UsageStats{
			ByEndpoint:  make(map[string]int),
			Cost:        make(map[string]int),
			PeriodStart: time.Now().UTC().AddDate(0, 0, -30),
		}, nil
	}
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, apperror.BadRequest("Invalid user id")
	}
	since := time.Now().UTC().AddDate(0, 0, -30)
	return s.usage.GetUserStats(ctx, uid, since)
}
