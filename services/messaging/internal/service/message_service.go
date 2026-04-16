package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
	"github.com/redis/go-redis/v9"
)

type MessageService struct {
	messages     store.MessageStore
	chats        store.ChatStore
	blockedStore store.BlockedUsersStore
	audit        store.AuditStore
	nats         Publisher
	redis        *redis.Client

	// @orbit-ai mention bot — populated from env at startup. When
	// either field is empty the feature is dormant: mention detection
	// still runs but the async AI call is skipped.
	aiBotUserID      *uuid.UUID
	aiServiceURL     string
	aiInternalToken  string
	aiHTTPClient     *http.Client
}

func NewMessageService(messages store.MessageStore, chats store.ChatStore, blockedStore store.BlockedUsersStore, nats Publisher, rdb *redis.Client, audit ...store.AuditStore) *MessageService {
	svc := &MessageService{messages: messages, chats: chats, blockedStore: blockedStore, nats: nats, redis: rdb}
	if len(audit) > 0 {
		svc.audit = audit[0]
	}
	return svc
}

// ConfigureOrbitAIBot wires the @orbit-ai mention handler. Called from
// main.go at startup with values pulled from env:
//   ORBIT_AI_BOT_USER_ID  — UUID of the seeded bot user
//   AI_SERVICE_URL        — base URL of the ai microservice (http://ai:8085)
//   INTERNAL_SECRET       — shared gateway token for service-to-service
// Any empty value leaves the feature disabled.
func (s *MessageService) ConfigureOrbitAIBot(botUserID, aiServiceURL, internalToken string) {
	botUserID = strings.TrimSpace(botUserID)
	aiServiceURL = strings.TrimRight(strings.TrimSpace(aiServiceURL), "/")
	internalToken = strings.TrimSpace(internalToken)
	if botUserID == "" || aiServiceURL == "" || internalToken == "" {
		return
	}
	parsed, err := uuid.Parse(botUserID)
	if err != nil {
		slog.Warn("orbit-ai bot disabled: invalid ORBIT_AI_BOT_USER_ID", "value", botUserID)
		return
	}
	s.aiBotUserID = &parsed
	s.aiServiceURL = aiServiceURL
	s.aiInternalToken = internalToken
	s.aiHTTPClient = &http.Client{Timeout: 60 * time.Second}
	slog.Info("orbit-ai mention bot enabled", "bot_user_id", parsed.String())
}

// checkChatAccess verifies membership or privileged access. Returns true if access is granted.
// For privileged access, it writes an audit log entry (fail-closed).
func (s *MessageService) checkChatAccess(ctx context.Context, chatID, userID uuid.UUID, userRole string) (bool, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	if isMember {
		return true, nil
	}

	// Fallback: privileged access via system permissions
	if permissions.HasSysPermission(userRole, permissions.SysReadAllContent) && s.audit != nil {
		entry := &model.AuditEntry{
			ActorID:    userID,
			Action:     model.AuditChatPrivilegedRead,
			TargetType: "chat",
		}
		targetStr := chatID.String()
		entry.TargetID = &targetStr

		if err := s.audit.Log(ctx, entry); err != nil {
			slog.Error("audit log write failed for privileged access", "error", err, "user_id", userID, "chat_id", chatID)
			return false, apperror.Internal("audit log write failed")
		}
		return true, nil
	}

	return false, nil
}

func (s *MessageService) ListMessages(ctx context.Context, chatID, userID uuid.UUID, userRole, cursor string, limit int) ([]model.Message, string, bool, error) {
	hasAccess, err := s.checkChatAccess(ctx, chatID, userID, userRole)
	if err != nil {
		return nil, "", false, err
	}
	if !hasAccess {
		return nil, "", false, apperror.Forbidden("Not a member of this chat")
	}

	return s.messages.ListByChat(ctx, chatID, cursor, limit)
}

func (s *MessageService) FindByDate(ctx context.Context, chatID, userID uuid.UUID, userRole string, date time.Time, limit int) ([]model.Message, string, bool, error) {
	hasAccess, err := s.checkChatAccess(ctx, chatID, userID, userRole)
	if err != nil {
		return nil, "", false, err
	}
	if !hasAccess {
		return nil, "", false, apperror.Forbidden("Not a member of this chat")
	}

	return s.messages.FindByChatAndDate(ctx, chatID, date, limit)
}

func (s *MessageService) GetMessage(ctx context.Context, msgID, userID uuid.UUID, userRole string) (*model.Message, error) {
	msg, err := s.messages.GetByID(ctx, msgID)
	if err != nil {
		return nil, fmt.Errorf("get message: %w", err)
	}
	if msg == nil || msg.IsDeleted {
		return nil, apperror.NotFound("Message not found")
	}

	hasAccess, err := s.checkChatAccess(ctx, msg.ChatID, userID, userRole)
	if err != nil {
		return nil, err
	}
	if !hasAccess {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	return msg, nil
}

func (s *MessageService) ViewOneTimeMessage(ctx context.Context, msgID, userID uuid.UUID) (*model.Message, error) {
	msg, err := s.messages.MarkOneTimeViewed(ctx, msgID, userID)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, apperror.NotFound("Message not found")
		case errors.Is(err, store.ErrMessageForbidden):
			return nil, apperror.Forbidden("Not a member of this chat")
		case errors.Is(err, store.ErrMessageNotOneTime):
			return nil, apperror.BadRequest("Message is not one-time media")
		default:
			return nil, fmt.Errorf("view one-time message: %w", err)
		}
	}

	s.enrichMessageMedia(ctx, msg)
	s.publishMessageUpdated(ctx, msg)

	return msg, nil
}

// SendMessageOption allows passing optional bot-related fields to SendMessage.
type SendMessageOption func(msg *model.Message)

// WithReplyMarkup attaches inline keyboard markup to the message.
func WithReplyMarkup(markup json.RawMessage) SendMessageOption {
	return func(msg *model.Message) {
		if len(markup) > 0 {
			msg.ReplyMarkup = markup
		}
	}
}

// WithViaBotID marks the message as sent via a bot.
func WithViaBotID(botID uuid.UUID) SendMessageOption {
	return func(msg *model.Message) {
		msg.ViaBotID = &botID
	}
}

func (s *MessageService) SendMessage(ctx context.Context, chatID, senderID uuid.UUID, content string, entities json.RawMessage, replyToID *uuid.UUID, msgType string, opts ...SendMessageOption) (*model.Message, error) {
	chat, err := s.chats.GetByID(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("get chat: %w", err)
	}
	if chat == nil {
		return nil, apperror.NotFound("Chat not found")
	}

	member, err := s.chats.GetMember(ctx, chatID, senderID)
	if err != nil {
		return nil, fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanSendMessages) {
		return nil, apperror.Forbidden("You don't have permission to send messages")
	}

	// Block check: in direct chats, check if either user has blocked the other
	if chat.Type == "direct" && s.blockedStore != nil {
		members, _, _, err := s.chats.GetMembers(ctx, chatID, "", 2)
		if err != nil {
			return nil, fmt.Errorf("get dm members: %w", err)
		}
		for _, m := range members {
			if m.UserID == senderID {
				continue
			}
			// Check if recipient blocked the sender
			blocked, err := s.blockedStore.IsBlocked(ctx, m.UserID, senderID)
			if err != nil {
				return nil, fmt.Errorf("check blocked: %w", err)
			}
			if blocked {
				return nil, apperror.Forbidden("You cannot send messages to this user")
			}
			// Check if sender blocked the recipient
			blocked, err = s.blockedStore.IsBlocked(ctx, senderID, m.UserID)
			if err != nil {
				return nil, fmt.Errorf("check blocked: %w", err)
			}
			if blocked {
				return nil, apperror.Forbidden("You have blocked this user")
			}
		}
	}

	// Slow mode: atomic check-and-set BEFORE creating the message
	if chat.SlowModeSeconds > 0 && !permissions.IsAdminOrOwner(member.Role) {
		redisKey := fmt.Sprintf("slowmode:%s:%s", chatID, senderID)
		ttl := time.Duration(chat.SlowModeSeconds) * time.Second
		wasSet, err := s.redis.SetNX(ctx, redisKey, "1", ttl).Result()
		if err != nil {
			slog.Error("redis slow mode check failed", "error", err)
			return nil, apperror.Internal("Slow mode check temporarily unavailable")
		}
		if !wasSet {
			remaining, ttlErr := s.redis.TTL(ctx, redisKey).Result()
			waitSec := int(remaining.Seconds())
			if ttlErr != nil || waitSec <= 0 {
				waitSec = chat.SlowModeSeconds
			}
			return nil, apperror.TooManyRequests(fmt.Sprintf("Slow mode: wait %d seconds", waitSec))
		}
	}

	if msgType == "" {
		msgType = "text"
	}

	// Validate reply_to_id belongs to the same chat
	if replyToID != nil {
		replyMsg, err := s.messages.GetByID(ctx, *replyToID)
		if err != nil {
			return nil, fmt.Errorf("check reply message: %w", err)
		}
		if replyMsg == nil || replyMsg.IsDeleted {
			return nil, apperror.BadRequest("Reply message not found")
		}
		if replyMsg.ChatID != chatID {
			return nil, apperror.BadRequest("Cannot reply to a message from a different chat")
		}
	}

	msg := &model.Message{
		ChatID:    chatID,
		SenderID:  &senderID,
		Type:      msgType,
		Content:   &content,
		Entities:  entities,
		ReplyToID: replyToID,
	}
	for _, opt := range opts {
		opt(msg)
	}
	if err := s.messages.Create(ctx, msg); err != nil {
		return nil, fmt.Errorf("create message: %w", err)
	}

	// Fetch full message with sender info
	full, err := s.messages.GetByID(ctx, msg.ID)
	if err != nil {
		return msg, nil // Still return the message even if we can't get full info
	}

	// Publish to NATS
	memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", chatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.new", chatID.String())

	s.nats.Publish(subject, "new_message", full, memberIDs, senderID.String())

	// Parse @mention entities and notify mentioned users
	if len(entities) > 0 {
		var ents []struct {
			Type   string `json:"type"`
			UserID string `json:"user_id"`
		}
		if json.Unmarshal(entities, &ents) == nil {
			for _, e := range ents {
				if e.Type == "mention" && e.UserID != "" {
					// Validate user_id is a proper UUID to prevent NATS subject injection
					if _, err := uuid.Parse(e.UserID); err != nil {
						continue
					}
					mentionSubject := fmt.Sprintf("orbit.user.%s.mention", e.UserID)
					s.nats.Publish(mentionSubject, "mention", map[string]interface{}{
						"id":              full.ID.String(),
						"chat_id":         chatID.String(),
						"message_id":      msg.ID.String(),
						"sender_id":       senderID.String(),
						"type":            full.Type,
						"content":         full.Content,
						"sender_name":     full.SenderName,
						"sequence_number": full.SequenceNumber,
					}, []string{e.UserID})
				}
			}
		}
	}

	// @orbit-ai literal mention: when enabled, forward the prompt to the
	// AI service and post the reply as a new message from the bot
	// account. Runs in a goroutine so the caller's send path is not
	// blocked by a Claude round-trip.
	s.maybeHandleOrbitAIMention(chatID, senderID, content)

	return full, nil
}

// maybeHandleOrbitAIMention fires off a background call to the AI
// service when the outgoing message contains `@orbit-ai` and the bot
// is configured. Never called for bot-authored messages (would loop),
// never called for encrypted chats (bot cannot encrypt).
func (s *MessageService) maybeHandleOrbitAIMention(chatID, senderID uuid.UUID, content string) {
	if s.aiBotUserID == nil || s.aiHTTPClient == nil {
		return
	}
	if senderID == *s.aiBotUserID {
		return
	}
	prompt := extractOrbitAIPrompt(content)
	if prompt == "" {
		return
	}
	go s.runOrbitAIMention(chatID, prompt)
}

// extractOrbitAIPrompt finds the first `@orbit-ai` literal mention in
// the message (case-insensitive, delimited by start-of-string /
// whitespace) and returns everything that follows it as the user's
// question. Empty string = no mention or empty prompt.
func extractOrbitAIPrompt(content string) string {
	lower := strings.ToLower(content)
	idx := strings.Index(lower, "@orbit-ai")
	if idx == -1 {
		return ""
	}
	// Require that the mention is not embedded inside a longer word
	// like `@orbit-ai-bot-foo` — must be followed by whitespace, end
	// of string, or punctuation.
	after := idx + len("@orbit-ai")
	if after < len(content) {
		next := content[after]
		if !isOrbitAIBoundary(next) {
			return ""
		}
	}
	prompt := strings.TrimSpace(content[after:])
	return prompt
}

func isOrbitAIBoundary(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', ',', '!', '?', ':', ';':
		return true
	}
	return b > 127 // non-ASCII byte (e.g. Cyrillic, em-dash) — treat as word boundary
}

func (s *MessageService) runOrbitAIMention(chatID uuid.UUID, prompt string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("orbit-ai goroutine panic recovered", "panic", r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	body, err := json.Marshal(map[string]string{
		"chat_id": chatID.String(),
		"prompt":  prompt,
	})
	if err != nil {
		slog.Error("orbit-ai marshal failed", "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.aiServiceURL+"/ai/ask", bytes.NewReader(body))
	if err != nil {
		slog.Error("orbit-ai request build failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", s.aiInternalToken)
	// The AI service derives per-user rate limiting from X-User-ID —
	// charge the bot itself so mentions don't burn the caller's quota.
	req.Header.Set("X-User-ID", s.aiBotUserID.String())

	resp, err := s.aiHTTPClient.Do(req)
	if err != nil {
		slog.Warn("orbit-ai ask call failed", "error", err, "chat_id", chatID.String())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		slog.Warn("orbit-ai ask non-200", "status", resp.StatusCode, "body", string(payload))
		return
	}

	var parsed struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		slog.Warn("orbit-ai ask decode failed", "error", err)
		return
	}
	reply := strings.TrimSpace(parsed.Reply)
	if reply == "" {
		return
	}

	// Send the reply back into the same chat as a normal message from
	// the bot user. Reuses the full SendMessage path so permission /
	// slow-mode / NATS fanout all work without duplication.
	if _, err := s.SendMessage(ctx, chatID, *s.aiBotUserID, reply, nil, nil, "text"); err != nil {
		slog.Warn("orbit-ai reply post failed", "error", err, "chat_id", chatID.String())
	}
}

func (s *MessageService) EditMessage(ctx context.Context, msgID, userID uuid.UUID, content string, entities json.RawMessage, replyMarkup ...json.RawMessage) (*model.Message, error) {
	msg, err := s.messages.GetByID(ctx, msgID)
	if err != nil {
		return nil, fmt.Errorf("get message: %w", err)
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
	if msg.SenderID == nil || *msg.SenderID != userID {
		return nil, apperror.Forbidden("You can only edit your own messages")
	}
	if msg.IsDeleted {
		return nil, apperror.BadRequest("Cannot edit a deleted message")
	}

	msg.Content = &content
	msg.Entities = entities
	if len(replyMarkup) > 0 && len(replyMarkup[0]) > 0 {
		msg.ReplyMarkup = replyMarkup[0]
	}
	if err := s.messages.Update(ctx, msg); err != nil {
		return nil, fmt.Errorf("update message: %w", err)
	}

	// Fetch updated
	updated, err := s.messages.GetByID(ctx, msgID)
	if err != nil {
		return msg, nil
	}

	memberIDs, err := s.chats.GetMemberIDs(ctx, msg.ChatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", msg.ChatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.updated", msg.ChatID.String())
	s.nats.Publish(subject, "message_updated", updated, memberIDs)

	return updated, nil
}

func (s *MessageService) DeleteMessage(ctx context.Context, msgID, userID uuid.UUID) error {
	msg, err := s.messages.GetByID(ctx, msgID)
	if err != nil {
		return fmt.Errorf("get message: %w", err)
	}
	if msg == nil {
		return apperror.NotFound("Message not found")
	}
	isMember, _, err := s.chats.IsMember(ctx, msg.ChatID, userID)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return apperror.Forbidden("Not a member of this chat")
	}

	// Atomic ownership check + soft delete to prevent TOCTOU race
	chatID, seqNum, err := s.messages.SoftDeleteAuthorized(ctx, msgID, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NotFound("Message not found")
		}
		if err.Error() == "forbidden" {
			return apperror.Forbidden("Only the author or chat admin can delete messages")
		}
		return fmt.Errorf("delete message: %w", err)
	}

	memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", chatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.deleted", chatID.String())
	s.nats.Publish(subject, "message_deleted", map[string]interface{}{
		"id":              msgID.String(),
		"chat_id":         chatID.String(),
		"sequence_number": seqNum,
	}, memberIDs)

	return nil
}

func (s *MessageService) ForwardMessages(ctx context.Context, messageIDs []uuid.UUID, toChatID, senderID uuid.UUID) ([]model.Message, error) {
	chat, err := s.chats.GetByID(ctx, toChatID)
	if err != nil {
		return nil, fmt.Errorf("get target chat: %w", err)
	}
	if chat == nil {
		return nil, apperror.NotFound("Target chat not found")
	}

	member, err := s.chats.GetMember(ctx, toChatID, senderID)
	if err != nil {
		return nil, fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return nil, apperror.Forbidden("Not a member of the target chat")
	}

	if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanSendMessages) {
		return nil, apperror.Forbidden("You don't have permission to send messages")
	}

	// Block check in direct chats
	if chat.Type == "direct" && s.blockedStore != nil {
		members, _, _, err := s.chats.GetMembers(ctx, toChatID, "", 2)
		if err != nil {
			return nil, fmt.Errorf("get dm members: %w", err)
		}
		for _, m := range members {
			if m.UserID == senderID {
				continue
			}
			blocked, err := s.blockedStore.IsBlocked(ctx, m.UserID, senderID)
			if err != nil {
				return nil, fmt.Errorf("check blocked: %w", err)
			}
			if blocked {
				return nil, apperror.Forbidden("You cannot send messages to this user")
			}
			blocked, err = s.blockedStore.IsBlocked(ctx, senderID, m.UserID)
			if err != nil {
				return nil, fmt.Errorf("check blocked: %w", err)
			}
			if blocked {
				return nil, apperror.Forbidden("You have blocked this user")
			}
		}
	}

	// Slow mode: atomic check-and-set BEFORE creating the message
	if chat.SlowModeSeconds > 0 && !permissions.IsAdminOrOwner(member.Role) {
		redisKey := fmt.Sprintf("slowmode:%s:%s", toChatID, senderID)
		ttl := time.Duration(chat.SlowModeSeconds) * time.Second
		wasSet, err := s.redis.SetNX(ctx, redisKey, "1", ttl).Result()
		if err != nil {
			slog.Error("redis slow mode check failed", "error", err)
			return nil, apperror.Internal("Slow mode check temporarily unavailable")
		}
		if !wasSet {
			remaining, ttlErr := s.redis.TTL(ctx, redisKey).Result()
			waitSec := int(remaining.Seconds())
			if ttlErr != nil || waitSec <= 0 {
				waitSec = chat.SlowModeSeconds
			}
			return nil, apperror.TooManyRequests(fmt.Sprintf("Slow mode: wait %d seconds", waitSec))
		}
	}

	// Batch fetch all messages in one query (instead of N queries)
	originals, err := s.messages.GetByIDs(ctx, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("batch fetch messages: %w", err)
	}

	// IDOR: check membership in each unique source chat (1 query per unique chat, not per message)
	sourceChatChecked := make(map[uuid.UUID]bool)
	var toForward []model.Message
	var origIDs []uuid.UUID
	for i := range originals {
		orig := &originals[i]
		if orig.IsDeleted {
			continue
		}
		if allowed, checked := sourceChatChecked[orig.ChatID]; checked {
			if !allowed {
				continue
			}
		} else {
			isSourceMember, _, err := s.chats.IsMember(ctx, orig.ChatID, senderID)
			sourceChatChecked[orig.ChatID] = err == nil && isSourceMember
			if err != nil || !isSourceMember {
				continue
			}
		}
		toForward = append(toForward, model.Message{
			ChatID:        toChatID,
			SenderID:      &senderID,
			Type:          orig.Type,
			Content:       orig.Content,
			Entities:      orig.Entities,
			ForwardedFrom: orig.SenderID,
		})
		origIDs = append(origIDs, orig.ID)
	}

	if len(toForward) == 0 {
		return nil, apperror.BadRequest("No valid messages to forward")
	}

	created, err := s.messages.CreateForwarded(ctx, toForward)
	if err != nil {
		return nil, fmt.Errorf("create forwarded: %w", err)
	}

	// Copy media attachments from originals to forwarded messages
	if len(origIDs) == len(created) {
		origMediaMap, mediaErr := s.messages.GetMediaByMessageIDs(ctx, origIDs)
		if mediaErr != nil {
			slog.Error("failed to get media for forwarded messages", "error", mediaErr)
		} else {
			for i, origID := range origIDs {
				attachments, ok := origMediaMap[origID]
				if !ok || len(attachments) == 0 {
					continue
				}
				mediaIDs := make([]string, len(attachments))
				for j, a := range attachments {
					mediaIDs[j] = a.MediaID
				}
				if copyErr := s.messages.CopyMediaLinks(ctx, created[i].ID, mediaIDs); copyErr != nil {
					slog.Error("failed to copy media for forwarded message", "orig_id", origID, "new_id", created[i].ID, "error", copyErr)
				}
			}
		}
	}

	// Publish events for each forwarded message
	memberIDs, err := s.chats.GetMemberIDs(ctx, toChatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", toChatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.new", toChatID.String())
	for _, m := range created {
		s.nats.Publish(subject, "new_message", m, memberIDs, senderID.String())
	}

	return created, nil
}

func (s *MessageService) PinMessage(ctx context.Context, chatID, msgID, userID uuid.UUID) error {
	member, err := s.chats.GetMember(ctx, chatID, userID)
	if err != nil {
		return fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return apperror.Forbidden("Not a member of this chat")
	}

	chat, err := s.chats.GetByID(ctx, chatID)
	if err != nil || chat == nil {
		return apperror.NotFound("Chat not found")
	}

	if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanPinMessages) {
		return apperror.Forbidden("Not enough permissions to pin")
	}

	if err := s.messages.Pin(ctx, chatID, msgID); err != nil {
		return err
	}

	s.publishPinEvent(ctx, chatID, msgID, true)
	return nil
}

func (s *MessageService) UnpinMessage(ctx context.Context, chatID, msgID, userID uuid.UUID) error {
	isMember, role, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return apperror.Forbidden("Not a member of this chat")
	}

	// In non-DM chats, only owner/admin can unpin (consistent with UnpinAll)
	if role != "owner" && role != "admin" {
		chat, err := s.chats.GetByID(ctx, chatID)
		if err != nil {
			return fmt.Errorf("get chat: %w", err)
		}
		if chat != nil && chat.Type != "direct" {
			return apperror.Forbidden("Only admins can unpin messages")
		}
	}

	if err := s.messages.Unpin(ctx, chatID, msgID); err != nil {
		return err
	}

	s.publishPinEvent(ctx, chatID, msgID, false)
	return nil
}

func (s *MessageService) UnpinAll(ctx context.Context, chatID, userID uuid.UUID) error {
	isMember, role, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return apperror.Forbidden("Not a member of this chat")
	}

	// In DM chats, any member can unpin all. In groups, only owner/admin.
	if role != "owner" && role != "admin" {
		chat, err := s.chats.GetByID(ctx, chatID)
		if err != nil {
			return fmt.Errorf("get chat: %w", err)
		}
		if chat != nil && chat.Type != "direct" {
			return apperror.Forbidden("Only admins can unpin all messages")
		}
	}

	if err := s.messages.UnpinAll(ctx, chatID); err != nil {
		return err
	}

	memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
	if err != nil {
		slog.Error("failed to get member IDs for unpin_all", "error", err, "chat_id", chatID)
	} else {
		s.nats.Publish(
			fmt.Sprintf("orbit.chat.%s.message.updated", chatID),
			"unpin_all",
			map[string]interface{}{
				"chat_id": chatID.String(),
			},
			memberIDs,
			userID.String(),
		)
	}

	return nil
}

func (s *MessageService) publishPinEvent(ctx context.Context, chatID, msgID uuid.UUID, pinned bool) {
	msg, err := s.messages.GetByID(ctx, msgID)
	if err != nil || msg == nil {
		slog.Error("failed to get message for pin event", "msg_id", msgID, "error", err)
		return
	}
	memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
	if err != nil {
		slog.Error("failed to get member IDs for pin event", "chat_id", chatID, "error", err)
		return
	}
	eventType := "message_pinned"
	if !pinned {
		eventType = "message_unpinned"
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.%s", chatID.String(), eventType)
	s.nats.Publish(subject, eventType, map[string]interface{}{
		"id":              msgID.String(),
		"chat_id":         chatID.String(),
		"sequence_number": msg.SequenceNumber,
		"is_pinned":       pinned,
	}, memberIDs)
}

func (s *MessageService) ListPinned(ctx context.Context, chatID, userID uuid.UUID) ([]model.Message, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	return s.messages.ListPinned(ctx, chatID)
}

func (s *MessageService) publishMessageUpdated(ctx context.Context, msg *model.Message) {
	if msg == nil {
		return
	}

	memberIDs, err := s.chats.GetMemberIDs(ctx, msg.ChatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", msg.ChatID, "error", err)
	}

	subject := fmt.Sprintf("orbit.chat.%s.message.updated", msg.ChatID.String())
	s.nats.Publish(subject, "message_updated", msg, memberIDs)
}

// SendMediaMessage creates a message with media attachments.
func (s *MessageService) SendMediaMessage(ctx context.Context, chatID, senderID uuid.UUID,
	content string, entities json.RawMessage, replyToID *uuid.UUID, msgType string,
	mediaIDs []uuid.UUID, isSpoiler bool, groupedID *string) (*model.Message, error) {

	chat, err := s.chats.GetByID(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("get chat: %w", err)
	}
	if chat == nil {
		return nil, apperror.NotFound("Chat not found")
	}

	member, err := s.chats.GetMember(ctx, chatID, senderID)
	if err != nil {
		return nil, fmt.Errorf("get member: %w", err)
	}
	if member == nil {
		return nil, apperror.Forbidden("Not a member of this chat")
	}

	if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanSendMedia) {
		return nil, apperror.Forbidden("You don't have permission to send media")
	}

	// Block check: in direct chats, check if either user has blocked the other
	if chat.Type == "direct" && s.blockedStore != nil {
		members, _, _, err := s.chats.GetMembers(ctx, chatID, "", 2)
		if err != nil {
			return nil, fmt.Errorf("get dm members: %w", err)
		}
		for _, m := range members {
			if m.UserID == senderID {
				continue
			}
			blocked, err := s.blockedStore.IsBlocked(ctx, m.UserID, senderID)
			if err != nil {
				return nil, fmt.Errorf("check blocked: %w", err)
			}
			if blocked {
				return nil, apperror.Forbidden("You cannot send messages to this user")
			}
			blocked, err = s.blockedStore.IsBlocked(ctx, senderID, m.UserID)
			if err != nil {
				return nil, fmt.Errorf("check blocked: %w", err)
			}
			if blocked {
				return nil, apperror.Forbidden("You have blocked this user")
			}
		}
	}

	// Slow mode: atomic check-and-set BEFORE creating the message
	if chat.SlowModeSeconds > 0 && !permissions.IsAdminOrOwner(member.Role) {
		redisKey := fmt.Sprintf("slowmode:%s:%s", chatID, senderID)
		ttl := time.Duration(chat.SlowModeSeconds) * time.Second
		wasSet, err := s.redis.SetNX(ctx, redisKey, "1", ttl).Result()
		if err != nil {
			slog.Error("redis slow mode check failed", "error", err)
			return nil, apperror.Internal("Slow mode check temporarily unavailable")
		}
		if !wasSet {
			remaining, ttlErr := s.redis.TTL(ctx, redisKey).Result()
			waitSec := int(remaining.Seconds())
			if ttlErr != nil || waitSec <= 0 {
				waitSec = chat.SlowModeSeconds
			}
			return nil, apperror.TooManyRequests(fmt.Sprintf("Slow mode: wait %d seconds", waitSec))
		}
	}

	// Validate reply_to_id belongs to the same chat
	if replyToID != nil {
		replyMsg, err := s.messages.GetByID(ctx, *replyToID)
		if err != nil {
			return nil, fmt.Errorf("check reply message: %w", err)
		}
		if replyMsg == nil || replyMsg.IsDeleted {
			return nil, apperror.BadRequest("Reply message not found")
		}
		if replyMsg.ChatID != chatID {
			return nil, apperror.BadRequest("Cannot reply to a message from a different chat")
		}
	}

	// Auto-detect message type from first media if not provided
	if msgType == "" && len(mediaIDs) > 0 {
		msgType = "photo" // will be overridden by frontend typically
	}
	if msgType == "" {
		msgType = "text"
	}

	msg := &model.Message{
		ChatID:    chatID,
		SenderID:  &senderID,
		Type:      msgType,
		Content:   strPtrOrNil(content),
		Entities:  entities,
		ReplyToID: replyToID,
		GroupedID: groupedID,
	}

	if err := s.messages.CreateWithMedia(ctx, msg, mediaIDs, isSpoiler); err != nil {
		if errors.Is(err, model.ErrMediaNotOwned) {
			return nil, apperror.Forbidden("You can only attach media files that you uploaded")
		}
		return nil, fmt.Errorf("create media message: %w", err)
	}

	// Fetch full message with sender info + media
	full, err := s.messages.GetByID(ctx, msg.ID)
	if err != nil {
		return msg, nil
	}

	// Enrich with media attachments
	s.enrichMessageMedia(ctx, full)

	// Publish to NATS
	memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", chatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.message.new", chatID.String())
	s.nats.Publish(subject, "new_message", full, memberIDs, senderID.String())

	return full, nil
}

// enrichMessageMedia loads media attachments for a single message.
func (s *MessageService) enrichMessageMedia(ctx context.Context, msg *model.Message) {
	if msg == nil {
		return
	}
	mediaMap, err := s.messages.GetMediaByMessageIDs(ctx, []uuid.UUID{msg.ID})
	if err != nil {
		slog.Error("failed to load media for message", "msg_id", msg.ID, "error", err)
		return
	}
	if atts, ok := mediaMap[msg.ID]; ok {
		msg.MediaAttachments = atts
	}
}

// EnrichMessagesMedia loads media attachments for a batch of messages. Avoids N+1.
func (s *MessageService) EnrichMessagesMedia(ctx context.Context, msgs []model.Message) {
	if len(msgs) == 0 {
		return
	}

	// Collect IDs of messages that might have media
	var mediaMessageIDs []uuid.UUID
	for i := range msgs {
		switch msgs[i].Type {
		case "photo", "video", "file", "voice", "videonote", "gif", "sticker":
			mediaMessageIDs = append(mediaMessageIDs, msgs[i].ID)
		}
	}
	if len(mediaMessageIDs) == 0 {
		return
	}

	mediaMap, err := s.messages.GetMediaByMessageIDs(ctx, mediaMessageIDs)
	if err != nil {
		slog.Error("failed to batch-load media", "error", err)
		return
	}

	for i := range msgs {
		if atts, ok := mediaMap[msgs[i].ID]; ok {
			msgs[i].MediaAttachments = atts
		}
	}
}

// ListSharedMedia returns media in a chat, optionally filtered by type.
func (s *MessageService) ListSharedMedia(ctx context.Context, chatID, userID uuid.UUID, mediaType string, cursor string, limit int) ([]model.SharedMediaItem, string, bool, error) {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return nil, "", false, fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return nil, "", false, apperror.Forbidden("Not a member of this chat")
	}

	return s.messages.ListSharedMedia(ctx, chatID, mediaType, cursor, limit)
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *MessageService) MarkRead(ctx context.Context, chatID, userID, lastReadMsgID uuid.UUID) error {
	isMember, _, err := s.chats.IsMember(ctx, chatID, userID)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return apperror.Forbidden("Not a member of this chat")
	}

	if err := s.messages.UpdateReadPointer(ctx, chatID, userID, lastReadMsgID); err != nil {
		return fmt.Errorf("update read pointer: %w", err)
	}

	// Publish read event
	memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
	if err != nil {
		slog.Error("failed to get member IDs for NATS publish", "chat_id", chatID, "error", err)
	}
	subject := fmt.Sprintf("orbit.chat.%s.messages.read", chatID.String())
	s.nats.Publish(subject, "messages_read", map[string]interface{}{
		"chat_id":              chatID.String(),
		"user_id":              userID.String(),
		"last_read_message_id": lastReadMsgID.String(),
	}, memberIDs)

	return nil
}
