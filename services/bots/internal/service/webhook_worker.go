// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/crypto"
	"github.com/mst-corp/orbit/services/bots/internal/botapi"
	"github.com/redis/go-redis/v9"
)

type WebhookWorker struct {
	redis         *redis.Client
	encryptionKey []byte
	logger        *slog.Logger
	// Custom transport with SSRF-safe dialer
	httpClient *http.Client
}

type webhookDeliveryJob struct {
	BotID      uuid.UUID     `json:"bot_id"`
	WebhookURL string        `json:"webhook_url"`
	SecretEnc  string        `json:"secret_enc,omitempty"` // AES-encrypted raw secret
	Update     botapi.Update `json:"update"`
}

func NewWebhookWorker(rdb *redis.Client, encryptionKey []byte, logger *slog.Logger) *WebhookWorker {
	return &WebhookWorker{
		redis:         rdb,
		encryptionKey: encryptionKey,
		logger:        logger,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext: ssrfSafeDialer,
			},
		},
	}
}

func (w *WebhookWorker) Enqueue(botID uuid.UUID, webhookURL string, secretEnc string, update botapi.Update) error {
	if w.redis == nil {
		return fmt.Errorf("redis is not configured")
	}

	job := webhookDeliveryJob{
		BotID:      botID,
		WebhookURL: webhookURL,
		SecretEnc:  secretEnc,
		Update:     update,
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal webhook delivery job: %w", err)
	}

	key := "webhook_queue:" + botID.String()
	ctx := context.Background()

	// Track active queues in a SET instead of scanning all keys
	pipe := w.redis.TxPipeline()
	pipe.RPush(ctx, key, payload)
	pipe.Expire(ctx, key, botUpdatesTTL)
	pipe.SAdd(ctx, "webhook_active_queues", botID.String())
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("enqueue webhook delivery job: %w", err)
	}

	return nil
}

func (w *WebhookWorker) Start(ctx context.Context) {
	iterations := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Periodic cleanup of stale entries in webhook_active_queues (every 100 iterations)
		iterations++
		if iterations%100 == 0 {
			w.cleanupActiveQueues(ctx)
		}

		// Use SET of active queues instead of KEYS scan (O(N) → O(1) per member)
		members, err := w.redis.SMembers(ctx, "webhook_active_queues").Result()
		if err != nil {
			w.logger.Error("webhook worker scan failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		if len(members) == 0 {
			time.Sleep(time.Second)
			continue
		}

		keys := make([]string, len(members))
		for i, m := range members {
			keys[i] = "webhook_queue:" + m
		}

		result, err := w.redis.BLPop(ctx, time.Second, keys...).Result()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			w.logger.Error("webhook worker blpop failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		if len(result) != 2 {
			continue
		}

		var job webhookDeliveryJob
		if err := json.Unmarshal([]byte(result[1]), &job); err != nil {
			w.logger.Error("webhook worker decode failed", "error", err)
			continue
		}

		// Clean up empty queues from active set
		queueLen, _ := w.redis.LLen(ctx, result[0]).Result()
		if queueLen == 0 {
			w.redis.SRem(ctx, "webhook_active_queues", job.BotID.String())
		}

		payload, err := json.Marshal(job.Update)
		if err != nil {
			w.logger.Error("webhook worker marshal update failed", "error", err, "bot_id", job.BotID)
			continue
		}

		// Decrypt the raw secret for HMAC signing
		var rawSecret string
		if job.SecretEnc != "" {
			rawSecret, err = crypto.Decrypt(job.SecretEnc, w.encryptionKey)
			if err != nil {
				w.logger.Error("webhook worker decrypt secret failed", "error", err, "bot_id", job.BotID)
				continue
			}
		}

		backoffs := []time.Duration{time.Second, 5 * time.Second, 25 * time.Second}
		var deliveryErr error
		for attempt, backoff := range backoffs {
			deliveryErr = w.deliverWebhook(job.WebhookURL, rawSecret, payload)
			if deliveryErr == nil {
				break
			}
			w.logger.Warn("webhook delivery failed",
				"bot_id", job.BotID,
				"webhook_url", job.WebhookURL,
				"attempt", attempt+1,
				"error", deliveryErr,
			)
			time.Sleep(backoff)
		}
		if deliveryErr != nil {
			w.logger.Error("webhook delivery permanently failed", "bot_id", job.BotID, "error", deliveryErr)
			w.redis.Incr(ctx, "webhook_failures:"+job.BotID.String())
		}
	}
}

func (w *WebhookWorker) deliverWebhook(webhookURL string, rawSecret string, payload []byte) error {
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if rawSecret != "" {
		mac := hmac.New(sha256.New, []byte(rawSecret))
		mac.Write(payload)
		req.Header.Set("X-Orbit-Signature", hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := w.httpClient.Do(req) // Uses SSRF-safe transport
	if err != nil {
		return fmt.Errorf("send webhook request: %w", err)
	}
	// Drain and cap response body to allow connection reuse
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// ssrfSafeDialer resolves DNS and validates IPs in the same step,
// preventing DNS rebinding attacks (TOCTOU between validation and connect).
func ssrfSafeDialer(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host: %w", err)
	}

	for _, ip := range ips {
		if ip.IP.IsLoopback() || ip.IP.IsPrivate() || ip.IP.IsLinkLocalMulticast() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsUnspecified() || ip.IP.IsMulticast() {
			return nil, fmt.Errorf("private or reserved webhook hosts are not allowed")
		}
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no IPs resolved for host")
	}

	// Connect directly to the validated IP
	validatedAddr := net.JoinHostPort(ips[0].IP.String(), port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, network, validatedAddr)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// cleanupActiveQueues removes stale bot IDs from the active queues SET
// (entries whose Redis list keys have expired via TTL).
func (w *WebhookWorker) cleanupActiveQueues(ctx context.Context) {
	members, err := w.redis.SMembers(ctx, "webhook_active_queues").Result()
	if err != nil {
		return
	}
	for _, m := range members {
		exists, err := w.redis.Exists(ctx, "webhook_queue:"+m).Result()
		if err == nil && exists == 0 {
			w.redis.SRem(ctx, "webhook_active_queues", m)
		}
	}
}

// isPrivateOrReservedIP is kept for any standalone URL validation needs.
func isPrivateOrReservedIP(ipStr string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast()
}
