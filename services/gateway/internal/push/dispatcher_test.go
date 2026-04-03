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
