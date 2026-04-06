package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

const maxConcurrentFetches = 50 // Bound goroutines spawned for fallback member-ID fetches

type pushSender interface {
	SendToUsers(userIDs []string, payload []byte) error
}

// Subscriber listens for NATS events and routes them to WebSocket clients.
type Subscriber struct {
	hub                 *Hub
	nc                  *nats.Conn
	subs                []*nats.Subscription
	messagingServiceURL string
	internalSecret      string
	httpClient          *http.Client
	pushDispatcher      pushSender
	sem                 chan struct{} // semaphore to bound concurrent goroutines
}

func NewSubscriber(hub *Hub, nc *nats.Conn, messagingServiceURL, internalSecret string, pushDispatchers ...pushSender) *Subscriber {
	var pushDispatcher pushSender
	if len(pushDispatchers) > 0 {
		pushDispatcher = pushDispatchers[0]
	}

	return &Subscriber{
		hub:                 hub,
		nc:                  nc,
		messagingServiceURL: messagingServiceURL,
		internalSecret:      internalSecret,
		httpClient:          &http.Client{Timeout: 5 * time.Second},
		pushDispatcher:      pushDispatcher,
		sem:                 make(chan struct{}, maxConcurrentFetches),
	}
}

// Start subscribes to all relevant NATS subjects.
func (s *Subscriber) Start() error {
	subjects := []string{
		"orbit.chat.*.message.new",
		"orbit.chat.*.message.updated",
		"orbit.chat.*.message.deleted",
		"orbit.chat.*.message.message_pinned",
		"orbit.chat.*.message.message_unpinned",
		"orbit.chat.*.messages.read",
		"orbit.chat.*.typing",
		"orbit.user.*.status",
		"orbit.chat.*.lifecycle",
		"orbit.chat.*.member.*",
		"orbit.user.*.mention",
		"orbit.media.*.ready",
	}

	for _, subj := range subjects {
		sub, err := s.nc.Subscribe(subj, s.handleEvent)
		if err != nil {
			return err
		}
		s.subs = append(s.subs, sub)
		slog.Info("nats: subscribed", "subject", subj)
	}

	return nil
}

// Stop unsubscribes from all NATS subjects.
func (s *Subscriber) Stop() {
	for _, sub := range s.subs {
		sub.Unsubscribe()
	}
}

type pushMessageData struct {
	ID             string  `json:"id"`
	ChatID         string  `json:"chat_id"`
	Type           string  `json:"type"`
	Content        *string `json:"content"`
	SenderName     string  `json:"sender_name"`
	SequenceNumber int64   `json:"sequence_number"`
}

type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Data  struct {
		ChatID               string `json:"chat_id"`
		MessageID            int64  `json:"message_id"`
		Type                 string `json:"type"`
		ShouldReplaceHistory bool   `json:"should_replace_history"`
	} `json:"data"`
}

func (s *Subscriber) handleNewMessageEvent(subject string, event NATSEvent, envelope Envelope) {
	if len(event.MemberIDs) > 0 {
		s.hub.SendToUsers(event.MemberIDs, envelope, "")
		s.enqueuePushDispatch(event, event.MemberIDs)
		return
	}

	chatID := extractChatIDFromSubject(subject)
	if chatID == "" {
		return
	}

	s.runAsync(
		"new_message_fallback",
		func() {
			memberIDs := s.fetchChatMemberIDs(chatID)
			if len(memberIDs) == 0 {
				return
			}

			s.hub.SendToUsers(memberIDs, envelope, "")
			s.dispatchPushNotifications(event, memberIDs)
		},
	)
}

func (s *Subscriber) enqueuePushDispatch(event NATSEvent, memberIDs []string) {
	if s.pushDispatcher == nil || len(memberIDs) == 0 {
		return
	}

	s.runAsync(
		"push_dispatch",
		func() {
			s.dispatchPushNotifications(event, memberIDs)
		},
	)
}

func (s *Subscriber) dispatchPushNotifications(event NATSEvent, memberIDs []string) {
	if s.pushDispatcher == nil || len(memberIDs) == 0 {
		return
	}

	var msg pushMessageData
	if err := json.Unmarshal(event.Data, &msg); err != nil {
		slog.Warn("nats: failed to decode new_message payload for push", "error", err)
		return
	}
	if msg.ChatID == "" || msg.ID == "" {
		slog.Warn("nats: skipping push for malformed new_message payload", "chat_id", msg.ChatID, "message_id", msg.ID)
		return
	}

	recipients := make([]string, 0, len(memberIDs))
	for _, userID := range memberIDs {
		if userID == "" || userID == event.SenderID {
			continue
		}
		recipients = append(recipients, userID)
	}
	if len(recipients) == 0 {
		return
	}

	mutedUserIDs, err := s.fetchMutedUserIDs(msg.ChatID, recipients)
	if err != nil {
		slog.Warn("nats: failed to fetch muted users, skipping push", "chat_id", msg.ChatID, "error", err)
		return
	}

	if len(mutedUserIDs) > 0 {
		mutedSet := make(map[string]struct{}, len(mutedUserIDs))
		for _, userID := range mutedUserIDs {
			mutedSet[userID] = struct{}{}
		}

		filtered := recipients[:0]
		for _, userID := range recipients {
			if _, muted := mutedSet[userID]; muted {
				continue
			}
			filtered = append(filtered, userID)
		}
		recipients = filtered
	}
	if len(recipients) == 0 {
		return
	}

	payload, err := buildPushPayload(msg)
	if err != nil {
		slog.Warn("nats: failed to marshal push payload", "error", err, "chat_id", msg.ChatID, "message_id", msg.ID)
		return
	}

	if err := s.pushDispatcher.SendToUsers(recipients, payload); err != nil {
		slog.Error("nats: push dispatch failed", "error", err, "chat_id", msg.ChatID)
	}
}

// enqueueMentionPushDispatch dispatches push for @mentions, bypassing mute checks.
func (s *Subscriber) enqueueMentionPushDispatch(event NATSEvent, memberIDs []string) {
	if s.pushDispatcher == nil || len(memberIDs) == 0 {
		return
	}

	s.runAsync(
		"mention_push_dispatch",
		func() {
			var msg pushMessageData
			if err := json.Unmarshal(event.Data, &msg); err != nil {
				slog.Warn("nats: failed to decode mention payload for push", "error", err)
				return
			}
			if msg.ChatID == "" || msg.ID == "" {
				return
			}

			recipients := make([]string, 0, len(memberIDs))
			for _, userID := range memberIDs {
				if userID == "" || userID == event.SenderID {
					continue
				}
				recipients = append(recipients, userID)
			}
			if len(recipients) == 0 {
				return
			}

			// Skip mute check — @mention always pushes per spec
			payload, err := buildPushPayload(msg)
			if err != nil {
				slog.Warn("nats: failed to marshal mention push payload", "error", err)
				return
			}

			if err := s.pushDispatcher.SendToUsers(recipients, payload); err != nil {
				slog.Error("nats: mention push dispatch failed", "error", err, "chat_id", msg.ChatID)
			}
		},
	)
}

func (s *Subscriber) fetchMutedUserIDs(chatID string, userIDs []string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, err := json.Marshal(map[string]any{
		"chat_id":  chatID,
		"user_ids": userIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/internal/notification-settings/muted-users", s.messagingServiceURL),
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", s.internalSecret)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		MutedUserIDs []string `json:"muted_user_ids"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return result.MutedUserIDs, nil
}

func (s *Subscriber) runAsync(label string, fn func()) {
	select {
	case s.sem <- struct{}{}:
		go func() {
			defer func() { <-s.sem }()
			fn()
		}()
	default:
		slog.Warn("nats: goroutine limit reached, dropping async task", "task", label)
	}
}

func buildPushPayload(msg pushMessageData) ([]byte, error) {
	if msg.SequenceNumber <= 0 {
		return nil, fmt.Errorf("invalid sequence number: %d", msg.SequenceNumber)
	}

	payload := pushPayload{
		Title: strings.TrimSpace(msg.SenderName),
		Body:  buildMessagePreview(msg.Content, msg.Type),
	}
	if payload.Title == "" {
		payload.Title = "Новое сообщение"
	}
	payload.Data.ChatID = msg.ChatID
	payload.Data.MessageID = msg.SequenceNumber
	payload.Data.Type = "new_message"
	payload.Data.ShouldReplaceHistory = true

	return json.Marshal(payload)
}

func buildMessagePreview(content *string, messageType string) string {
	if content != nil {
		trimmed := strings.TrimSpace(*content)
		if trimmed != "" {
			runes := []rune(trimmed)
			if len(runes) > 100 {
				return string(runes[:100]) + "..."
			}
			return trimmed
		}
	}

	switch messageType {
	case "photo":
		return "Фото"
	case "video":
		return "Видео"
	case "voice":
		return "Голосовое сообщение"
	case "file":
		return "Файл"
	case "gif":
		return "GIF"
	default:
		return "Новое сообщение"
	}
}

func (s *Subscriber) handleEvent(msg *nats.Msg) {
	var event NATSEvent
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		slog.Error("nats: failed to unmarshal event", "error", err, "subject", msg.Subject)
		return
	}

	slog.Debug("nats: received event", "event", event.Event, "subject", msg.Subject,
		"member_ids_count", len(event.MemberIDs), "sender_id", event.SenderID)

	envelope := Envelope{
		Type: event.Event,
		Data: event.Data,
	}

	if event.Event == EventNewMessage {
		s.handleNewMessageEvent(msg.Subject, event, envelope)
		return
	}

	// Route to specific members (messaging service provides member_ids for chat events).
	// Send to ALL members including the sender — the frontend handles dedup for own messages
	// via pendingSendUuids. Excluding the sender here would block delivery to their other
	// tabs/devices (SendToUsers excludes by userID, not by connection).
	if len(event.MemberIDs) > 0 {
		slog.Info("nats: delivering to members", "event", event.Event,
			"member_count", len(event.MemberIDs), "online_users", len(s.hub.OnlineUserIDs()))
		s.hub.SendToUsers(event.MemberIDs, envelope, "")
		return
	}

	// Fallback: if member_ids is empty for a chat fanout event, extract chat_id from subject
	// and fetch member IDs from messaging service. Subject format: orbit.chat.<chatID>.message.*
	if shouldFetchChatMemberIDs(event.Event) {
		chatID := extractChatIDFromSubject(msg.Subject)
		if chatID != "" {
			select {
			case s.sem <- struct{}{}:
				go func() {
					defer func() { <-s.sem }()
					slog.Warn("nats: member_ids empty, fetching from messaging service",
						"event", event.Event, "chat_id", chatID)
					memberIDs := s.fetchChatMemberIDs(chatID)
					if len(memberIDs) > 0 {
						s.hub.SendToUsers(memberIDs, envelope, "")
					}
				}()
			default:
				slog.Warn("nats: goroutine limit reached, dropping fallback fetch",
					"event", event.Event, "chat_id", chatID)
			}
		}
		return
	}

	// For typing/stop_typing events, fetch chat member IDs and broadcast
	if event.Event == EventTyping || event.Event == EventStopTyping {
		select {
		case s.sem <- struct{}{}:
			go func() {
				defer func() { <-s.sem }()
				var td TypingData
				if err := json.Unmarshal(event.Data, &td); err == nil {
					if _, parseErr := uuid.Parse(td.ChatID); parseErr != nil {
						slog.Warn("nats: invalid chat ID in typing data, dropping", "chat_id", td.ChatID)
						return
					}
					memberIDs := s.fetchChatMemberIDs(td.ChatID)
					var userData struct {
						UserID string `json:"user_id"`
					}
					json.Unmarshal(event.Data, &userData)
					s.hub.SendToUsers(memberIDs, envelope, userData.UserID)
				}
			}()
		default:
			slog.Warn("nats: goroutine limit reached, dropping typing fetch")
		}
		return
	}

	// For mention events, send to the mentioned user + push (bypass mute per spec)
	if event.Event == "mention" {
		if len(event.MemberIDs) > 0 {
			for _, uid := range event.MemberIDs {
				s.hub.SendToUser(uid, envelope)
			}
			// Push even if muted — @mention always notifies
			s.enqueueMentionPushDispatch(event, event.MemberIDs)
		}
		return
	}

	// Media events: route to uploader user. Subject: orbit.media.<userId>.ready
	if event.Event == EventMediaReady || event.Event == EventMediaUploadProgress {
		userID := extractUserIDFromMediaSubject(msg.Subject)
		if userID != "" {
			s.hub.SendToUser(userID, envelope)
		}
		return
	}

	// For user status events, fetch contacts from messaging service
	// and only send to users who share a chat (not all online users)
	if event.Event == EventUserStatus {
		select {
		case s.sem <- struct{}{}:
			go func() {
				defer func() { <-s.sem }()
				var sd StatusData
				if err := json.Unmarshal(event.Data, &sd); err == nil {
					if _, parseErr := uuid.Parse(sd.UserID); parseErr != nil {
						slog.Warn("nats: invalid user ID in status data, dropping", "user_id", sd.UserID)
						return
					}
					contactIDs := s.fetchContactIDs(sd.UserID)
					for _, cid := range contactIDs {
						if cid != sd.UserID {
							s.hub.SendToUser(cid, envelope)
						}
					}
				}
			}()
		default:
			slog.Warn("nats: goroutine limit reached, dropping status fetch")
		}
	}
}

func shouldFetchChatMemberIDs(event string) bool {
	switch event {
	case EventMessageUpdated,
		EventMessageDeleted,
		EventMessagesRead,
		EventMessagePinned,
		EventMessageUnpinned,
		EventReactionAdded,
		EventReactionRemoved,
		EventPollVote,
		EventPollClosed:
		return true
	default:
		return false
	}
}

// extractUserIDFromMediaSubject parses user ID from NATS subject like "orbit.media.<userId>.ready"
// Returns empty string if the segment is not a valid UUID (prevents path traversal via crafted subjects).
func extractUserIDFromMediaSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) >= 3 && parts[0] == "orbit" && parts[1] == "media" {
		if _, err := uuid.Parse(parts[2]); err != nil {
			slog.Warn("nats: invalid user ID in media subject, dropping", "subject", subject)
			return ""
		}
		return parts[2]
	}
	return ""
}

// extractChatIDFromSubject parses chat ID from NATS subject like "orbit.chat.<uuid>.message.new"
// Returns empty string if the segment is not a valid UUID (prevents path traversal via crafted subjects).
func extractChatIDFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) >= 3 && parts[0] == "orbit" && parts[1] == "chat" {
		if _, err := uuid.Parse(parts[2]); err != nil {
			slog.Warn("nats: invalid chat ID in subject, dropping", "subject", subject)
			return ""
		}
		return parts[2]
	}
	return ""
}

// fetchChatMemberIDs calls messaging service to get member IDs of a chat.
func (s *Subscriber) fetchChatMemberIDs(chatID string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/internal/chats/%s/member-ids", s.messagingServiceURL, chatID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		slog.Warn("failed to create member-ids request", "chat_id", chatID, "error", err)
		return nil
	}
	if s.internalSecret != "" {
		req.Header.Set("X-Internal-Token", s.internalSecret)
	}
	// Set a system user ID so the messaging service handler doesn't reject with 401
	req.Header.Set("X-User-ID", "00000000-0000-0000-0000-000000000000")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		slog.Warn("failed to fetch chat member IDs", "chat_id", chatID, "error", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("member-ids request failed", "chat_id", chatID, "status", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil
	}

	var result struct {
		MemberIDs []string `json:"member_ids"`
	}
	if json.Unmarshal(body, &result) != nil {
		return nil
	}
	return result.MemberIDs
}

// fetchContactIDs calls messaging service to get users who share chats with userID.
func (s *Subscriber) fetchContactIDs(userID string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/users/%s/contacts", s.messagingServiceURL, userID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		slog.Warn("failed to create contacts request", "user_id", userID, "error", err)
		return nil
	}
	if s.internalSecret != "" {
		req.Header.Set("X-Internal-Token", s.internalSecret)
	}
	req.Header.Set("X-User-ID", userID)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		slog.Warn("failed to fetch contact IDs", "user_id", userID, "error", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil
	}

	var result struct {
		ContactIDs []string `json:"contact_ids"`
	}
	if json.Unmarshal(body, &result) != nil {
		return nil
	}
	return result.ContactIDs
}
