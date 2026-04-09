package ws

import (
	"bytes"
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

type mockPushSender struct {
	sendToUsersFn     func(userIDs []string, payload []byte) error
	sendCallToUsersFn func(userIDs []string, payload []byte) error
}

func (m *mockPushSender) SendToUsers(userIDs []string, payload []byte) error {
	if m.sendToUsersFn != nil {
		return m.sendToUsersFn(userIDs, payload)
	}
	return nil
}

func (m *mockPushSender) SendCallToUsers(userIDs []string, payload []byte) error {
	if m.sendCallToUsersFn != nil {
		return m.sendCallToUsersFn(userIDs, payload)
	}
	return nil
}

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

				wantPath := fmt.Sprintf("/internal/chats/%s/member-ids", chatID)
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

func TestSubscriber_HandleEvent_NewMessageDispatchesPushToOfflineUnmutedUsers(t *testing.T) {
	senderID := uuid.New().String()
	onlineRecipient := uuid.New().String()
	offlineRecipient := uuid.New().String()
	mutedRecipient := uuid.New().String()
	chatID := uuid.New().String()
	messageID := uuid.New().String()
	sequenceNumber := int64(4242)

	var pushedUserIDs []string
	var pushedPayload []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/notification-settings/muted-users" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.Header.Get("X-Internal-Token"); got != "internal-secret" {
			t.Fatalf("unexpected internal token: %s", got)
		}

		var body struct {
			ChatID  string   `json:"chat_id"`
			UserIDs []string `json:"user_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.ChatID != chatID {
			t.Fatalf("unexpected chat id: %s", body.ChatID)
		}
		if !reflect.DeepEqual(body.UserIDs, []string{onlineRecipient, offlineRecipient, mutedRecipient}) {
			t.Fatalf("unexpected recipient user ids: %+v", body.UserIDs)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"muted_user_ids":["%s"]}`, mutedRecipient)
	}))
	defer server.Close()

	hub := NewHub()
	deliveries := make(chan Envelope, 1)
	hub.Register(newCapturingConn(onlineRecipient, deliveries))

	pushSender := &mockPushSender{
		sendToUsersFn: func(userIDs []string, payload []byte) error {
			pushedUserIDs = append([]string(nil), userIDs...)
			pushedPayload = append([]byte(nil), payload...)
			return nil
		},
	}

	subscriber := NewSubscriber(hub, nil, server.URL, "internal-secret", pushSender)
	subscriber.httpClient = server.Client()

	payload := marshalTestNATSEvent(t, NATSEvent{
		Event: EventNewMessage,
		Data: json.RawMessage(fmt.Sprintf(
			`{"id":"%s","chat_id":"%s","type":"text","content":"%s","sender_name":"%s","sequence_number":%d}`,
			messageID,
			chatID,
			"Привет, команда",
			"Алиса",
			sequenceNumber,
		)),
		MemberIDs: []string{senderID, onlineRecipient, offlineRecipient, mutedRecipient},
		SenderID:  senderID,
		Timestamp: time.Now().Format(time.RFC3339),
	})

	subscriber.handleEvent(&nats.Msg{Subject: fmt.Sprintf("orbit.chat.%s.message.new", chatID), Data: payload})

	assertEnvelope(
		t,
		waitForEnvelope(t, deliveries),
		EventNewMessage,
		fmt.Sprintf(
			`{"id":"%s","chat_id":"%s","type":"text","content":"%s","sender_name":"%s","sequence_number":%d}`,
			messageID,
			chatID,
			"Привет, команда",
			"Алиса",
			sequenceNumber,
		),
	)

	waitForCondition(t, func() bool { return len(pushedUserIDs) == 2 }, "push dispatch")

	if !reflect.DeepEqual(pushedUserIDs, []string{onlineRecipient, offlineRecipient}) {
		t.Fatalf("unexpected push recipients: %+v", pushedUserIDs)
	}

	var pushBody struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Data  struct {
			ChatID               string `json:"chat_id"`
			MessageID            int64  `json:"message_id"`
			Type                 string `json:"type"`
			ShouldReplaceHistory bool   `json:"should_replace_history"`
		} `json:"data"`
	}
	if err := json.NewDecoder(bytes.NewReader(pushedPayload)).Decode(&pushBody); err != nil {
		t.Fatalf("decode push payload: %v", err)
	}
	if pushBody.Title != "Алиса" || pushBody.Body != "Привет, команда" {
		t.Fatalf("unexpected push payload: %+v", pushBody)
	}
	if pushBody.Data.ChatID != chatID || pushBody.Data.MessageID != sequenceNumber || pushBody.Data.Type != "new_message" {
		t.Fatalf("unexpected push data: %+v", pushBody.Data)
	}
	if !pushBody.Data.ShouldReplaceHistory {
		t.Fatalf("expected should_replace_history to be true")
	}
}

func TestSubscriber_HandleEvent_NewMessageSkipsPushWhenMuteLookupFails(t *testing.T) {
	senderID := uuid.New().String()
	offlineRecipient := uuid.New().String()
	chatID := uuid.New().String()
	pushCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	subscriber := NewSubscriber(
		NewHub(),
		nil,
		server.URL,
		"internal-secret",
		&mockPushSender{
			sendToUsersFn: func(userIDs []string, payload []byte) error {
				pushCalls++
				return nil
			},
		},
	)
	subscriber.httpClient = server.Client()

	payload := marshalTestNATSEvent(t, NATSEvent{
		Event: EventNewMessage,
		Data: json.RawMessage(fmt.Sprintf(
			`{"id":"%s","chat_id":"%s","type":"text","content":"%s","sender_name":"%s","sequence_number":%d}`,
			uuid.New(),
			chatID,
			"Сообщение",
			"Боб",
			101,
		)),
		MemberIDs: []string{senderID, offlineRecipient},
		SenderID:  senderID,
		Timestamp: time.Now().Format(time.RFC3339),
	})

	subscriber.handleEvent(&nats.Msg{Subject: fmt.Sprintf("orbit.chat.%s.message.new", chatID), Data: payload})

	time.Sleep(150 * time.Millisecond)
	if pushCalls != 0 {
		t.Fatalf("expected no push calls when mute lookup fails, got %d", pushCalls)
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

func newClosableConn(userID string, closes chan string) *Conn {
	return &Conn{
		UserID: userID,
		done:   make(chan struct{}),
		closeFn: func(code int, text string) error {
			closes <- fmt.Sprintf("%d:%s", code, text)
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

func waitForCondition(t *testing.T, check func() bool, label string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", label)
}

func TestSubscriber_HandleEvent_CallIncomingPushesOnlyToOfflineRecipients(t *testing.T) {
	initiatorID := uuid.New().String()
	onlineCallee := uuid.New().String()
	offlineCalleeOne := uuid.New().String()
	offlineCalleeTwo := uuid.New().String()
	chatID := uuid.New().String()
	callID := uuid.New().String()

	hub := NewHub()
	deliveries := make(chan Envelope, 1)
	hub.Register(newCapturingConn(onlineCallee, deliveries))

	var pushedRecipients []string
	var pushedPayload []byte
	pushSender := &mockPushSender{
		sendCallToUsersFn: func(userIDs []string, payload []byte) error {
			pushedRecipients = append([]string(nil), userIDs...)
			pushedPayload = append([]byte(nil), payload...)
			return nil
		},
		sendToUsersFn: func(userIDs []string, payload []byte) error {
			t.Fatalf("SendToUsers must not be called for call_incoming, got %+v", userIDs)
			return nil
		},
	}

	subscriber := NewSubscriber(hub, nil, "", "internal-secret", pushSender)

	payload := marshalTestNATSEvent(t, NATSEvent{
		Event: EventCallIncoming,
		Data: json.RawMessage(fmt.Sprintf(
			`{"id":"%s","type":"video","mode":"group","chat_id":"%s","initiator_id":"%s","status":"ringing"}`,
			callID, chatID, initiatorID,
		)),
		MemberIDs: []string{initiatorID, onlineCallee, offlineCalleeOne, offlineCalleeTwo},
		SenderID:  initiatorID,
		Timestamp: time.Now().Format(time.RFC3339),
	})

	subscriber.handleEvent(&nats.Msg{Subject: "orbit.call." + callID + ".lifecycle", Data: payload})

	// Online callee still gets the WS frame so the in-app ringtone plays.
	gotEnvelope := waitForEnvelope(t, deliveries)
	if gotEnvelope.Type != EventCallIncoming {
		t.Fatalf("expected call_incoming envelope on online callee, got %q", gotEnvelope.Type)
	}

	waitForCondition(t, func() bool { return len(pushedRecipients) > 0 }, "call push dispatch")

	wantRecipients := map[string]bool{offlineCalleeOne: true, offlineCalleeTwo: true}
	if len(pushedRecipients) != 2 {
		t.Fatalf("expected 2 offline push recipients, got %+v", pushedRecipients)
	}
	for _, uid := range pushedRecipients {
		if !wantRecipients[uid] {
			t.Fatalf("unexpected push recipient %s (initiator/online must be skipped)", uid)
		}
	}

	var body callPushPayload
	if err := json.Unmarshal(pushedPayload, &body); err != nil {
		t.Fatalf("decode call push payload: %v", err)
	}
	if body.Type != "call_incoming" || body.CallID != callID || body.CallerID != initiatorID {
		t.Fatalf("unexpected call push payload: %+v", body)
	}
	if body.CallType != "video" || body.CallMode != "group" || body.ChatID != chatID {
		t.Fatalf("unexpected call push payload fields: %+v", body)
	}
}

func TestSubscriber_HandleEvent_UserDeactivatedClosesOnlyMatchingConnections(t *testing.T) {
	targetUserID := uuid.New().String()
	otherUserID := uuid.New().String()

	targetCloseOne := make(chan string, 1)
	targetCloseTwo := make(chan string, 1)
	otherClose := make(chan string, 1)

	hub := NewHub()
	hub.Register(newClosableConn(targetUserID, targetCloseOne))
	hub.Register(newClosableConn(targetUserID, targetCloseTwo))
	hub.Register(newClosableConn(otherUserID, otherClose))

	subscriber := NewSubscriber(hub, nil, "", "")

	payload := marshalTestNATSEvent(t, NATSEvent{
		Event:     EventUserDeactivated,
		Data:      json.RawMessage(fmt.Sprintf(`{"user_id":"%s"}`, targetUserID)),
		Timestamp: time.Now().Format(time.RFC3339),
	})

	subscriber.handleEvent(&nats.Msg{
		Subject: fmt.Sprintf("orbit.user.%s.deactivated", targetUserID),
		Data:    payload,
	})

	wantClose := fmt.Sprintf("%d:%s", closeCodePolicyViolation, "account deactivated")
	for i, closes := range []<-chan string{targetCloseOne, targetCloseTwo} {
		select {
		case got := <-closes:
			if got != wantClose {
				t.Fatalf("target close %d mismatch: want %q, got %q", i, wantClose, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for close on target connection %d", i)
		}
	}

	select {
	case got := <-otherClose:
		t.Fatalf("unexpected close for other user: %q", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSubscriber_HandleEvent_CallIncomingSkipsPushWhenAllRecipientsOnline(t *testing.T) {
	initiatorID := uuid.New().String()
	calleeID := uuid.New().String()
	chatID := uuid.New().String()
	callID := uuid.New().String()

	hub := NewHub()
	deliveries := make(chan Envelope, 1)
	hub.Register(newCapturingConn(calleeID, deliveries))

	var pushCalls int32
	pushSender := &mockPushSender{
		sendCallToUsersFn: func(userIDs []string, payload []byte) error {
			atomic.AddInt32(&pushCalls, 1)
			return nil
		},
	}

	subscriber := NewSubscriber(hub, nil, "", "internal-secret", pushSender)

	payload := marshalTestNATSEvent(t, NATSEvent{
		Event: EventCallIncoming,
		Data: json.RawMessage(fmt.Sprintf(
			`{"id":"%s","type":"voice","mode":"p2p","chat_id":"%s","initiator_id":"%s","status":"ringing"}`,
			callID, chatID, initiatorID,
		)),
		MemberIDs: []string{initiatorID, calleeID},
		SenderID:  initiatorID,
		Timestamp: time.Now().Format(time.RFC3339),
	})

	subscriber.handleEvent(&nats.Msg{Subject: "orbit.call." + callID + ".lifecycle", Data: payload})

	if got := waitForEnvelope(t, deliveries); got.Type != EventCallIncoming {
		t.Fatalf("expected call_incoming envelope on online callee, got %q", got.Type)
	}

	time.Sleep(150 * time.Millisecond)
	if n := atomic.LoadInt32(&pushCalls); n != 0 {
		t.Fatalf("expected zero push dispatches when all recipients online, got %d", n)
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
