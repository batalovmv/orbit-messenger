// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package push

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"
)

// TestSendToUserWithReport_MixedOutcomes covers the three buckets the admin
// inspector cares about: a healthy device (ok), a provider-evicted device
// (stale → 410), and a transient failure (fail). The report must aggregate
// counts AND surface per-device rows with sanitized errors and host suffixes.
func TestSendToUserWithReport_MixedOutcomes(t *testing.T) {
	userID := "11111111-1111-1111-1111-111111111111"
	deviceOK := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	deviceStale := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	deviceFail := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")

	// fetch returns 3 subscriptions across distinct providers; the gateway
	// must report per-device endpoint_host AND never echo the raw endpoint.
	subsJSON := `[
		{"id":"` + deviceOK.String() + `","endpoint":"https://fcm.googleapis.com/secret/path","p256dh":"k","auth":"a","user_agent":"Chrome/121"},
		{"id":"` + deviceStale.String() + `","endpoint":"https://updates.push.services.mozilla.com/secret/path","p256dh":"k","auth":"a","user_agent":"Firefox/122"},
		{"id":"` + deviceFail.String() + `","endpoint":"https://web.push.apple.com/secret/path","p256dh":"k","auth":"a","user_agent":"Safari/17"}
	]`

	var deletedEndpoints []string
	client := &mockHTTPClient{
		doFn: func(req *http.Request) (*http.Response, error) {
			switch req.Method {
			case http.MethodGet:
				return jsonResponse(http.StatusOK, subsJSON), nil
			case http.MethodDelete:
				deletedEndpoints = append(deletedEndpoints, req.URL.Query().Get("endpoint"))
				return emptyResponse(http.StatusNoContent), nil
			default:
				t.Fatalf("unexpected method %s", req.Method)
				return nil, nil
			}
		},
	}

	dispatcher := NewDispatcher(Config{
		PublicKey: "p", PrivateKey: "k", Subscriber: "mailto:test@example.com",
		MessagingServiceURL: "http://messaging", InternalSecret: "secret",
		HTTPClient: client, Logger: slog.Default(),
	})
	dispatcher.sleepFn = func(time.Duration) {}
	dispatcher.sendNotificationFn = func(_ context.Context, _ []byte, sub *webpush.Subscription, _ *webpush.Options) (*http.Response, error) {
		switch {
		case strings.Contains(sub.Endpoint, "fcm.googleapis.com"):
			return emptyResponse(http.StatusCreated), nil
		case strings.Contains(sub.Endpoint, "mozilla.com"):
			return emptyResponse(http.StatusGone), nil
		default:
			return emptyResponse(http.StatusInternalServerError), nil
		}
	}

	report, err := dispatcher.SendToUserWithReport(context.Background(), userID, []byte(`{"title":"x"}`))
	if err != nil {
		t.Fatalf("SendToUserWithReport: %v", err)
	}
	if report.DeviceCount != 3 || report.Sent != 1 || report.Stale != 1 || report.Failed != 1 {
		t.Fatalf("counts mismatch: %+v", report)
	}
	if len(report.Outcomes) != 3 {
		t.Fatalf("expected 3 outcomes, got %d", len(report.Outcomes))
	}

	byID := map[uuid.UUID]Outcome{}
	for _, oc := range report.Outcomes {
		byID[oc.DeviceID] = oc
	}

	if got := byID[deviceOK]; got.Status != "ok" || got.EndpointHost != "fcm.googleapis.com" || got.Error != "" {
		t.Fatalf("ok device wrong: %+v", got)
	}
	if got := byID[deviceStale]; got.Status != "stale" || got.EndpointHost != "updates.push.services.mozilla.com" {
		t.Fatalf("stale device wrong: %+v", got)
	}
	if got := byID[deviceFail]; got.Status != "fail" || got.EndpointHost != "web.push.apple.com" {
		t.Fatalf("fail device wrong: %+v", got)
	}

	// CRITICAL: error strings must NOT include the raw endpoint URL — that's
	// the part that acts like an auth token. Hosts are fine; paths are not.
	for _, oc := range report.Outcomes {
		if strings.Contains(oc.Error, "/secret/path") {
			t.Fatalf("outcome %s leaked endpoint path in error: %q", oc.DeviceID, oc.Error)
		}
	}

	// Stale device must trigger a delete-subscription call so the user's
	// inspector report stops listing the dead device on the next click.
	if len(deletedEndpoints) != 1 || !strings.Contains(deletedEndpoints[0], "mozilla.com") {
		t.Fatalf("expected one delete for stale mozilla device, got %v", deletedEndpoints)
	}
}

// TestSendToUserWithReport_NoDevices covers the "user has no registered push
// subscriptions" path. Admin UI uses an empty Outcomes list to display
// "user has no devices" — we must NOT return an error here.
func TestSendToUserWithReport_NoDevices(t *testing.T) {
	userID := "22222222-2222-2222-2222-222222222222"
	client := &mockHTTPClient{
		doFn: func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `[]`), nil
		},
	}

	dispatcher := NewDispatcher(Config{
		PublicKey: "p", PrivateKey: "k",
		MessagingServiceURL: "http://messaging", InternalSecret: "secret",
		HTTPClient: client, Logger: slog.Default(),
	})

	report, err := dispatcher.SendToUserWithReport(context.Background(), userID, []byte(`{"title":"x"}`))
	if err != nil {
		t.Fatalf("expected nil error for no-devices, got %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.DeviceCount != 0 || len(report.Outcomes) != 0 {
		t.Fatalf("expected empty report, got %+v", report)
	}
}

// TestSendToUserWithReport_DispatcherDisabled covers the missing-VAPID
// configuration path. Returning a clean error lets the admin UI surface
// "push not configured on this deploy" rather than a generic 500.
func TestSendToUserWithReport_DispatcherDisabled(t *testing.T) {
	dispatcher := NewDispatcher(Config{
		// Missing public/private/messaging URL → Enabled() == false.
		Logger: slog.Default(),
	})

	report, err := dispatcher.SendToUserWithReport(context.Background(),
		"11111111-1111-1111-1111-111111111111", []byte(`{"title":"x"}`))
	if err == nil {
		t.Fatal("expected error when dispatcher disabled")
	}
	if report != nil {
		t.Fatalf("expected nil report on disabled, got %+v", report)
	}
}

// TestSendToUserWithReport_ContextCancelledMidLoop proves we honor caller
// deadlines mid-dispatch. The fetch returns 2 subscriptions; we cancel the
// context after the FIRST send. The dispatcher must observe ctx.Err() before
// the second send and return the cancellation error rather than a "200 with
// all-failures" report — the admin handler maps the error to 504.
func TestSendToUserWithReport_ContextCancelledMidLoop(t *testing.T) {
	userID := "33333333-3333-3333-3333-333333333333"
	subsJSON := `[
		{"id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","endpoint":"https://fcm.googleapis.com/x","p256dh":"k","auth":"a"},
		{"id":"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb","endpoint":"https://web.push.apple.com/y","p256dh":"k","auth":"a"}
	]`
	client := &mockHTTPClient{
		doFn: func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, subsJSON), nil
		},
	}
	dispatcher := NewDispatcher(Config{
		PublicKey: "p", PrivateKey: "k",
		MessagingServiceURL: "http://messaging", InternalSecret: "secret",
		HTTPClient: client, Logger: slog.Default(),
	})

	ctx, cancel := context.WithCancel(context.Background())

	sendCount := 0
	dispatcher.sendNotificationFn = func(_ context.Context, _ []byte, _ *webpush.Subscription, _ *webpush.Options) (*http.Response, error) {
		sendCount++
		if sendCount == 1 {
			// Cancel partway: the loop should not call sendNotificationFn
			// for the second subscription.
			cancel()
		}
		return emptyResponse(http.StatusCreated), nil
	}

	report, err := dispatcher.SendToUserWithReport(ctx, userID, []byte(`{"title":"x"}`))
	if err == nil {
		t.Fatalf("expected ctx.Err(), got nil; report=%+v", report)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if report != nil {
		t.Fatalf("expected nil report on cancellation, got %+v", report)
	}
	if sendCount != 1 {
		t.Fatalf("expected exactly 1 send before cancel, got %d", sendCount)
	}
}

// TestSendToUserWithReport_SingleDeviceCancelDuringSend covers the case where
// the only subscription's send returns context.DeadlineExceeded — the webpush
// transport translates that into a generic "fail" outcome, but
// SendToUserWithReport must observe ctx.Err() and surface the cancellation
// rather than returning a 1-device "fail" report. Without the post-send
// ctx check this test would see a 200-equivalent result.
func TestSendToUserWithReport_SingleDeviceCancelDuringSend(t *testing.T) {
	userID := "55555555-5555-5555-5555-555555555555"
	subsJSON := `[{"id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","endpoint":"https://web.push.apple.com/x","p256dh":"k","auth":"a"}]`
	client := &mockHTTPClient{
		doFn: func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, subsJSON), nil
		},
	}
	dispatcher := NewDispatcher(Config{
		PublicKey: "p", PrivateKey: "k",
		MessagingServiceURL: "http://messaging", InternalSecret: "secret",
		HTTPClient: client, Logger: slog.Default(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	dispatcher.sendNotificationFn = func(_ context.Context, _ []byte, _ *webpush.Subscription, _ *webpush.Options) (*http.Response, error) {
		// Simulate the webpush layer treating the deadline as a generic
		// transport failure: cancel ctx mid-send, then return a fake
		// response. Without the post-send ctx.Err check, the dispatcher
		// would treat this as just a "fail" device and return 200.
		cancel()
		return emptyResponse(http.StatusInternalServerError), nil
	}

	report, err := dispatcher.SendToUserWithReport(ctx, userID, []byte(`{"title":"x"}`))
	if err == nil {
		t.Fatalf("expected ctx error, got nil; report=%+v", report)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if report != nil {
		t.Fatalf("expected nil report on cancelled-during-send, got %+v", report)
	}
}

// TestSendToUserWithReport_PayloadEmpty rejects empty payloads — caller must
// always supply something the SW can render.
func TestSendToUserWithReport_PayloadEmpty(t *testing.T) {
	dispatcher := NewDispatcher(Config{
		PublicKey: "p", PrivateKey: "k",
		MessagingServiceURL: "http://messaging", InternalSecret: "secret",
		Logger: slog.Default(),
	})
	if _, err := dispatcher.SendToUserWithReport(context.Background(), "uid", nil); err == nil {
		t.Fatal("expected error on empty payload")
	}
}

// TestSendToUserWithReport_FetchSubscriptionsFails surfaces the upstream
// error to the caller cleanly — no panic, no nil-report, no "delivered"
// counters incremented.
func TestSendToUserWithReport_FetchSubscriptionsFails(t *testing.T) {
	userID := "44444444-4444-4444-4444-444444444444"
	client := &mockHTTPClient{
		doFn: func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("messaging unreachable")
		},
	}
	dispatcher := NewDispatcher(Config{
		PublicKey: "p", PrivateKey: "k",
		MessagingServiceURL: "http://messaging", InternalSecret: "secret",
		HTTPClient: client, Logger: slog.Default(),
	})

	report, err := dispatcher.SendToUserWithReport(context.Background(), userID, []byte(`{"x":1}`))
	if err == nil {
		t.Fatal("expected error from fetch failure")
	}
	if report != nil {
		t.Fatalf("expected nil report on fetch failure, got %+v", report)
	}
}

// TestEndpointHost_KnownProviders sanity-checks the host extractor —
// surfacing a wrong host to admins (or "unknown" for valid URLs) would
// degrade the inspector's debugging value.
func TestEndpointHost_KnownProviders(t *testing.T) {
	cases := []struct {
		endpoint string
		want     string
	}{
		{"https://fcm.googleapis.com/wp/abc123", "fcm.googleapis.com"},
		{"https://updates.push.services.mozilla.com/wpush/v2/xxx", "updates.push.services.mozilla.com"},
		{"https://web.push.apple.com/QFunny/path", "web.push.apple.com"},
		{"", ""},
		{"not-a-url", "unknown"},
	}
	for _, tc := range cases {
		if got := endpointHost(tc.endpoint); got != tc.want {
			t.Errorf("endpointHost(%q) = %q, want %q", tc.endpoint, got, tc.want)
		}
	}
}

// TestSanitizeSendError_NoEndpointLeak guards the contract that error strings
// returned to admins never include the per-device endpoint URL — these act
// like auth tokens for the push provider.
func TestSanitizeSendError_NoEndpointLeak(t *testing.T) {
	err := errors.New("provider returned: https://web.push.apple.com/secret/abcdef rejected")
	endpoint := "https://web.push.apple.com/secret/abcdef"

	got := sanitizeSendError(err, 0, endpoint)
	if strings.Contains(got, "/secret/abcdef") {
		t.Fatalf("sanitized error leaked endpoint path: %q", got)
	}
}

