// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	defaultRequestTimeout = 5 * time.Second
	defaultPushTTL        = 30
	defaultMaxAttempts    = 3
	defaultSubscriber     = "mailto:push@orbit.local"
	// callPushTTL caps how long providers should hold a call notification.
	// Calls auto-expire after ~60s anyway, so anything longer is wasted.
	callPushTTL = 30
	// readSyncPushTTL bounds how long a read-sync silent push waits at the
	// provider. The point of read-sync is to clear stale notifications quickly;
	// if the device hasn't woken in a minute, the user will reconcile on next
	// foreground via the /chats sync anyway. Shorter TTL also keeps APNs
	// throttle accounting tighter — Apple counts undelivered pushes against
	// the budget too.
	readSyncPushTTL = 60
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Config struct {
	PublicKey           string
	PrivateKey          string
	Subscriber          string
	MessagingServiceURL string
	InternalSecret      string
	Logger              *slog.Logger
	HTTPClient          HTTPClient
	TTL                 int
	// AttemptsCounter, when set, gets one Inc() per push attempt, labelled by
	// outcome: "ok" (delivered), "fail" (terminal failure after retries), or
	// "stale" (410/404 from the provider — subscription deleted, not a delivery
	// failure). The counter is registered by the caller so the Dispatcher
	// stays decoupled from the metrics registry and re-creating a Dispatcher
	// in tests does not double-register.
	AttemptsCounter *prometheus.CounterVec
}

type Dispatcher struct {
	publicKey           string
	privateKey          string
	subscriber          string
	messagingServiceURL string
	internalSecret      string
	httpClient          HTTPClient
	logger              *slog.Logger
	ttl                 int
	sendNotificationFn  func(ctx context.Context, payload []byte, sub *webpush.Subscription, opts *webpush.Options) (*http.Response, error)
	sleepFn             func(time.Duration)
	attemptsCounter     *prometheus.CounterVec
}

type PushSubscription struct {
	Endpoint string `json:"endpoint"`
	P256DH   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

func NewDispatcher(cfg Config) *Dispatcher {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}

	dispatcher := &Dispatcher{
		publicKey:           cfg.PublicKey,
		privateKey:          cfg.PrivateKey,
		subscriber:          cfg.Subscriber,
		messagingServiceURL: strings.TrimRight(cfg.MessagingServiceURL, "/"),
		internalSecret:      cfg.InternalSecret,
		httpClient:          httpClient,
		logger:              logger,
		ttl:                 cfg.TTL,
		sleepFn:             time.Sleep,
		attemptsCounter:     cfg.AttemptsCounter,
	}
	if dispatcher.subscriber == "" {
		dispatcher.subscriber = defaultSubscriber
	}
	if dispatcher.ttl <= 0 {
		dispatcher.ttl = defaultPushTTL
	}
	dispatcher.sendNotificationFn = dispatcher.sendNotification

	return dispatcher
}

func (d *Dispatcher) Enabled() bool {
	if d == nil {
		return false
	}

	return d.publicKey != "" && d.privateKey != "" && d.messagingServiceURL != ""
}

// sendOptions controls per-delivery web push parameters.
type sendOptions struct {
	ttl     int
	urgency webpush.Urgency
}

func (d *Dispatcher) defaultOptions() sendOptions {
	return sendOptions{ttl: d.ttl, urgency: webpush.UrgencyNormal}
}

func (d *Dispatcher) callOptions() sendOptions {
	return sendOptions{ttl: callPushTTL, urgency: webpush.UrgencyHigh}
}

// readSyncOptions: low urgency so iOS/APNs treats the silent payload as
// non-priority and is more likely to keep us inside the per-device budget.
// The SW on the receiving end never shows UI for these — it only calls
// closeNotifications on already-displayed banners, so urgency=low is correct.
func (d *Dispatcher) readSyncOptions() sendOptions {
	return sendOptions{ttl: readSyncPushTTL, urgency: webpush.UrgencyLow}
}

func (d *Dispatcher) SendToUsers(userIDs []string, payload []byte) error {
	return d.sendToUsers(userIDs, payload, d.defaultOptions())
}

// SendToUsersWithPriority sends a push notification with AI-classified priority.
func (d *Dispatcher) SendToUsersWithPriority(userIDs []string, payload []byte, priority string) error {
	opts := d.defaultOptions()
	switch priority {
	case "urgent":
		opts.urgency = webpush.UrgencyHigh
	case "important":
		opts.urgency = webpush.UrgencyNormal
	case "low":
		opts.urgency = webpush.UrgencyLow
	default:
		opts.urgency = webpush.UrgencyNormal
	}
	return d.sendToUsers(userIDs, payload, opts)
}

func (d *Dispatcher) SendToUser(userID string, payload []byte) error {
	return d.sendToUser(userID, payload, d.defaultOptions())
}

// SendReadSyncToUser fans a silent read-sync payload to a single user's push
// subscriptions with a short TTL and low urgency. The SW translates this into
// closeNotifications — there's no banner. Used as the offline fallback when
// the user's other devices have no active WS connection at the moment of
// MarkRead, so stale notifications would otherwise sit until next foreground.
func (d *Dispatcher) SendReadSyncToUser(userID string, payload []byte) error {
	return d.sendToUser(userID, payload, d.readSyncOptions())
}

// SendCallToUsers fans out a high-urgency call notification with a short TTL.
// Call auto-expires after ~60s, so a 30s TTL avoids stale notifications. The
// loop is fail-open across users: a 410/expired subscription for one user
// must not block delivery to the rest.
func (d *Dispatcher) SendCallToUsers(userIDs []string, payload []byte) error {
	return d.sendToUsers(userIDs, payload, d.callOptions())
}

func (d *Dispatcher) sendToUsers(userIDs []string, payload []byte, opts sendOptions) error {
	if !d.Enabled() || len(payload) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(userIDs))
	errs := make([]error, 0)

	for _, userID := range userIDs {
		if userID == "" {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}

		if err := d.sendToUser(userID, payload, opts); err != nil {
			errs = append(errs, fmt.Errorf("send push to user %s: %w", userID, err))
		}
	}

	return errors.Join(errs...)
}

func (d *Dispatcher) sendToUser(userID string, payload []byte, opts sendOptions) error {
	if !d.Enabled() || userID == "" || len(payload) == 0 {
		return nil
	}

	subscriptions, err := d.fetchSubscriptions(userID)
	if err != nil {
		return fmt.Errorf("fetch subscriptions: %w", err)
	}
	if len(subscriptions) == 0 {
		return nil
	}

	errs := make([]error, 0)
	for _, subscription := range subscriptions {
		if err := d.sendToSubscription(userID, subscription, payload, opts); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (d *Dispatcher) fetchSubscriptions(userID string) ([]PushSubscription, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
	defer cancel()

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

func (d *Dispatcher) deleteSubscription(userID, endpoint string) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
	defer cancel()

	deleteURL := fmt.Sprintf(
		"%s/internal/push-subscriptions/%s?endpoint=%s",
		d.messagingServiceURL,
		userID,
		url.QueryEscape(endpoint),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}
	req.Header.Set("X-Internal-Token", d.internalSecret)
	req.Header.Set("X-User-ID", userID)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute delete request: %w", err)
	}
	defer closeResponseBody(resp.Body)

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("unexpected delete status: %d", resp.StatusCode)
	}

	return nil
}

func (d *Dispatcher) sendToSubscription(userID string, subscription PushSubscription, payload []byte, opts sendOptions) error {
	pushSub := &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys: webpush.Keys{
			Auth:   subscription.Auth,
			P256dh: subscription.P256DH,
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

	var lastErr error

	for attempt := 1; attempt <= defaultMaxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
		resp, err := d.sendNotificationFn(ctx, payload, pushSub, options)
		cancel()

		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
			closeResponseBody(resp.Body)
		}

		switch {
		case statusCode == http.StatusGone || statusCode == http.StatusNotFound:
			if err := d.deleteSubscription(userID, subscription.Endpoint); err != nil {
				d.logger.Warn("delete stale push subscription failed", "error", err, "user_id", userID)
			}
			d.recordAttempt("stale")
			return nil
		case err == nil && statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices:
			d.recordAttempt("ok")
			return nil
		}

		if err != nil {
			lastErr = fmt.Errorf("send notification: %w", err)
		} else {
			lastErr = fmt.Errorf("send notification: unexpected status %d", statusCode)
		}

		if attempt == defaultMaxAttempts || !shouldRetry(statusCode, err) {
			break
		}

		d.sleepFn(time.Duration(1<<(attempt-1)) * 100 * time.Millisecond)
	}

	d.recordAttempt("fail")
	d.logger.Warn("web push delivery failed", "error", lastErr, "user_id", userID, "endpoint", subscription.Endpoint)
	return lastErr
}

// recordAttempt bumps the per-outcome counter when a metrics counter was wired
// in. It is a no-op when AttemptsCounter is nil (unit tests, services that
// chose not to instrument).
func (d *Dispatcher) recordAttempt(result string) {
	if d == nil || d.attemptsCounter == nil {
		return
	}
	d.attemptsCounter.WithLabelValues(result).Inc()
}

func (d *Dispatcher) sendNotification(ctx context.Context, payload []byte, sub *webpush.Subscription, opts *webpush.Options) (*http.Response, error) {
	options := *opts
	options.HTTPClient = d.httpClient
	return webpush.SendNotificationWithContext(ctx, payload, sub, &options)
}

func shouldRetry(statusCode int, err error) bool {
	if err != nil {
		return true
	}
	if statusCode == http.StatusTooManyRequests {
		return true
	}
	return statusCode >= http.StatusInternalServerError
}

func closeResponseBody(body io.ReadCloser) {
	if body == nil {
		return
	}

	io.Copy(io.Discard, io.LimitReader(body, 4096))
	body.Close()
}
