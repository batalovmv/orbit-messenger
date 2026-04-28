// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

type mockPushSender struct {
	sendToUsersFn             func(userIDs []string, payload []byte) error
	sendCallToUsersFn         func(userIDs []string, payload []byte) error
	sendToUsersWithPriorityFn func(userIDs []string, payload []byte, priority string) error
	sendReadSyncToUserFn      func(userID string, payload []byte) error
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

func (m *mockPushSender) SendToUsersWithPriority(userIDs []string, payload []byte, priority string) error {
	if m.sendToUsersWithPriorityFn != nil {
		return m.sendToUsersWithPriorityFn(userIDs, payload, priority)
	}
	return m.SendToUsers(userIDs, payload)
}

func (m *mockPushSender) SendReadSyncToUser(userID string, payload []byte) error {
	if m.sendReadSyncToUserFn != nil {
		return m.sendReadSyncToUserFn(userID, payload)
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

			subscriber := NewSubscriber(hub, nil, "", "", nil)

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

			subscriber := NewSubscriber(hub, nil, server.URL, "internal-secret", nil)
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

	subscriber := NewSubscriber(hub, nil, server.URL, "internal-secret", nil, pushSender)
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
		nil,
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

	subscriber := NewSubscriber(hub, nil, "", "internal-secret", nil, pushSender)

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

	subscriber := NewSubscriber(hub, nil, "", "", nil)

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

	subscriber := NewSubscriber(hub, nil, "", "internal-secret", nil, pushSender)

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

func TestSubscriber_HandleJSEvent_DedupSkipsDuplicateEventID(t *testing.T) {
	userID := uuid.New().String()
	chatID := uuid.New().String()
	eventID := uuid.New().String()

	hub := NewHub()
	deliveries := make(chan Envelope, 2) // buffer 2 to catch unexpected duplicates
	hub.Register(newCapturingConn(userID, deliveries))

	subscriber := NewSubscriber(hub, nil, "", "", nil)

	msgPayload := marshalTestNATSEvent(t, NATSEvent{
		Event:     EventMessageUpdated,
		Data:      json.RawMessage(`{"id":"abc"}`),
		MemberIDs: []string{userID},
		Timestamp: time.Now().Format(time.RFC3339),
	})

	makeMsg := func() *nats.Msg {
		msg := &nats.Msg{
			Subject: fmt.Sprintf("orbit.chat.%s.message.updated", chatID),
			Data:    msgPayload,
			Header:  nats.Header{},
		}
		msg.Header.Set("Nats-Msg-Id", eventID)
		return msg
	}

	// First delivery — should fan out.
	subscriber.handleJSEvent(makeMsg())

	select {
	case env := <-deliveries:
		if env.Type != EventMessageUpdated {
			t.Fatalf("expected message_updated, got %q", env.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first delivery")
	}

	// Second delivery of the same event ID — must be deduped, no fanout.
	subscriber.handleJSEvent(makeMsg())

	select {
	case env := <-deliveries:
		t.Fatalf("expected no second delivery, got %q", env.Type)
	case <-time.After(100 * time.Millisecond):
		// correct: no duplicate delivered
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

func TestFetchOffModeUserIDs(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	userOff := uuid.New().String()
	userSmart := uuid.New().String()
	userMissing := uuid.New().String()

	mr.Set("user_priority_mode:"+userOff, "off")
	mr.Set("user_priority_mode:"+userSmart, "smart")

	sub := &Subscriber{rdb: rdb}
	result := sub.fetchOffModeUserIDs(context.Background(), []string{userOff, userSmart, userMissing})

	if _, ok := result[userOff]; !ok {
		t.Errorf("expected userOff to be in off-mode set")
	}
	if _, ok := result[userSmart]; ok {
		t.Errorf("expected userSmart NOT to be in off-mode set")
	}
	if _, ok := result[userMissing]; ok {
		t.Errorf("expected userMissing (cache miss) NOT to be in off-mode set")
	}
}

func TestFetchOffModeUserIDs_NilRedis(t *testing.T) {
	sub := &Subscriber{rdb: nil}
	result := sub.fetchOffModeUserIDs(context.Background(), []string{"user1"})
	if len(result) != 0 {
		t.Errorf("expected empty set when rdb is nil, got %d", len(result))
	}
}

func TestDispatchPushNotifications_SkipsOffModeUsers(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	senderID := uuid.New().String()
	userOff := uuid.New().String()
	userNormal := uuid.New().String()

	mr.Set("user_priority_mode:"+userOff, "off")

	// Mock messaging service for muted users (returns none muted)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"muted_user_ids":[]}`)
	}))
	defer server.Close()

	var pushedUsers []string
	pushSender := &mockPushSender{
		sendToUsersWithPriorityFn: func(userIDs []string, payload []byte, priority string) error {
			pushedUsers = append(pushedUsers, userIDs...)
			return nil
		},
	}

	sub := NewSubscriber(NewHub(), nil, server.URL, "internal-secret", rdb, pushSender)
	sub.httpClient = server.Client()

	msgData, _ := json.Marshal(pushMessageData{
		ID:             uuid.New().String(),
		ChatID:         uuid.New().String(),
		Content:        strPtr("hello"),
		SenderName:     "Test",
		SequenceNumber: 1,
	})

	sub.dispatchPushNotifications(NATSEvent{
		Event:    EventNewMessage,
		Data:     msgData,
		SenderID: senderID,
	}, []string{senderID, userOff, userNormal})

	if len(pushedUsers) != 1 || pushedUsers[0] != userNormal {
		t.Errorf("expected push only to userNormal, got %v", pushedUsers)
	}
}

func strPtr(s string) *string { return &s }

// ---------------------------------------------------------------------------
// Day 4b: read_sync silent push fallback for offline users
// ---------------------------------------------------------------------------

// readSyncEnvelope marshals the standard NATSEvent for orbit.user.<uid>.read_sync.
func readSyncEnvelope(t *testing.T, userID, chatID string, unread int64, originSession string) (string, []byte) {
	t.Helper()
	subject := "orbit.user." + userID + ".read_sync"
	payload := marshalTestNATSEvent(t, NATSEvent{
		Event: EventReadSync,
		Data: json.RawMessage(fmt.Sprintf(
			`{"chat_id":%q,"last_read_message_id":%q,"last_read_seq_num":42,"unread_count":%d,"read_at":"2026-04-28T10:30:00Z","origin_session_id":%q}`,
			chatID, uuid.New().String(), unread, originSession,
		)),
		MemberIDs: []string{userID},
		SenderID:  userID,
		Timestamp: time.Now().Format(time.RFC3339),
	})
	return subject, payload
}

// withFastReadSyncDebounce drops the coalescer's debounce so tests don't need
// to wait the production 1.5s to observe a flush.
func withFastReadSyncDebounce(t *testing.T, sub *Subscriber, debounce time.Duration) {
	t.Helper()
	if sub.readSyncCoalescer != nil {
		sub.readSyncCoalescer.Stop()
	}
	sub.readSyncCoalescer = newReadSyncCoalescer(debounce, sub.flushReadSyncPush)
	t.Cleanup(func() {
		if sub.readSyncCoalescer != nil {
			sub.readSyncCoalescer.Stop()
		}
	})
}

// TestSubscriber_HandleReadSync_OtherDevicesOnlineSkipsPush asserts that
// when the user has at least one active WS connection BESIDES the origin,
// the silent push is suppressed — those non-origin devices already received
// the WS frame, pushing on top would just burn iOS APNs budget.
func TestSubscriber_HandleReadSync_OtherDevicesOnlineSkipsPush(t *testing.T) {
	userID := uuid.New().String()
	chatID := uuid.New().String()
	const originSession = "tab-A"
	const otherSession = "tab-B"

	hub := NewHub()
	// Origin session — would have been excluded from WS fanout.
	hub.Register(&Conn{UserID: userID, SessionID: originSession, done: make(chan struct{})})
	// A second connection that DID receive the WS frame.
	hub.Register(&Conn{UserID: userID, SessionID: otherSession, done: make(chan struct{}),
		sendFn: func(interface{}) error { return nil }})

	var pushCalls atomic.Int32
	push := &mockPushSender{
		sendReadSyncToUserFn: func(_ string, _ []byte) error {
			pushCalls.Add(1)
			return nil
		},
	}
	sub := NewSubscriber(hub, nil, "", "", nil, push)
	withFastReadSyncDebounce(t, sub, 20*time.Millisecond)

	subject, payload := readSyncEnvelope(t, userID, chatID, 0, originSession)
	sub.handleEvent(&nats.Msg{Subject: subject, Data: payload})

	time.Sleep(80 * time.Millisecond)
	if got := pushCalls.Load(); got != 0 {
		t.Fatalf("non-origin device online → push must be skipped, got %d calls", got)
	}
}

// TestSubscriber_HandleReadSync_OnlyOriginOnlineSendsPush is the bug-fix
// test from PR #14 review. When the user's ONLY active connection is the
// originating session (e.g. desktop tab open, iPhone PWA in background),
// the WS fanout reaches zero recipients (origin excluded) — but the user
// has stale notifications on the offline phone. The previous IsOnline
// gate suppressed the push in this case; CountConnectionsExcluding fixes it.
func TestSubscriber_HandleReadSync_OnlyOriginOnlineSendsPush(t *testing.T) {
	userID := uuid.New().String()
	chatID := uuid.New().String()
	const originSession = "tab-A"

	hub := NewHub()
	hub.Register(&Conn{UserID: userID, SessionID: originSession, done: make(chan struct{})})

	var pushCalls atomic.Int32
	push := &mockPushSender{
		sendReadSyncToUserFn: func(_ string, _ []byte) error {
			pushCalls.Add(1)
			return nil
		},
	}
	sub := NewSubscriber(hub, nil, "", "", nil, push)
	withFastReadSyncDebounce(t, sub, 20*time.Millisecond)

	subject, payload := readSyncEnvelope(t, userID, chatID, 0, originSession)
	sub.handleEvent(&nats.Msg{Subject: subject, Data: payload})

	// Generous wait — push goes through s.sem goroutine, not the timer fiber.
	deadline := time.After(time.Second)
	for {
		if pushCalls.Load() == 1 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("expected exactly 1 push (only-origin-online), got %d", pushCalls.Load())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// TestSubscriber_HandleReadSync_PartialReadSkipsPush asserts the unread_count
// gate: partial reads (unread_count != 0) do NOT trigger a push even when
// offline. The user-visible signal "chat went red→empty" only fires on full
// clear, so partial reads stay silent to preserve APNs budget.
func TestSubscriber_HandleReadSync_PartialReadSkipsPush(t *testing.T) {
	userID := uuid.New().String()
	chatID := uuid.New().String()

	hub := NewHub() // user offline — no Register call

	var pushCalls atomic.Int32
	push := &mockPushSender{
		sendReadSyncToUserFn: func(_ string, _ []byte) error {
			pushCalls.Add(1)
			return nil
		},
	}
	sub := NewSubscriber(hub, nil, "", "", nil, push)
	withFastReadSyncDebounce(t, sub, 20*time.Millisecond)

	// unread_count=3 — partial read, must not push
	subject, payload := readSyncEnvelope(t, userID, chatID, 3, "")
	sub.handleEvent(&nats.Msg{Subject: subject, Data: payload})

	time.Sleep(80 * time.Millisecond)
	if got := pushCalls.Load(); got != 0 {
		t.Fatalf("partial read must not push (unread_count > 0), got %d calls", got)
	}
}

// TestSubscriber_HandleReadSync_OfflineFullClearSendsPush is the happy-path
// for the fallback: user offline + unread cleared to 0 → exactly one silent
// push to that user with the read_sync payload shape.
func TestSubscriber_HandleReadSync_OfflineFullClearSendsPush(t *testing.T) {
	userID := uuid.New().String()
	chatID := uuid.New().String()

	hub := NewHub() // offline

	type call struct {
		userID  string
		payload []byte
	}
	calls := make(chan call, 4)
	push := &mockPushSender{
		sendReadSyncToUserFn: func(uid string, payload []byte) error {
			calls <- call{userID: uid, payload: append([]byte(nil), payload...)}
			return nil
		},
	}
	sub := NewSubscriber(hub, nil, "", "", nil, push)
	withFastReadSyncDebounce(t, sub, 20*time.Millisecond)

	subject, payload := readSyncEnvelope(t, userID, chatID, 0, "tab-A")
	sub.handleEvent(&nats.Msg{Subject: subject, Data: payload})

	select {
	case got := <-calls:
		if got.userID != userID {
			t.Fatalf("push targeted wrong user: got %s want %s", got.userID, userID)
		}
		var parsed readSyncPushPayload
		if err := json.Unmarshal(got.payload, &parsed); err != nil {
			t.Fatalf("parse push payload: %v", err)
		}
		if parsed.Type != "read_sync" {
			t.Errorf("payload.type: got %q want read_sync", parsed.Type)
		}
		if parsed.ChatID != chatID {
			t.Errorf("payload.chat_id: got %q want %s", parsed.ChatID, chatID)
		}
		if parsed.LastReadSeqNum != 42 {
			t.Errorf("payload.last_read_seq_num: got %d want 42", parsed.LastReadSeqNum)
		}
	case <-time.After(time.Second):
		t.Fatal("expected exactly one silent push, got none")
	}

	// Verify nothing else fires later (single-flush invariant).
	select {
	case extra := <-calls:
		t.Fatalf("unexpected second push: %+v", extra)
	case <-time.After(80 * time.Millisecond):
	}
}

// TestSubscriber_HandleReadSync_BurstCoalescesPerChat asserts that 5 rapid
// read_syncs for the SAME (user, chat) collapse to 1 push. This is the iOS
// budget protection — without it, scrolling fast through unread chats would
// burn through the APNs budget in seconds.
func TestSubscriber_HandleReadSync_BurstCoalescesPerChat(t *testing.T) {
	userID := uuid.New().String()
	chatID := uuid.New().String()

	hub := NewHub()

	var pushCalls atomic.Int32
	push := &mockPushSender{
		sendReadSyncToUserFn: func(_ string, _ []byte) error {
			pushCalls.Add(1)
			return nil
		},
	}
	sub := NewSubscriber(hub, nil, "", "", nil, push)
	withFastReadSyncDebounce(t, sub, 60*time.Millisecond)

	for i := 0; i < 5; i++ {
		subject, payload := readSyncEnvelope(t, userID, chatID, 0, "")
		sub.handleEvent(&nats.Msg{Subject: subject, Data: payload})
		time.Sleep(15 * time.Millisecond)
	}

	time.Sleep(150 * time.Millisecond)

	if got := pushCalls.Load(); got != 1 {
		t.Fatalf("5 rapid submits for the same key must coalesce to 1 push, got %d", got)
	}
}

// TestSubscriber_HandleReadSync_NilDispatcherSafe is a smoke test for the
// "push not configured" path (e.g. dev environments without VAPID keys):
// the handler must still broadcast the WS frame and not panic when
// pushDispatcher is nil.
func TestSubscriber_HandleReadSync_NilDispatcherSafe(t *testing.T) {
	userID := uuid.New().String()
	chatID := uuid.New().String()

	hub := NewHub()
	deliveries := make(chan Envelope, 1)
	hub.Register(newCapturingConn(userID, deliveries))

	sub := NewSubscriber(hub, nil, "", "", nil) // no pushDispatcher
	withFastReadSyncDebounce(t, sub, 20*time.Millisecond)

	subject, payload := readSyncEnvelope(t, userID, chatID, 0, "")
	sub.handleEvent(&nats.Msg{Subject: subject, Data: payload})

	// WS broadcast must still happen.
	got := waitForEnvelope(t, deliveries)
	if got.Type != EventReadSync {
		t.Fatalf("expected WS read_sync delivery, got %q", got.Type)
	}
}
