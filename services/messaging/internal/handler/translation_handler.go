package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

var langRe = regexp.MustCompile(`^[a-zA-Z]{2,5}$`)

type TranslationHandler struct {
	translations   store.TranslationStore
	messages       store.MessageStore
	chats          store.ChatStore
	aiServiceURL   string
	internalSecret string
	logger         *slog.Logger
	httpClient     *http.Client
}

func NewTranslationHandler(
	translations store.TranslationStore,
	messages store.MessageStore,
	chats store.ChatStore,
	aiServiceURL string,
	internalSecret string,
	logger *slog.Logger,
) *TranslationHandler {
	return &TranslationHandler{
		translations:   translations,
		messages:       messages,
		chats:          chats,
		aiServiceURL:   aiServiceURL,
		internalSecret: internalSecret,
		logger:         logger,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
	}
}

func (h *TranslationHandler) Register(app fiber.Router) {
	app.Get("/messages/:id/translation/:lang", h.GetTranslation)
	app.Get("/messages/translations", h.GetTranslationsBatch)
}

// GetTranslation returns a single cached translation, calling AI on cache miss.
func (h *TranslationHandler) GetTranslation(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}
	lang := c.Params("lang")
	if !langRe.MatchString(lang) {
		return response.Error(c, apperror.BadRequest("Invalid language code"))
	}

	// IDOR: verify membership
	msg, err := h.messages.GetByID(c.Context(), msgID)
	if err != nil {
		return response.Error(c, err)
	}
	if msg == nil {
		return response.Error(c, apperror.NotFound("Message not found"))
	}
	isMember, _, err := h.chats.IsMember(c.Context(), msg.ChatID, userID)
	if err != nil {
		return response.Error(c, err)
	}
	if !isMember {
		return response.Error(c, apperror.Forbidden("Not a member of this chat"))
	}

	// Cache hit
	t, err := h.translations.Get(c.Context(), msgID, lang)
	if err == nil && t != nil {
		return response.JSON(c, fiber.StatusOK, t)
	}

	// Cache miss — call AI service
	results, err := h.callAITranslate(c.Context(), []string{msgID.String()}, msg.ChatID.String(), lang)
	if err != nil {
		h.logger.Error("ai translate failed", "error", err)
		return response.Error(c, apperror.Internal("Translation service unavailable"))
	}
	text, ok := results[msgID.String()]
	if !ok || text == "" {
		return response.Error(c, apperror.Internal("Translation returned empty"))
	}

	tr := &model.MessageTranslation{MessageID: msgID, Lang: lang, Text: text}
	if upsertErr := h.translations.Upsert(c.Context(), tr); upsertErr != nil {
		h.logger.Error("failed to cache translation", "error", upsertErr)
	}
	return response.JSON(c, fiber.StatusOK, tr)
}

// GetTranslationsBatch returns translations for multiple messages.
func (h *TranslationHandler) GetTranslationsBatch(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	lang := c.Query("lang")
	if !langRe.MatchString(lang) {
		return response.Error(c, apperror.BadRequest("Invalid language code"))
	}
	rawIDs := strings.Split(c.Query("ids"), ",")
	if len(rawIDs) == 0 || (len(rawIDs) == 1 && rawIDs[0] == "") {
		return response.Error(c, apperror.BadRequest("ids parameter required"))
	}
	if len(rawIDs) > 50 {
		rawIDs = rawIDs[:50]
	}
	ids := make([]uuid.UUID, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			continue // skip invalid
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return response.Error(c, apperror.BadRequest("No valid message IDs"))
	}

	// Fetch messages to get chat IDs
	msgs, err := h.messages.GetByIDs(c.Context(), ids)
	if err != nil {
		return response.Error(c, err)
	}

	// Collect unique chat IDs and verify membership
	chatIDs := make(map[uuid.UUID]bool)
	msgByChatID := make(map[uuid.UUID]uuid.UUID) // messageID > chatID
	for i := range msgs {
		chatIDs[msgs[i].ChatID] = false
		msgByChatID[msgs[i].ID] = msgs[i].ChatID
	}
	for chatID := range chatIDs {
		ok, _, err := h.chats.IsMember(c.Context(), chatID, userID)
		if err != nil {
			return response.Error(c, err)
		}
		chatIDs[chatID] = ok
	}

	// Filter to allowed messages
	allowedIDs := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		chatID, exists := msgByChatID[id]
		if exists && chatIDs[chatID] {
			allowedIDs = append(allowedIDs, id)
		}
	}

	// Fetch cached translations
	cached, err := h.translations.GetBatch(c.Context(), allowedIDs, lang)
	if err != nil {
		return response.Error(c, err)
	}

	// Group uncached by chatID for per-group AI calls
	uncachedByChat := make(map[uuid.UUID][]string) // chatID -> []messageIDString
	for _, id := range allowedIDs {
		if _, ok := cached[id]; !ok {
			chatID := msgByChatID[id]
			uncachedByChat[chatID] = append(uncachedByChat[chatID], id.String())
		}
	}

	// Call AI for uncached — parallel per chat group
	failedIDs := make([]string, 0)
	if len(uncachedByChat) > 0 {
		var mu sync.Mutex
		var wg sync.WaitGroup

		for chatID, msgIDs := range uncachedByChat {
			wg.Add(1)
			go func(chatID uuid.UUID, msgIDs []string) {
				defer wg.Done()
				results, err := h.callAITranslate(c.Context(), msgIDs, chatID.String(), lang)

				mu.Lock()
				defer mu.Unlock()

				if err != nil {
					h.logger.Error("ai batch translate failed", "error", err, "chat_id", chatID)
					failedIDs = append(failedIDs, msgIDs...)
					return
				}

				// Anti-poison: only upsert keys actually in the response with non-empty text
				for _, idStr := range msgIDs {
					text, ok := results[idStr]
					if !ok || text == "" {
						failedIDs = append(failedIDs, idStr)
						continue
					}
					id, _ := uuid.Parse(idStr)
					tr := &model.MessageTranslation{MessageID: id, Lang: lang, Text: text}
					if upsertErr := h.translations.Upsert(c.Context(), tr); upsertErr != nil {
						h.logger.Error("failed to cache translation", "error", upsertErr)
					}
					cached[id] = tr
				}
			}(chatID, msgIDs)
		}

		wg.Wait()
	}

	// Build response
	translations := make(map[string]*model.MessageTranslation, len(cached))
	for id, t := range cached {
		translations[id.String()] = t
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"translations": translations,
		"uncached":     failedIDs,
	})
}

// callAITranslate calls POST /ai/translate and collects the SSE stream.
// Only returns complete translations. Partial/errored streams return error.
func (h *TranslationHandler) callAITranslate(ctx context.Context, messageIDs []string, chatID, lang string) (map[string]string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"message_ids":     messageIDs,
		"chat_id":         chatID,
		"target_language": lang,
		"response_format": "json_map",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.aiServiceURL+"/ai/translate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", h.internalSecret)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai service request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ai service returned %d", resp.StatusCode)
	}

	// Collect SSE text chunks. AI service streams "data: <text>\n" lines.
	var textBuilder strings.Builder
	complete := false
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				complete = true
				break
			}
			textBuilder.WriteString(payload)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading SSE stream: %w", err)
	}

	// If no [DONE] marker but stream ended cleanly (EOF), treat as complete
	if !complete && textBuilder.Len() > 0 {
		complete = true
	}
	if !complete || textBuilder.Len() == 0 {
		return nil, fmt.Errorf("incomplete or empty translation response")
	}

	result := make(map[string]string, len(messageIDs))
	fullText := textBuilder.String()

	if len(messageIDs) == 1 {
		result[messageIDs[0]] = fullText
	} else {
		// Extract JSON from response (strip markdown fences if Claude wrapped them)
		jsonStr := extractJSONFromResponse(fullText)
		if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
			return nil, fmt.Errorf("failed to parse AI translation response as JSON: %w", err)
		}
	}
	return result, nil
}

// extractJSONFromResponse strips markdown code fences and locates the JSON
// object in the AI response text.
func extractJSONFromResponse(raw string) string {
	stripped := strings.TrimSpace(raw)
	// Strip markdown fences
	if strings.HasPrefix(stripped, "```") {
		if idx := strings.Index(stripped, "\n"); idx != -1 {
			stripped = stripped[idx+1:]
		}
		if idx := strings.LastIndex(stripped, "```"); idx != -1 {
			stripped = stripped[:idx]
		}
		stripped = strings.TrimSpace(stripped)
	}
	// Find JSON object boundaries
	start := strings.Index(stripped, "{")
	end := strings.LastIndex(stripped, "}")
	if start != -1 && end > start {
		return stripped[start : end+1]
	}
	return stripped
}
