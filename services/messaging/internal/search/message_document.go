package search

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

var linkPattern = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+`)

type messageEntity struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// BuildMessageDocument creates a normalized message document for Meilisearch.
func BuildMessageDocument(
	id string,
	chatID string,
	senderID string,
	content string,
	messageType string,
	hasMedia bool,
	hasLinks bool,
	createdAt time.Time,
	sequenceNumber int64,
) map[string]interface{} {
	return map[string]interface{}{
		"id":              id,
		"chat_id":         chatID,
		"sender_id":       senderID,
		"content":         content,
		"type":            messageType,
		"has_media":       hasMedia,
		"has_links":       hasLinks,
		"created_at_ts":   createdAt.Unix(),
		"sequence_number": sequenceNumber,
	}
}

// HasLinks reports whether a message contains link entities or obvious URL text.
func HasLinks(content string, entities json.RawMessage) bool {
	if linkPattern.MatchString(content) {
		return true
	}

	if len(entities) == 0 {
		return false
	}

	var parsed []messageEntity
	if err := json.Unmarshal(entities, &parsed); err != nil {
		return false
	}

	for _, entity := range parsed {
		normalizedType := strings.ToLower(entity.Type)
		if entity.URL != "" || strings.Contains(normalizedType, "url") || strings.Contains(normalizedType, "link") {
			return true
		}
	}

	return false
}
