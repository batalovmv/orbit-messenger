package ws

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
)

// Subscriber listens for NATS events and routes them to WebSocket clients.
type Subscriber struct {
	hub                 *Hub
	nc                  *nats.Conn
	subs                []*nats.Subscription
	messagingServiceURL string
	httpClient          *http.Client
}

func NewSubscriber(hub *Hub, nc *nats.Conn, messagingServiceURL string) *Subscriber {
	return &Subscriber{
		hub:                 hub,
		nc:                  nc,
		messagingServiceURL: messagingServiceURL,
		httpClient:          &http.Client{Timeout: 5 * time.Second},
	}
}

// Start subscribes to all relevant NATS subjects.
func (s *Subscriber) Start() error {
	subjects := []string{
		"orbit.chat.*.message.new",
		"orbit.chat.*.message.updated",
		"orbit.chat.*.message.deleted",
		"orbit.chat.*.messages.read",
		"orbit.chat.*.typing",
		"orbit.user.*.status",
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

	envelope := Envelope{
		Type: event.Event,
		Data: event.Data,
	}

	// Route to specific members (messaging service provides member_ids for chat events)
	if len(event.MemberIDs) > 0 {
		s.hub.SendToUsers(event.MemberIDs, envelope, event.SenderID)
		return
	}

	// For typing/stop_typing events, fetch chat member IDs and broadcast
	if event.Event == EventTyping || event.Event == EventStopTyping {
		var td TypingData
		if err := json.Unmarshal(event.Data, &td); err == nil {
			memberIDs := s.fetchChatMemberIDs(td.ChatID)
			// Extract user_id from data to exclude sender
			var userData struct {
				UserID string `json:"user_id"`
			}
			json.Unmarshal(event.Data, &userData)
			s.hub.SendToUsers(memberIDs, envelope, userData.UserID)
		}
		return
	}

	// For user status events, fetch contacts from messaging service
	// and only send to users who share a chat (not all online users)
	if event.Event == EventUserStatus {
		var sd StatusData
		if err := json.Unmarshal(event.Data, &sd); err == nil {
			contactIDs := s.fetchContactIDs(sd.UserID)
			for _, cid := range contactIDs {
				if cid != sd.UserID {
					s.hub.SendToUser(cid, envelope)
				}
			}
		}
	}
}

// fetchChatMemberIDs calls messaging service to get member IDs of a chat.
func (s *Subscriber) fetchChatMemberIDs(chatID string) []string {
	url := fmt.Sprintf("%s/chats/%s/member-ids", s.messagingServiceURL, chatID)
	resp, err := s.httpClient.Get(url)
	if err != nil {
		slog.Warn("failed to fetch chat member IDs", "chat_id", chatID, "error", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
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
	url := fmt.Sprintf("%s/users/%s/contacts", s.messagingServiceURL, userID)
	resp, err := s.httpClient.Get(url)
	if err != nil {
		slog.Warn("failed to fetch contact IDs", "user_id", userID, "error", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
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
