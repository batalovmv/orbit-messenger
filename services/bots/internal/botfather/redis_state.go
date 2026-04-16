package botfather

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const stateTTL = 10 * time.Minute

type RedisStateStore struct {
	rdb *redis.Client
}

func NewRedisStateStore(rdb *redis.Client) *RedisStateStore {
	return &RedisStateStore{rdb: rdb}
}

func stateKey(userID uuid.UUID) string {
	return fmt.Sprintf("botfather:state:%s", userID)
}

func dmCacheKey(chatID uuid.UUID) string {
	return fmt.Sprintf("botfather:dm:%s", chatID)
}

func (s *RedisStateStore) GetState(ctx context.Context, userID uuid.UUID) (*ConversationState, error) {
	data, err := s.rdb.Get(ctx, stateKey(userID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return &ConversationState{Step: StepNone}, nil
		}
		return nil, fmt.Errorf("get botfather state: %w", err)
	}

	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return &ConversationState{Step: StepNone}, nil
	}
	return &state, nil
}

func (s *RedisStateStore) SetState(ctx context.Context, userID uuid.UUID, state *ConversationState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal botfather state: %w", err)
	}
	return s.rdb.Set(ctx, stateKey(userID), data, stateTTL).Err()
}

func (s *RedisStateStore) ClearState(ctx context.Context, userID uuid.UUID) error {
	return s.rdb.Del(ctx, stateKey(userID)).Err()
}

func (s *RedisStateStore) CacheDMChat(ctx context.Context, chatID uuid.UUID) error {
	return s.rdb.Set(ctx, dmCacheKey(chatID), "1", 24*time.Hour).Err()
}

func (s *RedisStateStore) IsCachedDM(ctx context.Context, chatID uuid.UUID) (bool, error) {
	val, err := s.rdb.Get(ctx, dmCacheKey(chatID)).Result()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, fmt.Errorf("check botfather dm cache: %w", err)
	}
	return val == "1", nil
}
