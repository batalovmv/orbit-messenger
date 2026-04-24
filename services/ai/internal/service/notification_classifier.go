// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mst-corp/orbit/services/ai/internal/client"
	"github.com/mst-corp/orbit/services/ai/internal/model"
)

// ClassifyNotification determines the priority of a message notification.
// Flow: cache check → rule-based pass → Claude Haiku fallback → cache result.
func (s *AIService) ClassifyNotification(
	ctx context.Context,
	userID string,
	req model.ClassifyNotificationRequest,
) (*model.ClassifyNotificationResponse, error) {
	if err := s.enforceNotificationRateLimit(ctx, userID); err != nil {
		return nil, err
	}

	// Check cache.
	cacheKey := notifCacheKey(userID, req.SenderID, req.MessageText)
	if cached, err := s.getNotifCache(ctx, cacheKey); err == nil && cached != nil {
		cached.Cached = true
		return cached, nil
	}

	// Rule-based classification.
	if result := s.classifyByRules(req); result != nil {
		s.setNotifCache(ctx, cacheKey, result)
		return result, nil
	}

	// Claude Haiku fallback.
	result := s.classifyWithAI(ctx, req)
	s.setNotifCache(ctx, cacheKey, result)
	return result, nil
}

// enforceNotificationRateLimit applies a separate 100/min limit for classify.
func (s *AIService) enforceNotificationRateLimit(ctx context.Context, userID string) error {
	if s.redis == nil {
		return nil // fail-open for notification classification
	}
	key := fmt.Sprintf("ratelimit:ai:%s:classify-notification", userID)
	result, err := rateLimitScript.Run(ctx, s.redis, []string{key}, 60).Int64()
	if err != nil {
		s.logger.Error("notification rate limiter redis error", "error", err, "user_id", userID)
		return nil // fail-open
	}
	if result > 100 {
		return fmt.Errorf("%w", model.ErrRateLimited)
	}
	return nil
}

// classifyByRules applies deterministic rules. Returns nil if no rule matched
// with a clear signal (meaning we should fall through to AI).
func (s *AIService) classifyByRules(req model.ClassifyNotificationRequest) *model.ClassifyNotificationResponse {
	priority := "normal"
	reasoning := ""
	matched := false

	// Admin DM → urgent
	if req.SenderRole == "admin" && req.ChatType == "direct" {
		return &model.ClassifyNotificationResponse{
			Priority:  "urgent",
			Reasoning: "Direct message from admin",
			Source:    "rule",
		}
	}

	// Bot in group without mention → low
	if req.SenderRole == "bot" && req.ChatType == "group" && !req.HasMention {
		return &model.ClassifyNotificationResponse{
			Priority:  "low",
			Reasoning: "Bot message in group without mention",
			Source:    "rule",
		}
	}

	// Mention → at least important
	if req.HasMention {
		priority = "important"
		reasoning = "Message mentions you"
		matched = true
	}

	// Reply to me → important
	if req.ReplyToMe {
		if priority != "important" {
			priority = "important"
			reasoning = "Reply to your message"
		} else {
			reasoning = "Reply to your message with mention"
		}
		matched = true
	}

	if matched {
		return &model.ClassifyNotificationResponse{
			Priority:  priority,
			Reasoning: reasoning,
			Source:    "rule",
		}
	}

	return nil // no clear signal → fall through to AI
}

// classifyWithAI calls Claude Haiku for classification. On any error, fails
// open with "normal" priority.
func (s *AIService) classifyWithAI(ctx context.Context, req model.ClassifyNotificationRequest) *model.ClassifyNotificationResponse {
	if s.classifyClient == nil || !s.classifyClient.Configured() {
		return &model.ClassifyNotificationResponse{
			Priority:  "normal",
			Reasoning: "AI classifier not configured",
			Source:    "rule",
		}
	}

	aiCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	text := req.MessageText
	runes := []rune(text)
	if len(runes) > 200 {
		text = string(runes[:200])
	}

	systemPrompt := "You classify message notification priority. " +
		"Respond ONLY with JSON: {\"priority\":\"urgent|important|normal|low\",\"reasoning\":\"one sentence\"}"

	userContent := fmt.Sprintf("Sender role: %s. Chat: %s. Message: %s",
		req.SenderRole, req.ChatType, text)

	messages := []client.AnthropicMessage{
		{Role: "user", Content: userContent},
	}

	result, err := s.classifyClient.CreateMessage(aiCtx, systemPrompt, messages, 128)
	if err != nil {
		s.logger.Warn("notification classify AI error, failing open", "error", err)
		return &model.ClassifyNotificationResponse{
			Priority:  "normal",
			Reasoning: "AI classification failed, defaulting to normal",
			Source:    "rule",
		}
	}

	// Parse response JSON.
	jsonStr, err := extractJSON(result.Text)
	if err != nil {
		s.logger.Warn("notification classify: failed to extract JSON", "raw", result.Text)
		return &model.ClassifyNotificationResponse{
			Priority:  "normal",
			Reasoning: "AI returned unparseable response",
			Source:    "rule",
		}
	}

	var parsed struct {
		Priority  string `json:"priority"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		s.logger.Warn("notification classify: invalid JSON", "raw", jsonStr)
		return &model.ClassifyNotificationResponse{
			Priority:  "normal",
			Reasoning: "AI returned invalid JSON",
			Source:    "rule",
		}
	}

	// Validate priority value.
	switch parsed.Priority {
	case "urgent", "important", "normal", "low":
		// ok
	default:
		return &model.ClassifyNotificationResponse{
			Priority:  "normal",
			Reasoning: "AI returned unknown priority: " + parsed.Priority,
			Source:    "rule",
		}
	}

	s.recordUsageAsync("system", "classify-notification", s.classifyClient.Model(),
		result.InputTokens, result.OutputTokens)

	return &model.ClassifyNotificationResponse{
		Priority:  parsed.Priority,
		Reasoning: parsed.Reasoning,
		Source:    "ai",
	}
}

// notifCacheKey builds a Redis key from user + sender + truncated message text.
func notifCacheKey(userID, senderID, text string) string {
	runes := []rune(text)
	if len(runes) > 100 {
		text = string(runes[:100])
	}
	h := sha256.Sum256([]byte(userID + ":" + senderID + ":" + text))
	return fmt.Sprintf("notif:classify:%x", h)
}

func (s *AIService) getNotifCache(ctx context.Context, key string) (*model.ClassifyNotificationResponse, error) {
	if s.redis == nil {
		return nil, fmt.Errorf("redis unavailable")
	}
	raw, err := s.redis.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}
	var resp model.ClassifyNotificationResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (s *AIService) setNotifCache(ctx context.Context, key string, resp *model.ClassifyNotificationResponse) {
	if s.redis == nil {
		return
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	if err := s.redis.Set(ctx, key, data, 5*time.Minute).Err(); err != nil {
		s.logger.Warn("failed to cache notification classification", "error", err)
	}
}
