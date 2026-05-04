// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"
)

// Outcome is the per-subscription delivery report exposed to admin tooling
// (Day 5 Push Inspector). Endpoint URLs and VAPID keys are deliberately
// omitted — they act like opaque tokens. Only the host suffix is surfaced
// so the admin can tell APNs/FCM/Mozilla apart.
type Outcome struct {
	DeviceID     uuid.UUID `json:"device_id"`
	UserAgent    string    `json:"user_agent,omitempty"`
	EndpointHost string    `json:"endpoint_host"`
	Status       string    `json:"status"`          // "ok" | "fail" | "stale"
	Error        string    `json:"error,omitempty"` // sanitized; never the endpoint URL
}

// Report aggregates per-device outcomes. Counts are derived (not persisted) —
// the caller can re-derive them from Outcomes if needed.
type Report struct {
	UserID      string    `json:"user_id"`
	DeviceCount int       `json:"device_count"`
	Sent        int       `json:"sent"`
	Failed      int       `json:"failed"`
	Stale       int       `json:"stale"`
	Outcomes    []Outcome `json:"devices"`
}

// SendToUserWithReport dispatches a payload to every active push subscription
// of userID and returns a per-device report. Unlike SendToUser/SendCallToUsers,
// this path:
//   - threads the caller's context through fetch+send (admin tools want a hard
//     deadline; existing call/read-sync paths fire-and-forget under
//     context.Background)
//   - is non-fatal per device: a 410 Gone on one device produces a "stale"
//     row and the loop continues, so the report shows the full picture
//   - never returns raw endpoint URLs in errors — only EndpointHost is safe
//     to expose to the admin client
//
// Returns a non-nil Report on success even when DeviceCount == 0; the admin UI
// uses an empty report to surface "user has no registered devices".
func (d *Dispatcher) SendToUserWithReport(ctx context.Context, userID string, payload []byte) (*Report, error) {
	if !d.Enabled() {
		return nil, errors.New("push dispatcher disabled")
	}
	if userID == "" {
		return nil, errors.New("empty user_id")
	}
	if len(payload) == 0 {
		return nil, errors.New("empty payload")
	}

	subs, err := d.fetchSubscriptionsCtx(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("fetch subscriptions: %w", err)
	}
	// Edge case: zero-device user where the caller's deadline expired DURING
	// the fetch. Without this check we'd cheerily return an empty 0-device
	// report and the admin handler would 200 OK on a stale request.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	report := &Report{
		UserID:      userID,
		DeviceCount: len(subs),
		Outcomes:    make([]Outcome, 0, len(subs)),
	}

	opts := d.defaultOptions()
	// Distinguish operator-initiated test pushes from real chat traffic in
	// the orbit_push_attempts_total counter so the PushDeliveryFailureRate
	// alert is not skewed by Push Inspector usage.
	opts.pushType = pushTypeAdminTest
	for _, sub := range subs {
		// Honor the caller's deadline between devices. Without this check a
		// cancelled context produces a "200 with all-failures" report — the
		// admin handler then reports success when the actual semantics is
		// "we ran out of time, please retry". Returning ctx.Err() lets the
		// handler map to 504.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		oc := d.dispatchOneWithReport(ctx, userID, sub, payload, opts)
		// Re-check after the send. The webpush transport sanitizes
		// context.DeadlineExceeded into a generic "fail" outcome — without
		// this we'd return a partial report on the LAST device's deadline
		// expiry, hiding a true timeout from the admin handler. Cheap.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch oc.Status {
		case "ok":
			report.Sent++
		case "stale":
			report.Stale++
		default:
			report.Failed++
		}
		report.Outcomes = append(report.Outcomes, oc)
	}

	return report, nil
}

// fetchSubscriptionsCtx is the context-aware sibling of fetchSubscriptions.
// Original fetchSubscriptions creates its own context.Background timeout; we
// don't want to silently extend an admin's deadline, so we honor ctx.
func (d *Dispatcher) fetchSubscriptionsCtx(ctx context.Context, userID string) ([]PushSubscription, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/internal/push-subscriptions/%s", d.messagingServiceURL, userID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Internal-Token", d.internalSecret)
	req.Header.Set("X-User-ID", userID)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer closeResponseBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var subscriptions []PushSubscription
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256*1024)).Decode(&subscriptions); err != nil {
		return nil, fmt.Errorf("decode subscriptions: %w", err)
	}
	return subscriptions, nil
}

// dispatchOneWithReport sends to a single subscription and produces an Outcome.
// It deliberately does NOT retry on transient failure — for admin "test push"
// the whole point is fast feedback; if the provider is flaky right now the
// admin can re-click. Stale (410/Gone) still triggers subscription deletion
// for parity with the production dispatch path.
func (d *Dispatcher) dispatchOneWithReport(ctx context.Context, userID string, sub PushSubscription, payload []byte, opts sendOptions) Outcome {
	oc := Outcome{
		DeviceID:     sub.ID,
		UserAgent:    sub.UserAgent,
		EndpointHost: endpointHost(sub.Endpoint),
	}

	pushSub := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			Auth:   sub.Auth,
			P256dh: sub.P256DH,
		},
	}

	ttl := opts.ttl
	if ttl <= 0 {
		ttl = d.ttl
	}
	options := &webpush.Options{
		Subscriber:      d.subscriber,
		TTL:             ttl,
		Urgency:         opts.urgency,
		VAPIDPublicKey:  d.publicKey,
		VAPIDPrivateKey: d.privateKey,
	}

	sendCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer cancel()
	resp, err := d.sendNotificationFn(sendCtx, payload, pushSub, options)

	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
		closeResponseBody(resp.Body)
	}

	switch {
	case statusCode == http.StatusGone || statusCode == http.StatusNotFound:
		// Provider says the subscription is dead — evict it so the user
		// stops getting "no push" reports for a phantom device.
		if delErr := d.deleteSubscription(userID, sub.Endpoint); delErr != nil {
			d.logger.Warn("delete stale push subscription failed", "error", delErr, "user_id", userID)
		}
		d.recordAttempt("stale", opts.pushType)
		oc.Status = "stale"
		oc.Error = "subscription expired or removed by provider"
		return oc
	case err == nil && statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices:
		d.recordAttempt("ok", opts.pushType)
		oc.Status = "ok"
		return oc
	}

	d.recordAttempt("fail", opts.pushType)
	oc.Status = "fail"
	oc.Error = sanitizeSendError(err, statusCode, sub.Endpoint)
	d.logger.Warn("admin test push delivery failed",
		"user_id", userID, "endpoint_host", oc.EndpointHost, "status", statusCode)
	return oc
}

// endpointHost returns the host suffix of a push endpoint URL — useful to
// surface "fcm.googleapis.com" vs "web.push.apple.com" without leaking the
// per-device path/query that acts as the auth token.
func endpointHost(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "unknown"
	}
	return u.Host
}

// sanitizeSendError returns a short error string safe to expose to admin UI.
// Provider-supplied error bodies are NOT included — they sometimes echo the
// endpoint URL or include opaque IDs we don't want surfaced. Status code +
// generic shape is enough to debug ("apns rejected", "fcm 5xx", etc).
func sanitizeSendError(err error, statusCode int, endpoint string) string {
	if statusCode != 0 {
		return fmt.Sprintf("provider returned status %d", statusCode)
	}
	if err == nil {
		return "unknown delivery error"
	}
	host := endpointHost(endpoint)
	if host != "" && host != "unknown" {
		return fmt.Sprintf("transport error to %s", host)
	}
	return "transport error"
}
