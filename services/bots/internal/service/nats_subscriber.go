package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/bots/internal/botapi"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/store"
	"github.com/nats-io/nats.go"
)

// BotFatherInterceptor is implemented by botfather.BotFather to handle DM messages.
type BotFatherInterceptor interface {
	HandleIfBotFatherDM(ctx context.Context, chatID uuid.UUID, senderID string, content string, messageType string) bool
	UserID() uuid.UUID
}

type BotNATSSubscriber struct {
	nc            *nats.Conn
	installations store.InstallationStore
	webhookWorker *WebhookWorker
	updateQueue   *UpdateQueue
	botFather     BotFatherInterceptor
	logger        *slog.Logger
}

type natsEvent struct {
	Event     string          `json:"event"`
	Data      json.RawMessage `json:"data"`
	MemberIDs []string        `json:"member_ids"`
	SenderID  string          `json:"sender_id,omitempty"`
	Timestamp string          `json:"timestamp"`
}

func NewBotNATSSubscriber(
	nc *nats.Conn,
	installations store.InstallationStore,
	webhookWorker *WebhookWorker,
	updateQueue *UpdateQueue,
	logger *slog.Logger,
) *BotNATSSubscriber {
	return &BotNATSSubscriber{
		nc:            nc,
		installations: installations,
		webhookWorker: webhookWorker,
		updateQueue:   updateQueue,
		logger:        logger,
	}
}

// SetBotFather sets the BotFather interceptor for DM message handling.
func (s *BotNATSSubscriber) SetBotFather(bf BotFatherInterceptor) {
	s.botFather = bf
}

func (s *BotNATSSubscriber) Start() error {
	if s.nc == nil {
		return fmt.Errorf("nats connection is not configured")
	}

	if _, err := s.nc.Subscribe("orbit.chat.*.message.new", s.handleEvent); err != nil {
		return fmt.Errorf("subscribe message events: %w", err)
	}
	if _, err := s.nc.Subscribe("orbit.chat.*.member.*", s.handleEvent); err != nil {
		return fmt.Errorf("subscribe member events: %w", err)
	}

	return nil
}

func (s *BotNATSSubscriber) handleEvent(msg *nats.Msg) {
	var event natsEvent
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		s.logger.Error("failed to decode bot nats event", "error", err, "subject", msg.Subject)
		return
	}

	chatID := extractChatID(msg.Subject)
	if chatID == uuid.Nil {
		s.logger.Warn("failed to extract chat id from subject", "subject", msg.Subject)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// BotFather interception: handle DM messages before normal bot delivery.
	// Check if BotFather's userID is in the event's member_ids list (meaning it's a participant in this chat).
	if s.botFather != nil && strings.HasSuffix(msg.Subject, ".message.new") {
		bfUID := s.botFather.UserID().String()
		isBFChat := false
		for _, mid := range event.MemberIDs {
			if mid == bfUID {
				isBFChat = true
				break
			}
		}
		if isBFChat {
			var payload struct {
				SenderID string `json:"sender_id"`
				Content  string `json:"content"`
				Type     string `json:"type"`
			}
			if err := json.Unmarshal(event.Data, &payload); err == nil {
				senderID := payload.SenderID
				if senderID == "" {
					senderID = event.SenderID
				}
				msgType := payload.Type
				if msgType == "" {
					msgType = "text"
				}
				if s.botFather.HandleIfBotFatherDM(ctx, chatID, senderID, payload.Content, msgType) {
					return
				}
			}
		}
	}

	bots, err := s.installations.ListChatsWithWebhookBots(ctx, chatID)
	if err != nil {
		s.logger.Error("failed to list installed bots for chat", "chat_id", chatID, "error", err)
		return
	}

	update := buildBotUpdate(chatID, event)
	for _, info := range bots {
		if event.SenderID != "" && info.UserID.String() == event.SenderID {
			continue
		}

		// Enforce scopes: only deliver message events to bots with ScopeReadMessages
		if info.Scopes&model.ScopeReadMessages == 0 {
			continue
		}

		if info.WebhookURL != "" {
			secretHash := ""
			if info.SecretHash != nil {
				secretHash = *info.SecretHash
			}
			if err := s.webhookWorker.Enqueue(info.BotID, info.WebhookURL, secretHash, update); err != nil {
				s.logger.Error("enqueue webhook delivery failed", "bot_id", info.BotID, "error", err)
			}
			continue
		}

		if err := s.updateQueue.Push(info.BotID, update); err != nil {
			s.logger.Error("push bot update failed", "bot_id", info.BotID, "error", err)
		}
	}
}

func extractChatID(subject string) uuid.UUID {
	parts := strings.Split(subject, ".")
	if len(parts) < 3 {
		return uuid.Nil
	}
	chatID, err := uuid.Parse(parts[2])
	if err != nil {
		return uuid.Nil
	}
	return chatID
}

func buildBotUpdate(chatID uuid.UUID, event natsEvent) botapi.Update {
	message := &botapi.APIMessage{
		MessageID: uuid.NewString(),
		ChatID:    chatID.String(),
		FromID:    event.SenderID,
		Text:      event.Event,
		Date:      time.Now().Unix(),
	}

	var payload struct {
		ID         string `json:"id"`
		ChatID     string `json:"chat_id"`
		SenderID   string `json:"sender_id"`
		SenderName string `json:"sender_name"`
		Content    string `json:"content"`
		CreatedAt  string `json:"created_at"`
	}
	if err := json.Unmarshal(event.Data, &payload); err == nil {
		if payload.ID != "" {
			message.MessageID = payload.ID
		}
		if payload.ChatID != "" {
			message.ChatID = payload.ChatID
		}
		if payload.SenderID != "" {
			message.FromID = payload.SenderID
		}
		if payload.SenderName != "" {
			message.FromName = payload.SenderName
		}
		if payload.Content != "" {
			message.Text = payload.Content
		}
		if payload.CreatedAt != "" {
			if createdAt, err := time.Parse(time.RFC3339, payload.CreatedAt); err == nil {
				message.Date = createdAt.Unix()
			}
		}
	}

	return botapi.Update{Message: message}
}
