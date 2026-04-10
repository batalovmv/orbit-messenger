package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/bots/internal/botapi"
	"github.com/redis/go-redis/v9"
)

type WebhookWorker struct {
	redis  *redis.Client
	logger *slog.Logger
}

type webhookDeliveryJob struct {
	BotID      uuid.UUID     `json:"bot_id"`
	WebhookURL string        `json:"webhook_url"`
	SecretHash string        `json:"secret_hash,omitempty"`
	Update     botapi.Update `json:"update"`
}

func NewWebhookWorker(redis *redis.Client, logger *slog.Logger) *WebhookWorker {
	return &WebhookWorker{redis: redis, logger: logger}
}

func (w *WebhookWorker) Enqueue(botID uuid.UUID, webhookURL string, secretHash string, update botapi.Update) error {
	if w.redis == nil {
		return fmt.Errorf("redis is not configured")
	}

	job := webhookDeliveryJob{
		BotID:      botID,
		WebhookURL: webhookURL,
		SecretHash: secretHash,
		Update:     update,
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal webhook delivery job: %w", err)
	}

	key := "webhook_queue:" + botID.String()
	ctx := context.Background()
	pipe := w.redis.TxPipeline()
	pipe.RPush(ctx, key, payload)
	pipe.Expire(ctx, key, botUpdatesTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("enqueue webhook delivery job: %w", err)
	}

	return nil
}

func (w *WebhookWorker) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		keys, err := w.redis.Keys(ctx, "webhook_queue:*").Result()
		if err != nil {
			w.logger.Error("webhook worker scan failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		if len(keys) == 0 {
			time.Sleep(time.Second)
			continue
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

		payload, err := json.Marshal(job.Update)
		if err != nil {
			w.logger.Error("webhook worker marshal update failed", "error", err, "bot_id", job.BotID)
			continue
		}

		backoffs := []time.Duration{time.Second, 5 * time.Second, 25 * time.Second}
		var deliveryErr error
		for attempt, backoff := range backoffs {
			deliveryErr = w.deliverWebhook(job.WebhookURL, job.SecretHash, payload)
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

func (w *WebhookWorker) deliverWebhook(webhookURL string, secretHash string, payload []byte) error {
	if err := validateOutgoingWebhookURL(webhookURL); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if secretHash != "" {
		mac := hmac.New(sha256.New, []byte(secretHash))
		mac.Write(payload)
		req.Header.Set("X-Orbit-Signature", hex.EncodeToString(mac.Sum(nil)))
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

func validateOutgoingWebhookURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.Scheme == "" {
		return fmt.Errorf("invalid webhook url")
	}

	ips, err := net.LookupIP(parsed.Hostname())
	if err != nil {
		return fmt.Errorf("resolve webhook host: %w", err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() {
			return fmt.Errorf("private webhook hosts are not allowed")
		}
	}

	return nil
}
