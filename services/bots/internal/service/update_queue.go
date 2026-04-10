package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/bots/internal/botapi"
	"github.com/redis/go-redis/v9"
)

const (
	botUpdatesTTL       = 24 * time.Hour
	botUpdatesMaxLength = 100
)

type UpdateQueue struct {
	redis *redis.Client
}

func NewUpdateQueue(redis *redis.Client) *UpdateQueue {
	return &UpdateQueue{redis: redis}
}

func (q *UpdateQueue) Push(botID uuid.UUID, update botapi.Update) error {
	if q.redis == nil {
		return fmt.Errorf("redis is not configured")
	}

	ctx := context.Background()
	key := q.queueKey(botID)
	seqKey := q.seqKey(botID)

	updateID, err := q.redis.Incr(ctx, seqKey).Result()
	if err != nil {
		return fmt.Errorf("increment bot update sequence: %w", err)
	}
	update.UpdateID = updateID

	payload, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("marshal bot update: %w", err)
	}

	pipe := q.redis.TxPipeline()
	pipe.RPush(ctx, key, payload)
	pipe.LTrim(ctx, key, -botUpdatesMaxLength, -1)
	pipe.Expire(ctx, key, botUpdatesTTL)
	pipe.Expire(ctx, seqKey, botUpdatesTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("push bot update: %w", err)
	}

	return nil
}

func (q *UpdateQueue) Pop(ctx context.Context, botID uuid.UUID, limit int, timeout time.Duration) ([]botapi.Update, error) {
	if q.redis == nil {
		return nil, fmt.Errorf("redis is not configured")
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	key := q.queueKey(botID)
	updates := make([]botapi.Update, 0, limit)

	if timeout > 0 {
		result, err := q.redis.BLPop(ctx, timeout, key).Result()
		if err == redis.Nil {
			return updates, nil
		}
		if err != nil {
			return nil, fmt.Errorf("blpop bot updates: %w", err)
		}
		if len(result) == 2 {
			update, err := decodeUpdate(result[1])
			if err != nil {
				return nil, err
			}
			updates = append(updates, update)
		}
	}

	remaining := limit - len(updates)
	if remaining <= 0 {
		return updates, nil
	}

	items, err := q.redis.LRange(ctx, key, 0, int64(remaining-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("lrange bot updates: %w", err)
	}
	if len(items) == 0 {
		return updates, nil
	}

	for _, item := range items {
		update, err := decodeUpdate(item)
		if err != nil {
			return nil, err
		}
		updates = append(updates, update)
	}

	if err := q.redis.LTrim(ctx, key, int64(len(items)), -1).Err(); err != nil {
		return nil, fmt.Errorf("ltrim bot updates: %w", err)
	}

	return updates, nil
}

func (q *UpdateQueue) Ack(botID uuid.UUID, offset int64) error {
	if q.redis == nil {
		return fmt.Errorf("redis is not configured")
	}
	if offset <= 0 {
		return nil
	}

	ctx := context.Background()
	key := q.queueKey(botID)

	items, err := q.redis.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return fmt.Errorf("read bot updates for ack: %w", err)
	}
	if len(items) == 0 {
		return nil
	}

	remaining := make([]string, 0, len(items))
	for _, item := range items {
		update, err := decodeUpdate(item)
		if err != nil {
			return err
		}
		if update.UpdateID >= offset {
			remaining = append(remaining, item)
		}
	}

	pipe := q.redis.TxPipeline()
	pipe.Del(ctx, key)
	if len(remaining) > 0 {
		values := make([]any, len(remaining))
		for i, item := range remaining {
			values[i] = item
		}
		pipe.RPush(ctx, key, values...)
		pipe.Expire(ctx, key, botUpdatesTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("ack bot updates: %w", err)
	}

	return nil
}

func (q *UpdateQueue) queueKey(botID uuid.UUID) string {
	return "bot_updates:" + botID.String()
}

func (q *UpdateQueue) seqKey(botID uuid.UUID) string {
	return "bot_update_seq:" + botID.String()
}

func decodeUpdate(value string) (botapi.Update, error) {
	var update botapi.Update
	if err := json.Unmarshal([]byte(value), &update); err != nil {
		return botapi.Update{}, fmt.Errorf("decode bot update: %w", err)
	}
	return update, nil
}
