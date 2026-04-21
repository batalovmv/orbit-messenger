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

	// Find uncached
	uncachedIDs := make([]string, 0)
	uncachedChatID := ""
	for _, id := range allowedIDs {
		if _, ok := cached[id]; !ok {
			uncachedIDs = append(uncachedIDs, id.String())
			if cid, ok := msgByChatID[id]; ok {
				uncachedChatID = cid.String()
			}
		}
	}

	// Call AI for uncached
	failedIDs := make([]string, 0)
	if len(uncachedIDs) > 0 {
		results, err := h.callAITranslate(c.Context(), uncachedIDs, uncachedChatID, lang)
		if err != nil {
			h.logger.Error("ai batch translate failed", "error", err)
			failedIDs = uncachedIDs
		} else {
			for _, idStr := range uncachedIDs {
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
		}
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

	// For single message, map the full text to the first ID.
	// AI service returns a single stream for the batch.
	result := make(map[string]string, len(messageIDs))
	if len(messageIDs) == 1 {
		result[messageIDs[0]] = textBuilder.String()
	} else {
		// Try to parse as JSON map if batch response
		var parsed map[string]string
		if err := json.Unmarshal([]byte(textBuilder.String()), &parsed); err == nil {
			result = parsed
		} else {
			// Fallback: assign full text to first ID
			result[messageIDs[0]] = textBuilder.String()
		}
	}
	return result, nil
}
