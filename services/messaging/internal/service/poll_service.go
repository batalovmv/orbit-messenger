package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// PollService handles business logic for polls.
type PollService struct {
	polls    store.PollStore
	messages store.MessageStore
	chats    store.ChatStore
	nats     Publisher
	logger   *slog.Logger
}

// NewPollService creates a new PollService.
func NewPollService(
	polls store.PollStore,
	messages store.MessageStore,
	chats store.ChatStore,
	nats Publisher,
	logger *slog.Logger,
) *PollService {
	return &PollService{
		polls:    polls,
		messages: messages,
		chats:    chats,
		nats:     nats,
		logger:   logger,
	}
}

// CreatePoll creates a poll message in a chat.
func (s *PollService) CreatePoll(
	ctx context.Context,
	chatID, senderID uuid.UUID,
	question string,
	options []string,
	isAnonymous, isMultiple, isQuiz bool,
	correctOption *int,
	solution *string,
	solutionEntities json.RawMessage,
) (*model.Poll, *model.Message, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, nil, apperror.BadRequest("Question is required")
	}
	if len(options) < 2 || len(options) > 10 {
		return nil, nil, apperror.BadRequest("Poll must have between 2 and 10 options")
	}
	if isQuiz {
		if correctOption == nil {
			return nil, nil, apperror.BadRequest("Quiz polls require correct_option")
		}
		if *correctOption < 0 || *correctOption >= len(options) {
			return nil, nil, apperror.BadRequest("correct_option is out of range")
		}
	}
	if !isQuiz {
		solution = nil
		solutionEntities = nil
	}
	if solution != nil {
		trimmed := strings.TrimSpace(*solution)
		if trimmed == "" {
			solution = nil
			solutionEntities = nil
		} else {
			solution = &trimmed
		}
	}
	if solution == nil {
		solutionEntities = nil
	}

	pollOptions := make([]model.PollOption, 0, len(options))
	for i, option := range options {
		option = strings.TrimSpace(option)
		if option == "" {
			return nil, nil, apperror.BadRequest("Poll options cannot be empty")
		}
		pollOptions = append(pollOptions, model.PollOption{
			Text:     option,
			Position: i,
		})
	}

	isMember, _, err := s.chats.IsMember(ctx, chatID, senderID)
	if err != nil {
		return nil, nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, nil, apperror.Forbidden("Not a member of this chat")
	}

	chat, err := s.chats.GetByID(ctx, chatID)
	if err != nil {
		return nil, nil, fmt.Errorf("get chat: %w", err)
	}
	if chat == nil {
		return nil, nil, apperror.NotFound("Chat not found")
	}

	member, err := s.chats.GetMember(ctx, chatID, senderID)
	if err != nil {
		return nil, nil, fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return nil, nil, apperror.Forbidden("Not a member of this chat")
	}
	defaultPerms := chat.DefaultPermissions
	if defaultPerms == 0 && member.Permissions == permissions.PermissionsUnset {
		defaultPerms = permissions.DefaultGroupPermissions
	}
	if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, defaultPerms, permissions.CanSendMessages) {
		return nil, nil, apperror.Forbidden("You don't have permission to send messages")
	}

	msg := &model.Message{
		ChatID:   chatID,
		SenderID: &senderID,
		Type:     "poll",
		Content:  &question,
	}
	if err := s.messages.Create(ctx, msg); err != nil {
		return nil, nil, fmt.Errorf("create poll message: %w", err)
	}

	poll := &model.Poll{
		MessageID:        msg.ID,
		Question:         question,
		IsAnonymous:      isAnonymous,
		IsMultiple:       isMultiple,
		IsQuiz:           isQuiz,
		CorrectOption:    correctOption,
		Solution:         solution,
		SolutionEntities: solutionEntities,
		Options:          pollOptions,
	}
	if err := s.polls.Create(ctx, poll); err != nil {
		return nil, nil, fmt.Errorf("create poll: %w", err)
	}

	storedMsg, err := s.messages.GetByID(ctx, msg.ID)
	if err == nil && storedMsg != nil {
		msg = storedMsg
	}
	msg.Poll = poll

	s.publishPollMessage(ctx, chatID, senderID, msg)

	return poll, msg, nil
}

// Vote records a user's vote on a poll option.
func (s *PollService) Vote(ctx context.Context, messageID, userID uuid.UUID, optionIDs []uuid.UUID) (*model.Poll, error) {
	if len(optionIDs) == 0 {
		return nil, apperror.BadRequest("At least one option is required")
	}

	poll, err := s.polls.GetByMessageID(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("get poll by message: %w", err)
	}
	if poll == nil {
		return nil, apperror.NotFound("Poll not found")
	}
	if poll.IsClosed {
		return nil, apperror.BadRequest("Poll is closed")
	}
	if !poll.IsMultiple && len(optionIDs) > 1 {
		return nil, apperror.BadRequest("Poll does not allow multiple answers")
	}

	validOptions := make(map[uuid.UUID]struct{}, len(poll.Options))
	for _, option := range poll.Options {
		validOptions[option.ID] = struct{}{}
	}

	uniqueOptionIDs := make([]uuid.UUID, 0, len(optionIDs))
	seen := make(map[uuid.UUID]struct{}, len(optionIDs))
	for _, optionID := range optionIDs {
		if _, ok := validOptions[optionID]; !ok {
			return nil, apperror.BadRequest("Invalid option ID")
		}
		if _, ok := seen[optionID]; ok {
			continue
		}
		seen[optionID] = struct{}{}
		uniqueOptionIDs = append(uniqueOptionIDs, optionID)
	}

	msg, err := s.messages.GetByID(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("get poll message: %w", err)
	}
	if msg == nil {
		return nil, apperror.NotFound("Message not found")
	}

	isMember, _, err := s.chats.IsMember(ctx, msg.ChatID, userID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	if err := s.polls.VoteAtomic(ctx, poll.ID, userID, uniqueOptionIDs, poll.IsMultiple); err != nil {
		return nil, fmt.Errorf("atomic vote: %w", err)
	}

	updated, err := s.polls.GetByID(ctx, poll.ID)
	if err != nil {
		return nil, fmt.Errorf("refresh poll: %w", err)
	}
	if updated == nil {
		updated = poll
	}

	if err := s.hydratePollChoices(ctx, userID, updated); err != nil {
		return nil, err
	}

	s.publishPollUpdate(ctx, msg, userID, "poll_vote", updated, uniqueOptionIDs)

	return updated, nil
}

// Unvote removes a user's votes from a poll.
func (s *PollService) Unvote(ctx context.Context, messageID, userID uuid.UUID) (*model.Poll, error) {
	poll, err := s.polls.GetByMessageID(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("get poll by message: %w", err)
	}
	if poll == nil {
		return nil, apperror.NotFound("Poll not found")
	}
	if poll.IsClosed {
		return nil, apperror.BadRequest("Poll is closed")
	}

	msg, err := s.messages.GetByID(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("get poll message: %w", err)
	}
	if msg == nil {
		return nil, apperror.NotFound("Message not found")
	}

	isMember, _, err := s.chats.IsMember(ctx, msg.ChatID, userID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	if err := s.polls.UnvoteAll(ctx, poll.ID, userID); err != nil {
		return nil, fmt.Errorf("remove votes: %w", err)
	}

	updated, err := s.polls.GetByID(ctx, poll.ID)
	if err != nil {
		return nil, fmt.Errorf("refresh poll: %w", err)
	}
	if updated == nil {
		updated = poll
	}

	if err := s.hydratePollChoices(ctx, userID, updated); err != nil {
		return nil, err
	}

	s.publishPollUpdate(ctx, msg, userID, "poll_vote", updated, nil)

	return updated, nil
}

// ClosePoll closes a poll (creator or admin only).
func (s *PollService) ClosePoll(ctx context.Context, messageID, userID uuid.UUID) (*model.Poll, error) {
	poll, err := s.polls.GetByMessageID(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("get poll by message: %w", err)
	}
	if poll == nil {
		return nil, apperror.NotFound("Poll not found")
	}
	if poll.IsClosed {
		return nil, apperror.BadRequest("Poll is already closed")
	}

	msg, err := s.messages.GetByID(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("get poll message: %w", err)
	}
	if msg == nil {
		return nil, apperror.NotFound("Message not found")
	}

	isCreator := msg.SenderID != nil && *msg.SenderID == userID
	if !isCreator {
		member, err := s.chats.GetMember(ctx, msg.ChatID, userID)
		if err != nil {
			return nil, fmt.Errorf("get chat member: %w", err)
		}
		if member == nil || !permissions.IsAdminOrOwner(member.Role) {
			return nil, apperror.Forbidden("Only the poll creator or a chat admin can close the poll")
		}
	}

	if err := s.polls.Close(ctx, poll.ID); err != nil {
		return nil, fmt.Errorf("close poll: %w", err)
	}

	updated, err := s.polls.GetByID(ctx, poll.ID)
	if err != nil {
		return nil, fmt.Errorf("refresh poll: %w", err)
	}
	if updated == nil {
		poll.IsClosed = true
		updated = poll
	}

	if err := s.hydratePollChoices(ctx, userID, updated); err != nil {
		return nil, err
	}

	s.publishPollUpdate(ctx, msg, userID, "poll_closed", updated, nil)

	return updated, nil
}

// GetPollVoters returns voters for a specific poll option (non-anonymous only).
func (s *PollService) GetPollVoters(
	ctx context.Context,
	messageID, userID uuid.UUID,
	optionID uuid.UUID,
	limit int,
	cursor string,
) ([]model.PollVote, string, bool, error) {
	poll, err := s.polls.GetByMessageID(ctx, messageID)
	if err != nil {
		return nil, "", false, fmt.Errorf("get poll by message: %w", err)
	}
	if poll == nil {
		return nil, "", false, apperror.NotFound("Poll not found")
	}
	if poll.IsAnonymous {
		return nil, "", false, apperror.BadRequest("Anonymous poll voters are not available")
	}

	validOption := false
	for _, option := range poll.Options {
		if option.ID == optionID {
			validOption = true
			break
		}
	}
	if !validOption {
		return nil, "", false, apperror.BadRequest("Invalid option ID")
	}

	msg, err := s.messages.GetByID(ctx, messageID)
	if err != nil {
		return nil, "", false, fmt.Errorf("get poll message: %w", err)
	}
	if msg == nil {
		return nil, "", false, apperror.NotFound("Message not found")
	}

	isMember, _, err := s.chats.IsMember(ctx, msg.ChatID, userID)
	if err != nil {
		return nil, "", false, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, "", false, apperror.Forbidden("Not a member of this chat")
	}

	if limit <= 0 || limit > 100 {
		limit = 50
	}

	voters, nextCursor, hasMore, err := s.polls.GetVoters(ctx, poll.ID, optionID, limit, cursor)
	if err != nil {
		return nil, "", false, fmt.Errorf("get poll voters: %w", err)
	}
	return voters, nextCursor, hasMore, nil
}

func (s *PollService) HydrateMessagePolls(ctx context.Context, userID uuid.UUID, msgs []model.Message) error {
	if len(msgs) == 0 {
		return nil
	}

	messageIDs := make([]uuid.UUID, 0, len(msgs))
	for i := range msgs {
		if msgs[i].Type == "poll" {
			messageIDs = append(messageIDs, msgs[i].ID)
		}
	}
	if len(messageIDs) == 0 {
		return nil
	}

	pollsByMessageID, err := s.polls.ListByMessageIDs(ctx, messageIDs)
	if err != nil {
		return fmt.Errorf("list polls by message IDs: %w", err)
	}
	if len(pollsByMessageID) == 0 {
		return nil
	}

	pollIDs := make([]uuid.UUID, 0, len(pollsByMessageID))
	for _, poll := range pollsByMessageID {
		pollIDs = append(pollIDs, poll.ID)
	}

	userVotesByPollID, err := s.polls.ListUserVotesByPollIDs(ctx, pollIDs, userID)
	if err != nil {
		return fmt.Errorf("list user poll votes by poll IDs: %w", err)
	}

	for i := range msgs {
		poll := pollsByMessageID[msgs[i].ID]
		if poll == nil {
			continue
		}

		applyPollChoiceState(poll, userVotesByPollID[poll.ID])
		msgs[i].Poll = poll
	}

	return nil
}

func (s *PollService) publishPollMessage(ctx context.Context, chatID, senderID uuid.UUID, msg *model.Message) {
	memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
	if err != nil {
		s.logError("failed to get member IDs for poll message publish", "chat_id", chatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.new", chatID.String())
	s.nats.Publish(subject, "new_message", msg, memberIDs, senderID.String())
}

func (s *PollService) publishPollUpdate(
	ctx context.Context,
	msg *model.Message,
	senderID uuid.UUID,
	event string,
	poll *model.Poll,
	optionIDs []uuid.UUID,
) {
	memberIDs, err := s.chats.GetMemberIDs(ctx, msg.ChatID)
	if err != nil {
		s.logError("failed to get member IDs for poll publish", "chat_id", msg.ChatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.updated", msg.ChatID.String())
	payload := fiberMapFromPollEvent(msg, senderID, poll, optionIDs)
	s.nats.Publish(subject, event, payload, memberIDs, senderID.String())
}

func (s *PollService) logError(msg string, args ...any) {
	if s.logger != nil {
		s.logger.Error(msg, args...)
		return
	}
	slog.Error(msg, args...)
}

func (s *PollService) hydratePollChoices(ctx context.Context, userID uuid.UUID, poll *model.Poll) error {
	if poll == nil {
		return nil
	}

	votes, err := s.polls.GetUserVotes(ctx, poll.ID, userID)
	if err != nil {
		return fmt.Errorf("get user poll votes: %w", err)
	}

	applyPollChoiceState(poll, votes)
	return nil
}

func applyPollChoiceState(poll *model.Poll, optionIDs []uuid.UUID) {
	if poll == nil {
		return
	}

	chosen := make(map[uuid.UUID]struct{}, len(optionIDs))
	for _, optionID := range optionIDs {
		chosen[optionID] = struct{}{}
	}

	for i := range poll.Options {
		_, isChosen := chosen[poll.Options[i].ID]
		poll.Options[i].IsChosen = isChosen
		poll.Options[i].IsCorrect = poll.CorrectOption != nil &&
			poll.Options[i].Position == *poll.CorrectOption &&
			(poll.IsClosed || (poll.IsQuiz && len(optionIDs) > 0) || isChosen)
	}
}

func fiberMapFromPollEvent(msg *model.Message, userID uuid.UUID, poll *model.Poll, optionIDs []uuid.UUID) map[string]any {
	payload := map[string]any{
		"chat_id":    msg.ChatID.String(),
		"message_id": msg.ID.String(),
		"poll":       poll,
	}
	if msg.SequenceNumber > 0 {
		payload["sequence_number"] = msg.SequenceNumber
	}
	if userID != uuid.Nil {
		payload["user_id"] = userID.String()
	}
	if len(optionIDs) > 0 {
		stringOptionIDs := make([]string, 0, len(optionIDs))
		for _, optionID := range optionIDs {
			stringOptionIDs = append(stringOptionIDs, optionID.String())
		}
		payload["option_ids"] = stringOptionIDs
	}

	return payload
}
