package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

const maxConcurrentFetches = 50 // Bound goroutines spawned for fallback member-ID fetches

// Subscriber listens for NATS events and routes them to WebSocket clients.
type Subscriber struct {
	hub                 *Hub
	nc                  *nats.Conn
	subs                []*nats.Subscription
	messagingServiceURL string
	internalSecret      string
	httpClient          *http.Client
	sem                 chan struct{} // semaphore to bound concurrent goroutines
}

func NewSubscriber(hub *Hub, nc *nats.Conn, messagingServiceURL, internalSecret string) *Subscriber {
	return &Subscriber{
		hub:                 hub,
		nc:                  nc,
		messagingServiceURL: messagingServiceURL,
		internalSecret:      internalSecret,
		httpClient:          &http.Client{Timeout: 5 * time.Second},
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

	// Fallback: if member_ids is empty for a message event, extract chat_id from subject
	// and fetch member IDs from messaging service. Subject format: orbit.chat.<chatID>.message.*
	if event.Event == EventNewMessage || event.Event == EventMessageUpdated ||
		event.Event == EventMessageDeleted || event.Event == EventMessagesRead ||
		event.Event == EventMessagePinned || event.Event == EventMessageUnpinned {
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

	// For mention events, send to the mentioned user
	if event.Event == "mention" {
		if len(event.MemberIDs) > 0 {
			for _, uid := range event.MemberIDs {
				s.hub.SendToUser(uid, envelope)
			}
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

// extractUserIDFromMediaSubject parses user ID from NATS subject like "orbit.media.<userId>.ready"
func extractUserIDFromMediaSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) >= 3 && parts[0] == "orbit" && parts[1] == "media" {
		return parts[2]
	}
	return ""
}

// extractChatIDFromSubject parses chat ID from NATS subject like "orbit.chat.<uuid>.message.new"
func extractChatIDFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) >= 3 && parts[0] == "orbit" && parts[1] == "chat" {
		return parts[2]
	}
	return ""
}

// fetchChatMemberIDs calls messaging service to get member IDs of a chat.
func (s *Subscriber) fetchChatMemberIDs(chatID string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/chats/%s/member-ids", s.messagingServiceURL, chatID)
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
