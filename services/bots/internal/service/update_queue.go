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

// atomicPopScript atomically reads and removes up to N items from a list.
// KEYS[1] = list key, ARGV[1] = count
var atomicPopScript = redis.NewScript(`
local items = redis.call('LRANGE', KEYS[1], 0, ARGV[1] - 1)
if #items > 0 then
  redis.call('LTRIM', KEYS[1], #items, -1)
end
return items
`)

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

	// Atomic pop via Lua script — prevents race between LRANGE and LTRIM
	items, err := atomicPopScript.Run(ctx, q.redis, []string{key}, remaining).StringSlice()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("atomic pop bot updates: %w", err)
	}

	for _, item := range items {
		update, err := decodeUpdate(item)
		if err != nil {
			return nil, err
		}
		updates = append(updates, update)
	}

	return updates, nil
}

// ackScript atomically removes all updates with update_id < offset.
// KEYS[1] = list key, ARGV[1] = offset, ARGV[2] = TTL seconds
var ackScript = redis.NewScript(`
local items = redis.call('LRANGE', KEYS[1], 0, -1)
if #items == 0 then return 0 end
redis.call('DEL', KEYS[1])
local kept = 0
for _, item in ipairs(items) do
  local update = cjson.decode(item)
  if update.update_id >= tonumber(ARGV[1]) then
    redis.call('RPUSH', KEYS[1], item)
    kept = kept + 1
  end
end
if kept > 0 then
  redis.call('EXPIRE', KEYS[1], tonumber(ARGV[2]))
end
return kept
`)

func (q *UpdateQueue) Ack(botID uuid.UUID, offset int64) error {
	if q.redis == nil {
		return fmt.Errorf("redis is not configured")
	}
	if offset <= 0 {
		return nil
	}

	ctx := context.Background()
	key := q.queueKey(botID)
	ttlSeconds := int(botUpdatesTTL.Seconds())

	if err := ackScript.Run(ctx, q.redis, []string{key}, offset, ttlSeconds).Err(); err != nil && err != redis.Nil {
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
