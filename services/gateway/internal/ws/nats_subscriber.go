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
	SendCallToUsers(userIDs []string, payload []byte) error
	SendToUsersWithPriority(userIDs []string, payload []byte, priority string) error
}

const (
	// dedupCacheCapacity is the number of event IDs held in the dedup LRU.
	// At 150 users × ~10 events/s peak the cache covers ~100 seconds of events.
	dedupCacheCapacity = 1024

	// durableName is the JetStream durable consumer name for the gateway WS fanout.
	// A single durable covering orbit.> means one ordered stream of all events.
	durableName = "gateway-ws"
)

// Subscriber listens for NATS events and routes them to WebSocket clients.
type Subscriber struct {
	hub                 *Hub
	nc                  *nats.Conn
	js                  nats.JetStreamContext // non-nil in production; nil in unit tests
	subs                []*nats.Subscription
	messagingServiceURL string
	internalSecret      string
	httpClient          *http.Client
	pushDispatcher      pushSender
	classifier          *NotificationClassifier
	sem                 chan struct{} // semaphore to bound concurrent goroutines
	dedup               *dedupCache  // deduplicates redelivered JetStream events
}

// SetNotificationClassifier attaches an AI notification classifier to the subscriber.
func (s *Subscriber) SetNotificationClassifier(nc *NotificationClassifier) {
	s.classifier = nc
}

// NewSubscriber creates a Subscriber backed by a JetStream durable consumer.
// When js is nil (unit tests) the subscriber falls back to core NATS nc.Subscribe.
func NewSubscriber(hub *Hub, nc *nats.Conn, messagingServiceURL, internalSecret string, pushDispatchers ...pushSender) *Subscriber {
	var pushDispatcher pushSender
	if len(pushDispatchers) > 0 {
		pushDispatcher = pushDispatchers[0]
	}

	var js nats.JetStreamContext
	if nc != nil {
		if ctx, err := nc.JetStream(); err == nil {
			js = ctx
		}
	}

	return &Subscriber{
		hub:                 hub,
		nc:                  nc,
		js:                  js,
		messagingServiceURL: messagingServiceURL,
		internalSecret:      internalSecret,
		httpClient:          &http.Client{Timeout: 5 * time.Second},
		pushDispatcher:      pushDispatcher,
		sem:                 make(chan struct{}, maxConcurrentFetches),
		dedup:               newDedupCache(dedupCacheCapacity),
	}
}

// Start subscribes to all orbit.> events via a JetStream durable consumer.
// Falls back to core nc.Subscribe when JetStream is unavailable (unit tests).
func (s *Subscriber) Start() error {
	if s.js != nil {
		// Single durable subscription covering the entire orbit.> hierarchy.
		// AckExplicit: we ack after successful fanout, nak on error so JetStream retries.
		sub, err := s.js.Subscribe(
			"orbit.>",
			s.handleJSEvent,
			nats.Durable(durableName),
			nats.AckExplicit(),
			nats.DeliverNew(),
		)
		if err == nil {
			s.subs = append(s.subs, sub)
			slog.Info("nats: JetStream durable subscriber started", "durable", durableName, "subject", "orbit.>")
			return nil
		}
		// JetStream subscribe failed (e.g. JS not enabled on server) — fall back to core NATS.
		slog.Warn("nats: JetStream subscribe failed, falling back to core NATS", "error", err)
		s.js = nil
	}

	// Fallback: core NATS (no at-least-once delivery).
	subjects := []string{
		"orbit.chat.*.message.new",
		"orbit.chat.*.message.updated",
		"orbit.chat.*.message.deleted",
		"orbit.chat.*.message.message_pinned",
		"orbit.chat.*.message.message_unpinned",
		"orbit.chat.*.messages.read",
		"orbit.chat.*.typing",
		"orbit.user.*.status",
		"orbit.user.*.deactivated",
		"orbit.chat.*.lifecycle",
		"orbit.chat.*.member.*",
		"orbit.chat.*.bot.*",
		"orbit.user.*.mention",
		"orbit.media.*.ready",
		"orbit.call.*.lifecycle",
		"orbit.call.*.participant",
		"orbit.call.*.media",
	}
	for _, subj := range subjects {
		sub, err := s.nc.Subscribe(subj, s.handleEvent)
		if err != nil {
			return err
		}
		s.subs = append(s.subs, sub)
		slog.Info("nats: subscribed (core)", "subject", subj)
	}
	return nil
}

// Stop unsubscribes from all NATS subjects.
func (s *Subscriber) Stop() {
	for _, sub := range s.subs {
		sub.Unsubscribe()
	}
}

// handleJSEvent wraps handleEvent for JetStream messages: deduplicates by
// Nats-Msg-Id header, acks on success, naks on error so JetStream retries.
func (s *Subscriber) handleJSEvent(msg *nats.Msg) {
	// Dedup by Nats-Msg-Id header set by the publisher (Sub-commit 3).
	// Fall back to sequence number when the header is absent (old publishers).
	dedupKey := msg.Header.Get("Nats-Msg-Id")
	if dedupKey == "" {
		if meta, err := msg.Metadata(); err == nil {
			dedupKey = fmt.Sprintf("%s:%d", msg.Subject, meta.Sequence.Stream)
		} else {
			dedupKey = fmt.Sprintf("%s:%d", msg.Subject, time.Now().UnixNano())
		}
	}

	if s.dedup.seen(dedupKey) {
		slog.Debug("nats: duplicate event skipped", "key", dedupKey, "subject", msg.Subject)
		if err := msg.Ack(); err != nil {
			slog.Warn("nats: ack failed on duplicate", "error", err)
		}
		return
	}

	// Fanout the event; handleEvent is side-effect only (no error return).
	// We consider fanout successful if handleEvent does not panic — errors
	// within (push dispatch, member fetch) are already logged and retried
	// internally, not surfaced as NATS-level failures.
	s.handleEvent(msg)

	if err := msg.Ack(); err != nil {
		slog.Warn("nats: ack failed after fanout", "error", err, "subject", msg.Subject)
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
		Priority             string `json:"priority,omitempty"`
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

	// Classify notification priority (fail-open to "normal")
	priority := defaultPriority
	if s.classifier != nil {
		priority = s.classifier.Classify(context.Background(), classifyRequest{
			SenderID:    event.SenderID,
			SenderRole:  "member",
			ChatType:    inferChatType(event),
			MessageText: stringPtrToString(msg.Content),
			HasMention:  false,
			ReplyToMe:   false,
		})
	}

	payload = injectPriorityIntoPayload(payload, priority)

	if err := s.pushDispatcher.SendToUsersWithPriority(recipients, payload, priority); err != nil {
		slog.Error("nats: push dispatch failed", "error", err, "chat_id", msg.ChatID, "priority", priority)
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

			// Mentions are always "important" priority — no AI call needed
			payload = injectPriorityIntoPayload(payload, "important")
			if err := s.pushDispatcher.SendToUsersWithPriority(recipients, payload, "important"); err != nil {
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

func inferChatType(event NATSEvent) string {
	if len(event.MemberIDs) == 2 {
		return "direct"
	}
	return "group"
}

func stringPtrToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func injectPriorityIntoPayload(payload []byte, priority string) []byte {
	var p pushPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return payload
	}
	p.Data.Priority = priority
	result, err := json.Marshal(p)
	if err != nil {
		return payload
	}
	return result
}

func buildPushPayload(msg pushMessageData) ([]byte, error) {
	if msg.SequenceNumber <= 0 {
		return nil, fmt.Errorf("invalid sequence number: %d", msg.SequenceNumber)
	}

	payload := pushPayload{
		Title: strings.TrimSpace(msg.SenderName),
	}
	// For E2E encrypted messages, don't include content preview.
	if msg.Type == "encrypted" {
		payload.Body = "Новое сообщение"
	} else {
		payload.Body = buildMessagePreview(msg.Content, msg.Type)
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

// callPushData mirrors the fields published on orbit.call.<id>.lifecycle for
// the call_incoming event. We only decode what the service worker actually
// uses to render the notification.
type callPushData struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Mode        string `json:"mode"`
	ChatID      string `json:"chat_id"`
	InitiatorID string `json:"initiator_id"`
}

type callPushPayload struct {
	Type       string `json:"type"`
	CallID     string `json:"call_id"`
	CallerID   string `json:"caller_id"`
	CallerName string `json:"caller_name"`
	CallType   string `json:"call_type"`
	CallMode   string `json:"call_mode"`
	ChatID     string `json:"chat_id"`
	Timestamp  int64  `json:"timestamp"`
}

func (s *Subscriber) enqueueCallPushDispatch(event NATSEvent) {
	if s.pushDispatcher == nil || len(event.MemberIDs) == 0 {
		return
	}

	s.runAsync(
		"call_push_dispatch",
		func() {
			s.dispatchCallPushNotifications(event)
		},
	)
}

func (s *Subscriber) dispatchCallPushNotifications(event NATSEvent) {
	if s.pushDispatcher == nil {
		return
	}

	var call callPushData
	if err := json.Unmarshal(event.Data, &call); err != nil {
		slog.Warn("nats: failed to decode call_incoming payload for push", "error", err)
		return
	}
	if call.ID == "" || call.InitiatorID == "" {
		slog.Warn("nats: skipping call push for malformed payload", "call_id", call.ID)
		return
	}

	// Push only to recipients NOT currently connected via WebSocket. Online
	// members already received the WS frame and will play the in-app ringtone.
	recipients := make([]string, 0, len(event.MemberIDs))
	skipped := 0
	for _, userID := range event.MemberIDs {
		if userID == "" || userID == event.SenderID || userID == call.InitiatorID {
			continue
		}
		if s.hub.IsOnline(userID) {
			skipped++
			continue
		}
		recipients = append(recipients, userID)
	}
	if len(recipients) == 0 {
		slog.Info("nats: call push skipped (all recipients online)", "call_id", call.ID, "online", skipped)
		return
	}

	payload, err := buildCallPushPayload(call)
	if err != nil {
		slog.Warn("nats: failed to marshal call push payload", "error", err, "call_id", call.ID)
		return
	}

	slog.Info("nats: dispatching call push", "call_id", call.ID, "recipients", len(recipients), "online_skipped", skipped)
	if err := s.pushDispatcher.SendCallToUsers(recipients, payload); err != nil {
		// fail-open per user — log and continue. The dispatcher already
		// reaped expired subscriptions internally.
		slog.Warn("nats: call push dispatch failed", "error", err, "call_id", call.ID)
	}
}

func buildCallPushPayload(call callPushData) ([]byte, error) {
	callType := call.Type
	if callType == "" {
		callType = "voice"
	}
	callMode := call.Mode
	if callMode == "" {
		callMode = "p2p"
	}

	payload := callPushPayload{
		Type:       "call_incoming",
		CallID:     call.ID,
		CallerID:   call.InitiatorID,
		CallerName: "", // resolved client-side from cached users (avoids extra fetch in hot path)
		CallType:   callType,
		CallMode:   callMode,
		ChatID:     call.ChatID,
		Timestamp:  time.Now().Unix(),
	}
	return json.Marshal(payload)
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

	if event.Event == EventUserDeactivated {
		userID := extractUserIDFromUserSubject(msg.Subject)
		if userID == "" {
			var data struct {
				UserID string `json:"user_id"`
			}
			if err := json.Unmarshal(event.Data, &data); err == nil {
				userID = data.UserID
			}
		}
		if userID != "" {
			s.hub.CloseUserConnections(userID)
		}
		return
	}

	// Route to specific members (messaging service provides member_ids for chat events).
	// Send to ALL members including the sender — the frontend handles dedup for own messages
	// via pendingSendUuids. Excluding the sender here would block delivery to their other
	// tabs/devices (SendToUsers excludes by userID, not by connection).
	if len(event.MemberIDs) > 0 {
		if isCallEvent(event.Event) {
			// Phase 6 debug: call events are rare enough that we can afford to
			// log every delivery until calls are proven stable in prod.
			slog.Info("nats: call event delivery", "event", event.Event,
				"subject", msg.Subject, "member_ids", event.MemberIDs,
				"sender_id", event.SenderID, "online_users", len(s.hub.OnlineUserIDs()))
		} else {
			slog.Info("nats: delivering to members", "event", event.Event,
				"member_count", len(event.MemberIDs), "online_users", len(s.hub.OnlineUserIDs()))
		}
		s.hub.SendToUsers(event.MemberIDs, envelope, "")
		// Stage 4: ring closed/backgrounded clients via web push for incoming calls.
		// Online members already received the WS frame above and will play the
		// in-app ringtone — push goes only to recipients absent from the hub.
		if event.Event == EventCallIncoming {
			s.enqueueCallPushDispatch(event)
		}
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

	// Call events: route directly to specified members
	if isCallEvent(event.Event) {
		if len(event.MemberIDs) > 0 {
			s.hub.SendToUsers(event.MemberIDs, envelope, "")
		}
		// For incoming calls, push to members who are NOT currently connected via WS
		// so closed/backgrounded clients still ring. Online members already received
		// the WS event above and the in-app ringtone will play there.
		if event.Event == EventCallIncoming {
			s.enqueueCallPushDispatch(event)
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

func isCallEvent(event string) bool {
	switch event {
	case EventCallIncoming,
		EventCallAccepted,
		EventCallDeclined,
		EventCallEnded,
		EventCallParticipantJoined,
		EventCallParticipantLeft,
		EventCallMuted,
		EventCallUnmuted,
		EventScreenShareStarted,
		EventScreenShareStopped:
		return true
	default:
		return false
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

func extractUserIDFromUserSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) >= 4 && parts[0] == "orbit" && parts[1] == "user" {
		if _, err := uuid.Parse(parts[2]); err != nil {
			slog.Warn("nats: invalid user ID in user subject, dropping", "subject", subject)
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
