// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package push

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

type mockHTTPClient struct {
	doFn func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.doFn != nil {
		return m.doFn(req)
	}
	return nil, errors.New("unexpected request")
}

func TestDispatcher_SendToUser_RemovesStaleSubscription(t *testing.T) {
	userID := "11111111-1111-1111-1111-111111111111"
	var deleted bool

	client := &mockHTTPClient{
		doFn: func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/internal/push-subscriptions/"+userID):
				return jsonResponse(http.StatusOK, `[{"endpoint":"https://push.example/sub","p256dh":"key","auth":"auth"}]`), nil
			case req.Method == http.MethodDelete && strings.HasSuffix(req.URL.Path, "/internal/push-subscriptions/"+userID):
				if got := req.URL.Query().Get("endpoint"); got != "https://push.example/sub" {
					t.Fatalf("unexpected endpoint query: %s", got)
				}
				deleted = true
				return emptyResponse(http.StatusNoContent), nil
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		},
	}

	dispatcher := NewDispatcher(Config{
		PublicKey:           "public",
		PrivateKey:          "private",
		Subscriber:          "mailto:test@example.com",
		MessagingServiceURL: "http://messaging",
		InternalSecret:      "secret",
		HTTPClient:          client,
		Logger:              slog.Default(),
	})
	dispatcher.sendNotificationFn = func(context.Context, []byte, *webpush.Subscription, *webpush.Options) (*http.Response, error) {
		return emptyResponse(http.StatusGone), nil
	}

	if err := dispatcher.SendToUser(userID, []byte(`{"title":"Orbit"}`)); err != nil {
		t.Fatalf("SendToUser returned error: %v", err)
	}
	if !deleted {
		t.Fatal("expected stale subscription to be deleted")
	}
}

func TestDispatcher_SendToUser_RetriesTransientFailures(t *testing.T) {
	userID := "22222222-2222-2222-2222-222222222222"
	sendCalls := 0
	var sleeps []string

	client := &mockHTTPClient{
		doFn: func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet {
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
			}
			return jsonResponse(http.StatusOK, `[{"endpoint":"https://push.example/sub","p256dh":"key","auth":"auth"}]`), nil
		},
	}

	dispatcher := NewDispatcher(Config{
		PublicKey:           "public",
		PrivateKey:          "private",
		Subscriber:          "mailto:test@example.com",
		MessagingServiceURL: "http://messaging",
		InternalSecret:      "secret",
		HTTPClient:          client,
		Logger:              slog.Default(),
	})
	dispatcher.sleepFn = func(dur time.Duration) {
		sleeps = append(sleeps, dur.String())
	}
	dispatcher.sendNotificationFn = func(context.Context, []byte, *webpush.Subscription, *webpush.Options) (*http.Response, error) {
		sendCalls++
		if sendCalls < 3 {
			return emptyResponse(http.StatusServiceUnavailable), nil
		}
		return emptyResponse(http.StatusCreated), nil
	}

	if err := dispatcher.SendToUser(userID, []byte(`{"title":"Orbit"}`)); err != nil {
		t.Fatalf("SendToUser returned error: %v", err)
	}
	if sendCalls != 3 {
		t.Fatalf("expected 3 send attempts, got %d", sendCalls)
	}
	if len(sleeps) != 2 || sleeps[0] != "100ms" || sleeps[1] != "200ms" {
		t.Fatalf("unexpected backoff sequence: %+v", sleeps)
	}
}

func TestDispatcher_SendToUsers_DeduplicatesUserIDs(t *testing.T) {
	userOne := "33333333-3333-3333-3333-333333333333"
	userTwo := "44444444-4444-4444-4444-444444444444"
	fetches := make(map[string]int)
	sends := 0

	client := &mockHTTPClient{
		doFn: func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet {
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
			}
			userID := strings.TrimPrefix(req.URL.Path, "/internal/push-subscriptions/")
			fetches[userID]++
			return jsonResponse(http.StatusOK, `[{"endpoint":"https://push.example/`+userID+`","p256dh":"key","auth":"auth"}]`), nil
		},
	}

	dispatcher := NewDispatcher(Config{
		PublicKey:           "public",
		PrivateKey:          "private",
		Subscriber:          "mailto:test@example.com",
		MessagingServiceURL: "http://messaging",
		InternalSecret:      "secret",
		HTTPClient:          client,
		Logger:              slog.Default(),
	})
	dispatcher.sendNotificationFn = func(context.Context, []byte, *webpush.Subscription, *webpush.Options) (*http.Response, error) {
		sends++
		return emptyResponse(http.StatusCreated), nil
	}

	if err := dispatcher.SendToUsers([]string{userOne, userTwo, userOne, ""}, []byte(`{"title":"Orbit"}`)); err != nil {
		t.Fatalf("SendToUsers returned error: %v", err)
	}
	if fetches[userOne] != 1 || fetches[userTwo] != 1 {
		t.Fatalf("unexpected fetch counts: %+v", fetches)
	}
	if sends != 2 {
		t.Fatalf("expected 2 push sends, got %d", sends)
	}
}

func TestDispatcher_SendCallToUsers_UsesHighUrgencyAndShortTTL(t *testing.T) {
	userOne := "55555555-5555-5555-5555-555555555555"
	userTwo := "66666666-6666-6666-6666-666666666666"

	client := &mockHTTPClient{
		doFn: func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet {
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
			}
			userID := strings.TrimPrefix(req.URL.Path, "/internal/push-subscriptions/")
			return jsonResponse(http.StatusOK, `[{"endpoint":"https://push.example/`+userID+`","p256dh":"key","auth":"auth"}]`), nil
		},
	}

	dispatcher := NewDispatcher(Config{
		PublicKey:           "public",
		PrivateKey:          "private",
		Subscriber:          "mailto:test@example.com",
		MessagingServiceURL: "http://messaging",
		InternalSecret:      "secret",
		HTTPClient:          client,
		Logger:              slog.Default(),
		TTL:                 2 * 24 * 3600, // explicit non-call default to prove override
	})

	var capturedOpts []*webpush.Options
	dispatcher.sendNotificationFn = func(_ context.Context, _ []byte, _ *webpush.Subscription, opts *webpush.Options) (*http.Response, error) {
		clone := *opts
		capturedOpts = append(capturedOpts, &clone)
		return emptyResponse(http.StatusCreated), nil
	}

	if err := dispatcher.SendCallToUsers([]string{userOne, userTwo}, []byte(`{"type":"call_incoming"}`)); err != nil {
		t.Fatalf("SendCallToUsers returned error: %v", err)
	}

	if len(capturedOpts) != 2 {
		t.Fatalf("expected 2 send attempts, got %d", len(capturedOpts))
	}
	for i, opts := range capturedOpts {
		if opts.Urgency != webpush.UrgencyHigh {
			t.Fatalf("attempt %d: expected UrgencyHigh, got %q", i, opts.Urgency)
		}
		if opts.TTL != 30 {
			t.Fatalf("attempt %d: expected TTL=30, got %d", i, opts.TTL)
		}
	}
}

func TestDispatcher_SendCallToUsers_RemovesStaleSubscriptionAndContinues(t *testing.T) {
	deadUser := "77777777-7777-7777-7777-777777777777"
	liveUser := "88888888-8888-8888-8888-888888888888"

	var deletedEndpoint string
	var liveDelivered bool

	client := &mockHTTPClient{
		doFn: func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodGet:
				userID := strings.TrimPrefix(req.URL.Path, "/internal/push-subscriptions/")
				return jsonResponse(http.StatusOK, `[{"endpoint":"https://push.example/`+userID+`","p256dh":"key","auth":"auth"}]`), nil
			case req.Method == http.MethodDelete:
				deletedEndpoint = req.URL.Query().Get("endpoint")
				return emptyResponse(http.StatusNoContent), nil
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		},
	}

	dispatcher := NewDispatcher(Config{
		PublicKey:           "public",
		PrivateKey:          "private",
		Subscriber:          "mailto:test@example.com",
		MessagingServiceURL: "http://messaging",
		InternalSecret:      "secret",
		HTTPClient:          client,
		Logger:              slog.Default(),
	})
	dispatcher.sendNotificationFn = func(_ context.Context, _ []byte, sub *webpush.Subscription, _ *webpush.Options) (*http.Response, error) {
		if strings.HasSuffix(sub.Endpoint, deadUser) {
			return emptyResponse(http.StatusGone), nil
		}
		liveDelivered = true
		return emptyResponse(http.StatusCreated), nil
	}

	if err := dispatcher.SendCallToUsers([]string{deadUser, liveUser}, []byte(`{"type":"call_incoming"}`)); err != nil {
		t.Fatalf("SendCallToUsers returned error: %v", err)
	}
	if deletedEndpoint != "https://push.example/"+deadUser {
		t.Fatalf("expected stale subscription deletion for dead user, got %q", deletedEndpoint)
	}
	if !liveDelivered {
		t.Fatal("expected live user to receive push despite dead peer")
	}
}

func TestDispatcher_SendCallToUsers_NoSubscriptionsIsNoop(t *testing.T) {
	userID := "99999999-9999-9999-9999-999999999999"

	client := &mockHTTPClient{
		doFn: func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet {
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
			}
			return jsonResponse(http.StatusOK, `[]`), nil
		},
	}

	dispatcher := NewDispatcher(Config{
		PublicKey:           "public",
		PrivateKey:          "private",
		Subscriber:          "mailto:test@example.com",
		MessagingServiceURL: "http://messaging",
		InternalSecret:      "secret",
		HTTPClient:          client,
		Logger:              slog.Default(),
	})
	dispatcher.sendNotificationFn = func(context.Context, []byte, *webpush.Subscription, *webpush.Options) (*http.Response, error) {
		t.Fatal("sendNotificationFn must not be called when subscriptions are empty")
		return nil, nil
	}

	if err := dispatcher.SendCallToUsers([]string{userID}, []byte(`{"type":"call_incoming"}`)); err != nil {
		t.Fatalf("SendCallToUsers returned error: %v", err)
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func emptyResponse(status int) *http.Response {
	return jsonResponse(status, "")
}
