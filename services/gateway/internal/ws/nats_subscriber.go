package ws

import (
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go"
)

// Subscriber listens for NATS events and routes them to WebSocket clients.
type Subscriber struct {
	hub  *Hub
	nc   *nats.Conn
	subs []*nats.Subscription
}

func NewSubscriber(hub *Hub, nc *nats.Conn) *Subscriber {
	return &Subscriber{hub: hub, nc: nc}
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

	// Route to specific members
	if len(event.MemberIDs) > 0 {
		s.hub.SendToUsers(event.MemberIDs, envelope, "")
		return
	}

	// For user-specific events (status), extract user_id from data and broadcast to their contacts
	if event.Event == EventUserStatus {
		var sd StatusData
		if err := json.Unmarshal(event.Data, &sd); err == nil {
			// Broadcast to all online users (they'll filter client-side based on contacts)
			for _, uid := range s.hub.OnlineUserIDs() {
				if uid != sd.UserID {
					s.hub.SendToUser(uid, envelope)
				}
			}
		}
	}
}
