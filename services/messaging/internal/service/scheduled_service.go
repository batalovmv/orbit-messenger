package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// ScheduledMessageService handles business logic for scheduled messages.
type ScheduledMessageService struct {
	scheduled store.ScheduledMessageStore
	messages  store.MessageStore
	polls     store.PollStore
	chats     store.ChatStore
	nats      Publisher
	logger    *slog.Logger
}

type ScheduleMessageInput struct {
	Content     string
	Entities    json.RawMessage
	ReplyToID   *uuid.UUID
	Type        string
	MediaIDs    []uuid.UUID
	IsSpoiler   bool
	Poll        *model.ScheduledPollPayload
	ScheduledAt time.Time
}

// NewScheduledMessageService creates a new ScheduledMessageService.
func NewScheduledMessageService(
	scheduled store.ScheduledMessageStore,
	messages store.MessageStore,
	polls store.PollStore,
	chats store.ChatStore,
	nats Publisher,
	logger *slog.Logger,
) *ScheduledMessageService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ScheduledMessageService{
		scheduled: scheduled,
		messages:  messages,
		polls:     polls,
		chats:     chats,
		nats:      nats,
		logger:    logger,
	}
}

// Schedule creates a new scheduled message.
func (s *ScheduledMessageService) Schedule(
	ctx context.Context,
	chatID, senderID uuid.UUID,
	input ScheduleMessageInput,
) (*model.ScheduledMessage, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, senderID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	if !input.ScheduledAt.After(time.Now()) {
		return nil, apperror.BadRequest("scheduled_at must be in the future")
	}
	if input.Type == "" {
		input.Type = "text"
	}
	if err := validateScheduledMessageInput(input); err != nil {
		return nil, err
	}

	msg := &model.ScheduledMessage{
		ChatID:      chatID,
		SenderID:    senderID,
		Content:     strPtrOrNil(input.Content),
		Entities:    input.Entities,
		ReplyToID:   input.ReplyToID,
		Type:        input.Type,
		MediaIDs:    input.MediaIDs,
		IsSpoiler:   input.IsSpoiler,
		PollPayload: input.Poll,
		ScheduledAt: input.ScheduledAt,
	}
	if input.Poll != nil && msg.Content == nil {
		msg.Content = strPtrOrNil(strings.TrimSpace(input.Poll.Question))
	}
	if err := s.scheduled.Create(ctx, msg); err != nil {
		return nil, fmt.Errorf("create scheduled message: %w", err)
	}

	stored, err := s.scheduled.GetByID(ctx, msg.ID)
	if err != nil {
		return nil, fmt.Errorf("reload scheduled message: %w", err)
	}
	if stored != nil {
		return stored, nil
	}

	return msg, nil
}

// ListScheduled returns pending scheduled messages for a chat.
func (s *ScheduledMessageService) ListScheduled(ctx context.Context, chatID, userID uuid.UUID) ([]model.ScheduledMessage, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	msgs, err := s.scheduled.ListByChat(ctx, chatID, userID)
	if err != nil {
		return nil, fmt.Errorf("list scheduled messages: %w", err)
	}
	return msgs, nil
}

// Edit updates a scheduled message.
func (s *ScheduledMessageService) Edit(ctx context.Context, msgID, userID uuid.UUID, content *string, entities json.RawMessage, scheduledAt *time.Time) (*model.ScheduledMessage, error) {
	msg, err := s.scheduled.GetByID(ctx, msgID)
	if err != nil {
		return nil, fmt.Errorf("get scheduled message: %w", err)
	}
	if msg == nil {
		return nil, apperror.NotFound("Scheduled message not found")
	}
	if msg.SenderID != userID {
		return nil, apperror.Forbidden("You can only edit your own scheduled messages")
	}
	if msg.IsSent {
		return nil, apperror.BadRequest("Scheduled message has already been sent")
	}
	if content != nil && strings.TrimSpace(*content) == "" {
		return nil, apperror.BadRequest("Content is required")
	}
	if scheduledAt != nil && !scheduledAt.After(time.Now()) {
		return nil, apperror.BadRequest("scheduled_at must be in the future")
	}

	if err := s.scheduled.Update(ctx, msgID, content, entities, scheduledAt); err != nil {
		return nil, fmt.Errorf("update scheduled message: %w", err)
	}

	if content != nil {
		msg.Content = content
	}
	if entities != nil {
		msg.Entities = entities
	}
	if scheduledAt != nil {
		msg.ScheduledAt = *scheduledAt
	}

	updated, err := s.scheduled.GetByID(ctx, msgID)
	if err != nil {
		return nil, fmt.Errorf("reload scheduled message: %w", err)
	}
	if updated != nil {
		return updated, nil
	}

	return msg, nil
}

// Delete deletes a scheduled message.
func (s *ScheduledMessageService) Delete(ctx context.Context, msgID, userID uuid.UUID) error {
	msg, err := s.scheduled.GetByID(ctx, msgID)
	if err != nil {
		return fmt.Errorf("get scheduled message: %w", err)
	}
	if msg == nil {
		return apperror.NotFound("Scheduled message not found")
	}
	if msg.SenderID != userID {
		return apperror.Forbidden("You can only delete your own scheduled messages")
	}
	if msg.IsSent {
		return apperror.BadRequest("Scheduled message has already been sent")
	}

	if err := s.scheduled.Delete(ctx, msgID, userID); err != nil {
		return fmt.Errorf("delete scheduled message: %w", err)
	}
	return nil
}

// SendNow immediately sends a scheduled message.
func (s *ScheduledMessageService) SendNow(ctx context.Context, msgID, userID uuid.UUID) (*model.Message, error) {
	msg, err := s.scheduled.GetByID(ctx, msgID)
	if err != nil {
		return nil, fmt.Errorf("get scheduled message: %w", err)
	}
	if msg == nil {
		return nil, apperror.NotFound("Scheduled message not found")
	}
	if msg.SenderID != userID {
		return nil, apperror.Forbidden("You can only send your own scheduled messages")
	}
	if msg.IsSent {
		return nil, apperror.BadRequest("Scheduled message has already been sent")
	}

	delivered, err := s.deliver(ctx, *msg)
	if err != nil {
		return nil, err
	}
	if err := s.scheduled.MarkSent(ctx, msgID); err != nil {
		return nil, fmt.Errorf("mark scheduled message sent: %w", err)
	}

	return delivered, nil
}

// DeliverPending finds and delivers all messages whose scheduled_at <= now.
// Called by the cron job every 10 seconds.
func (s *ScheduledMessageService) DeliverPending(ctx context.Context) (int, error) {
	pending, err := s.scheduled.ListPending(ctx, time.Now())
	if err != nil {
		return 0, fmt.Errorf("list pending scheduled messages: %w", err)
	}
	if len(pending) == 0 {
		return 0, nil
	}

	deliveredCount := 0
	var deliveryErrs []error

	for _, scheduledMsg := range pending {
		if _, err := s.deliver(ctx, scheduledMsg); err != nil {
			s.logger.Error("failed to deliver scheduled message", "scheduled_id", scheduledMsg.ID, "chat_id", scheduledMsg.ChatID, "error", err)
			deliveryErrs = append(deliveryErrs, fmt.Errorf("deliver %s: %w", scheduledMsg.ID, err))
			continue
		}

		if err := s.scheduled.MarkSent(ctx, scheduledMsg.ID); err != nil {
			s.logger.Error("failed to mark scheduled message as sent", "scheduled_id", scheduledMsg.ID, "chat_id", scheduledMsg.ChatID, "error", err)
			deliveryErrs = append(deliveryErrs, fmt.Errorf("mark sent %s: %w", scheduledMsg.ID, err))
			continue
		}

		deliveredCount++
	}

	return deliveredCount, errors.Join(deliveryErrs...)
}

func (s *ScheduledMessageService) deliver(ctx context.Context, scheduledMsg model.ScheduledMessage) (*model.Message, error) {
	msgType := scheduledMsg.Type
	if msgType == "" {
		msgType = "text"
	}

	senderID := scheduledMsg.SenderID
	msg := &model.Message{
		ChatID:    scheduledMsg.ChatID,
		SenderID:  &senderID,
		Type:      msgType,
		Content:   scheduledMsg.Content,
		Entities:  scheduledMsg.Entities,
		ReplyToID: scheduledMsg.ReplyToID,
	}

	switch {
	case scheduledMsg.PollPayload != nil:
		if s.polls == nil {
			return nil, apperror.Internal("Poll store is not configured")
		}
		if err := s.messages.Create(ctx, msg); err != nil {
			return nil, fmt.Errorf("create delivered poll message: %w", err)
		}
		poll := buildPollFromScheduledPayload(msg.ID, *scheduledMsg.PollPayload)
		if err := s.polls.Create(ctx, poll); err != nil {
			return nil, fmt.Errorf("create delivered poll: %w", err)
		}
		msg.Poll = poll
	case len(scheduledMsg.MediaIDs) > 0:
		if err := s.messages.CreateWithMedia(ctx, msg, scheduledMsg.MediaIDs, scheduledMsg.IsSpoiler); err != nil {
			return nil, fmt.Errorf("create delivered media message: %w", err)
		}
	default:
		if err := s.messages.Create(ctx, msg); err != nil {
			return nil, fmt.Errorf("create delivered message: %w", err)
		}
	}

	full := msg
	stored, err := s.messages.GetByID(ctx, msg.ID)
	if err != nil {
		s.logger.Warn("failed to load delivered scheduled message", "scheduled_id", scheduledMsg.ID, "message_id", msg.ID, "error", err)
	} else if stored != nil {
		full = stored
	}
	if len(scheduledMsg.MediaIDs) > 0 {
		s.enrichDeliveredMedia(ctx, full)
	}
	if msg.Poll != nil {
		full.Poll = msg.Poll
	}

	memberIDs, err := s.chats.GetMemberIDs(ctx, scheduledMsg.ChatID)
	if err != nil {
		s.logger.Error("failed to get member IDs for scheduled publish", "chat_id", scheduledMsg.ChatID, "error", err)
		memberIDs = nil
	}

	subject := fmt.Sprintf("orbit.chat.%s.message.new", scheduledMsg.ChatID.String())
	s.nats.Publish(subject, "new_message", full, memberIDs, scheduledMsg.SenderID.String())

	return full, nil
}

func validateScheduledMessageInput(input ScheduleMessageInput) error {
	if input.Poll != nil {
		question := strings.TrimSpace(input.Poll.Question)
		if question == "" {
			return apperror.BadRequest("Question is required")
		}
		if len(question) > 4096 {
			return apperror.BadRequest("Content too long (max 4096 characters)")
		}
		if len(input.Poll.Options) < 2 || len(input.Poll.Options) > 10 {
			return apperror.BadRequest("Poll must have between 2 and 10 options")
		}
		for _, option := range input.Poll.Options {
			if strings.TrimSpace(option) == "" {
				return apperror.BadRequest("Poll options cannot be empty")
			}
		}
		if input.Poll.IsQuiz {
			if input.Poll.CorrectOption == nil {
				return apperror.BadRequest("Quiz polls require correct_option")
			}
			if *input.Poll.CorrectOption < 0 || *input.Poll.CorrectOption >= len(input.Poll.Options) {
				return apperror.BadRequest("correct_option is out of range")
			}
		}
		if input.Poll.Solution != nil {
			trimmed := strings.TrimSpace(*input.Poll.Solution)
			if trimmed == "" {
				input.Poll.Solution = nil
				input.Poll.SolutionEntities = nil
			} else {
				input.Poll.Solution = &trimmed
			}
		}
		if !input.Poll.IsQuiz || input.Poll.Solution == nil {
			input.Poll.Solution = nil
			input.Poll.SolutionEntities = nil
		}
		return nil
	}

	if len(input.MediaIDs) > 0 {
		if len(input.Content) > 4096 {
			return apperror.BadRequest("Content too long (max 4096 characters)")
		}
		return nil
	}

	if strings.TrimSpace(input.Content) == "" {
		return apperror.BadRequest("Content is required")
	}
	if len(input.Content) > 4096 {
		return apperror.BadRequest("Content too long (max 4096 characters)")
	}

	return nil
}

func buildPollFromScheduledPayload(messageID uuid.UUID, payload model.ScheduledPollPayload) *model.Poll {
	poll := &model.Poll{
		MessageID:        messageID,
		Question:         strings.TrimSpace(payload.Question),
		IsAnonymous:      payload.IsAnonymous,
		IsMultiple:       payload.IsMultiple,
		IsQuiz:           payload.IsQuiz,
		CorrectOption:    payload.CorrectOption,
		Solution:         payload.Solution,
		SolutionEntities: payload.SolutionEntities,
		Options:          make([]model.PollOption, 0, len(payload.Options)),
	}

	for i, option := range payload.Options {
		poll.Options = append(poll.Options, model.PollOption{
			Text:     strings.TrimSpace(option),
			Position: i,
		})
	}

	return poll
}

func (s *ScheduledMessageService) enrichDeliveredMedia(ctx context.Context, msg *model.Message) {
	if msg == nil {
		return
	}

	mediaMap, err := s.messages.GetMediaByMessageIDs(ctx, []uuid.UUID{msg.ID})
	if err != nil {
		s.logger.Warn("failed to load delivered scheduled media", "message_id", msg.ID, "error", err)
		return
	}

	if attachments, ok := mediaMap[msg.ID]; ok {
		msg.MediaAttachments = attachments
	}
}
