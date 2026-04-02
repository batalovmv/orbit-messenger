package ws

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

func TestSubscriber_HandleEvent_RichMessageEventsDeliverWithMemberIDs(t *testing.T) {
	tests := []struct {
		name    string
		event   string
		payload string
	}{
		{
			name:    "reaction added",
			event:   EventReactionAdded,
			payload: fmt.Sprintf(`{"message_id":"%s","user_id":"%s","emoji":"🔥"}`, uuid.New(), uuid.New()),
		},
		{
			name:    "reaction removed",
			event:   EventReactionRemoved,
			payload: fmt.Sprintf(`{"message_id":"%s","user_id":"%s","emoji":"👍"}`, uuid.New(), uuid.New()),
		},
		{
			name:  "poll vote",
			event: EventPollVote,
			payload: fmt.Sprintf(
				`{"id":"%s","message_id":"%s","question":"Lunch?","is_closed":false,"options":[{"id":"%s","text":"Pizza","position":0,"voters":3}],"total_voters":3}`,
				uuid.New(),
				uuid.New(),
				uuid.New(),
			),
		},
		{
			name:  "poll closed",
			event: EventPollClosed,
			payload: fmt.Sprintf(
				`{"id":"%s","message_id":"%s","question":"Standup time?","is_closed":true,"options":[{"id":"%s","text":"10:00","position":0,"voters":5}],"total_voters":5}`,
				uuid.New(),
				uuid.New(),
				uuid.New(),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatID := uuid.New().String()
			userOne := uuid.New().String()
			userTwo := uuid.New().String()
			outsider := uuid.New().String()

			hub := NewHub()
			userOneDeliveries := make(chan Envelope, 1)
			userTwoDeliveries := make(chan Envelope, 1)
			outsiderDeliveries := make(chan Envelope, 1)

			hub.Register(newCapturingConn(userOne, userOneDeliveries))
			hub.Register(newCapturingConn(userTwo, userTwoDeliveries))
			hub.Register(newCapturingConn(outsider, outsiderDeliveries))

			subscriber := NewSubscriber(hub, nil, "", "")

			subject := fmt.Sprintf("orbit.chat.%s.message.updated", chatID)
			payload := marshalTestNATSEvent(t, NATSEvent{
				Event:     tt.event,
				Data:      json.RawMessage(tt.payload),
				MemberIDs: []string{userOne, userTwo},
				SenderID:  userOne,
				Timestamp: time.Now().Format(time.RFC3339),
			})

			subscriber.handleEvent(&nats.Msg{Subject: subject, Data: payload})

			assertEnvelope(t, waitForEnvelope(t, userOneDeliveries), tt.event, tt.payload)
			assertEnvelope(t, waitForEnvelope(t, userTwoDeliveries), tt.event, tt.payload)
			assertNoEnvelope(t, outsiderDeliveries)
		})
	}
}

func TestSubscriber_HandleEvent_RichMessageEventsFallbackToMemberFetch(t *testing.T) {
	tests := []struct {
		name    string
		event   string
		payload string
	}{
		{
			name:    "reaction added",
			event:   EventReactionAdded,
			payload: fmt.Sprintf(`{"message_id":"%s","user_id":"%s","emoji":"🔥"}`, uuid.New(), uuid.New()),
		},
		{
			name:    "reaction removed",
			event:   EventReactionRemoved,
			payload: fmt.Sprintf(`{"message_id":"%s","user_id":"%s","emoji":"🎉"}`, uuid.New(), uuid.New()),
		},
		{
			name:  "poll vote",
			event: EventPollVote,
			payload: fmt.Sprintf(
				`{"id":"%s","message_id":"%s","question":"Deploy now?","is_closed":false,"options":[{"id":"%s","text":"Yes","position":0,"voters":4}],"total_voters":4}`,
				uuid.New(),
				uuid.New(),
				uuid.New(),
			),
		},
		{
			name:  "poll closed",
			event: EventPollClosed,
			payload: fmt.Sprintf(
				`{"id":"%s","message_id":"%s","question":"Office day?","is_closed":true,"options":[{"id":"%s","text":"Friday","position":0,"voters":6}],"total_voters":6}`,
				uuid.New(),
				uuid.New(),
				uuid.New(),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatID := uuid.New().String()
			userOne := uuid.New().String()
			userTwo := uuid.New().String()
			outsider := uuid.New().String()

			hub := NewHub()
			userOneDeliveries := make(chan Envelope, 1)
			userTwoDeliveries := make(chan Envelope, 1)
			outsiderDeliveries := make(chan Envelope, 1)

			hub.Register(newCapturingConn(userOne, userOneDeliveries))
			hub.Register(newCapturingConn(userTwo, userTwoDeliveries))
			hub.Register(newCapturingConn(outsider, outsiderDeliveries))

			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)

				wantPath := fmt.Sprintf("/chats/%s/member-ids", chatID)
				if r.URL.Path != wantPath {
					t.Errorf("unexpected request path: want %q, got %q", wantPath, r.URL.Path)
				}
				if got := r.Header.Get("X-Internal-Token"); got != "internal-secret" {
					t.Errorf("unexpected internal token header: want %q, got %q", "internal-secret", got)
				}
				if got := r.Header.Get("X-User-ID"); got != "00000000-0000-0000-0000-000000000000" {
					t.Errorf("unexpected X-User-ID header: got %q", got)
				}

				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"member_ids":["%s","%s"]}`, userOne, userTwo)
			}))
			defer server.Close()

			subscriber := NewSubscriber(hub, nil, server.URL, "internal-secret")
			subscriber.httpClient = server.Client()

			subject := fmt.Sprintf("orbit.chat.%s.message.updated", chatID)
			payload := marshalTestNATSEvent(t, NATSEvent{
				Event:     tt.event,
				Data:      json.RawMessage(tt.payload),
				SenderID:  userOne,
				Timestamp: time.Now().Format(time.RFC3339),
			})

			subscriber.handleEvent(&nats.Msg{Subject: subject, Data: payload})

			assertEnvelope(t, waitForEnvelope(t, userOneDeliveries), tt.event, tt.payload)
			assertEnvelope(t, waitForEnvelope(t, userTwoDeliveries), tt.event, tt.payload)
			assertNoEnvelope(t, outsiderDeliveries)

			if got := requests.Load(); got != 1 {
				t.Fatalf("expected exactly 1 member fetch request, got %d", got)
			}
		})
	}
}

func newCapturingConn(userID string, deliveries chan Envelope) *Conn {
	return &Conn{
		UserID: userID,
		done:   make(chan struct{}),
		sendFn: func(msg interface{}) error {
			switch value := msg.(type) {
			case Envelope:
				deliveries <- value
			case *Envelope:
				deliveries <- *value
			default:
				return fmt.Errorf("unexpected message type %T", msg)
			}
			return nil
		},
	}
}

func marshalTestNATSEvent(t *testing.T, event NATSEvent) []byte {
	t.Helper()

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal NATS event: %v", err)
	}

	return payload
}

func waitForEnvelope(t *testing.T, deliveries <-chan Envelope) Envelope {
	t.Helper()

	select {
	case envelope := <-deliveries:
		return envelope
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for envelope delivery")
		return Envelope{}
	}
}

func assertNoEnvelope(t *testing.T, deliveries <-chan Envelope) {
	t.Helper()

	select {
	case envelope := <-deliveries:
		t.Fatalf("unexpected envelope delivered: %+v", envelope)
	case <-time.After(100 * time.Millisecond):
	}
}

func assertEnvelope(t *testing.T, got Envelope, wantType, wantPayload string) {
	t.Helper()

	if got.Type != wantType {
		t.Fatalf("unexpected envelope type: want %q, got %q", wantType, got.Type)
	}

	if !jsonEqual(got.Data, []byte(wantPayload)) {
		t.Fatalf("unexpected envelope payload: want %s, got %s", wantPayload, string(got.Data))
	}
}

func jsonEqual(left, right []byte) bool {
	var leftValue any
	var rightValue any

	if err := json.Unmarshal(left, &leftValue); err != nil {
		return false
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return false
	}

	return reflect.DeepEqual(leftValue, rightValue)
}
