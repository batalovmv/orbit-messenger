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
)

const (
	defaultRequestTimeout = 5 * time.Second
	defaultPushTTL        = 30
	defaultMaxAttempts    = 3
	defaultSubscriber     = "mailto:push@orbit.local"
	// callPushTTL caps how long providers should hold a call notification.
	// Calls auto-expire after ~60s anyway, so anything longer is wasted.
	callPushTTL = 30
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

func (d *Dispatcher) SendToUsers(userIDs []string, payload []byte) error {
	return d.sendToUsers(userIDs, payload, d.defaultOptions())
}

func (d *Dispatcher) SendToUser(userID string, payload []byte) error {
	return d.sendToUser(userID, payload, d.defaultOptions())
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
			return nil
		case err == nil && statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices:
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

	d.logger.Warn("web push delivery failed", "error", lastErr, "user_id", userID, "endpoint", subscription.Endpoint)
	return lastErr
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
