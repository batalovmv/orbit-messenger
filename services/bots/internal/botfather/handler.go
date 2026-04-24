// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botfather

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/service"
)

// HandleIfBotFatherDM handles a message in a chat where BotFather is a member.
// The caller (NATS subscriber) has already verified that BotFather's userID is
// in the event's member_ids. Returns true if the message was handled.
func (bf *BotFather) HandleIfBotFatherDM(ctx context.Context, chatID uuid.UUID, senderID string, content string, messageType string, attachments []service.IncomingAttachment) bool {
	// Self-loop prevention: don't handle our own messages
	if senderID == bf.userID.String() {
		return true // consumed, but no action
	}

	senderUUID, err := uuid.Parse(senderID)
	if err != nil {
		return false
	}

	// Photo upload only makes sense inside /setuserpic; otherwise it's rejected.
	if messageType == "photo" || messageType == "image" {
		bf.handlePhoto(ctx, chatID, senderUUID, attachments)
		return true
	}

	// Other non-text messages are rejected with a hint.
	if messageType != "" && messageType != "text" {
		bf.reply(ctx, chatID, msgTextOnly, nil)
		return true
	}

	bf.handleMessage(ctx, chatID, senderUUID, strings.TrimSpace(content))
	return true
}

// handleMessage routes incoming text to the appropriate command or state handler.
func (bf *BotFather) handleMessage(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, text string) {
	// Commands always take priority
	if strings.HasPrefix(text, "/") {
		bf.handleCommand(ctx, chatID, senderID, text)
		return
	}

	// Check if there's an active conversation state
	state, err := bf.state.GetState(ctx, senderID)
	if err != nil {
		bf.logger.Error("failed to get conversation state", "error", err, "user_id", senderID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}

	if state.Step == StepNone {
		bf.reply(ctx, chatID, msgUnknownCommand, nil)
		return
	}

	bf.handleStatefulInput(ctx, chatID, senderID, text, state)
}

// handleCommand parses and dispatches slash commands.
func (bf *BotFather) handleCommand(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, text string) {
	parts := strings.Fields(text)
	cmd := strings.ToLower(parts[0])
	param := ""
	if len(parts) > 1 {
		param = strings.Join(parts[1:], " ")
	}

	switch cmd {
	case "/start":
		bf.cmdStart(ctx, chatID, senderID, param)
	case "/help":
		bf.reply(ctx, chatID, msgHelp, nil)
	case "/cancel":
		bf.cmdCancel(ctx, chatID, senderID)
	case "/newbot":
		bf.cmdNewBot(ctx, chatID, senderID)
	case "/mybots":
		bf.cmdMyBots(ctx, chatID, senderID)
	case "/setname":
		bf.cmdSetField(ctx, chatID, senderID, "setname")
	case "/setdescription":
		bf.cmdSetField(ctx, chatID, senderID, "setdescription")
	case "/setwebhook":
		bf.cmdSetField(ctx, chatID, senderID, "setwebhook")
	case "/setcommands":
		bf.cmdSetCommands(ctx, chatID, senderID)
	case "/deletebot":
		bf.cmdDeleteBot(ctx, chatID, senderID)
	case "/token":
		bf.cmdToken(ctx, chatID, senderID)
	case "/setintegration":
		bf.cmdSetIntegration(ctx, chatID, senderID)
	case "/setabouttext":
		bf.cmdSetAbout(ctx, chatID, senderID)
	case "/setprivacy":
		bf.cmdSetPrivacy(ctx, chatID, senderID)
	case "/setinline":
		bf.cmdSetInline(ctx, chatID, senderID)
	case "/setjoingroups":
		bf.cmdSetJoinGroups(ctx, chatID, senderID)
	case "/setmenubutton":
		bf.cmdSetMenuButton(ctx, chatID, senderID)
	case "/revoke":
		bf.cmdRevoke(ctx, chatID, senderID)
	case "/setuserpic":
		bf.cmdSetUserpic(ctx, chatID, senderID)
	default:
		bf.reply(ctx, chatID, msgUnknownCommand, nil)
	}
}

// HandleCallback processes an inline keyboard callback from the BotFather's buttons.
// Returns the response map to send back to the caller (answerCallbackQuery format).
func (bf *BotFather) HandleCallback(ctx context.Context, callerID uuid.UUID, chatID uuid.UUID, queryID string, data string) map[string]any {
	parts := strings.Split(data, ":")
	if len(parts) < 2 {
		return map[string]any{}
	}

	prefix := parts[0]
	action := parts[1]

	switch prefix {
	case "mybots":
		if action == "select" && len(parts) >= 3 {
			botID, err := uuid.Parse(parts[2])
			if err != nil {
				return map[string]any{}
			}
			bf.callbackBotSelected(ctx, chatID, callerID, botID)
		}

	case "manage":
		bf.callbackManage(ctx, chatID, callerID, action, parts)

	case "token":
		bf.callbackToken(ctx, chatID, callerID, action, parts)

	case "delete":
		bf.callbackDelete(ctx, chatID, callerID, action, parts)

	case "integration":
		bf.callbackIntegration(ctx, chatID, callerID, action, parts)

	case "setprivacy":
		bf.callbackPrivacy(ctx, chatID, callerID, action, parts)
	case "setinline":
		bf.callbackInline(ctx, chatID, callerID, action, parts)
	case "setjoin":
		bf.callbackJoinGroups(ctx, chatID, callerID, action, parts)
	case "setmenu":
		bf.callbackSetMenu(ctx, chatID, callerID, action, parts)
	case "revoke":
		bf.callbackRevoke(ctx, chatID, callerID, action, parts)
	}

	return map[string]any{}
}

// reply sends a text message (with optional inline keyboard) as BotFather.
func (bf *BotFather) reply(ctx context.Context, chatID uuid.UUID, text string, keyboard *InlineKeyboardMarkup) {
	var replyMarkup json.RawMessage
	if keyboard != nil {
		replyMarkup = marshalKeyboard(keyboard)
	}

	if _, err := bf.msgClient.SendMessage(ctx, bf.userID, chatID, text, "text", replyMarkup, nil); err != nil {
		bf.logger.Error("botfather failed to send message",
			"error", err,
			"chat_id", chatID,
		)
	}
}

// editMessage edits an existing message (for updating inline keyboards after callback).
func (bf *BotFather) editMessage(ctx context.Context, messageID uuid.UUID, text string, keyboard *InlineKeyboardMarkup) {
	var replyMarkup json.RawMessage
	if keyboard != nil {
		replyMarkup = marshalKeyboard(keyboard)
	}

	if _, err := bf.msgClient.EditMessage(ctx, bf.userID, messageID, text, replyMarkup); err != nil {
		bf.logger.Error("botfather failed to edit message",
			"error", err,
			"message_id", messageID,
		)
	}
}

// getUserBots returns all non-system bots owned by a user.
func (bf *BotFather) getUserBots(ctx context.Context, ownerID uuid.UUID) ([]model.Bot, error) {
	bots, _, err := bf.svc.ListBots(ctx, &ownerID, 100, 0)
	if err != nil {
		return nil, err
	}
	// Filter out system bots
	filtered := make([]model.Bot, 0, len(bots))
	for _, b := range bots {
		if !b.IsSystem {
			filtered = append(filtered, b)
		}
	}
	return filtered, nil
}

// sendBotList sends the user's bot list as an inline keyboard.
func (bf *BotFather) sendBotList(ctx context.Context, chatID uuid.UUID, ownerID uuid.UUID, headerMsg string) {
	bots, err := bf.getUserBots(ctx, ownerID)
	if err != nil {
		bf.logger.Error("failed to list user bots", "error", err, "user_id", ownerID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}

	if len(bots) == 0 {
		bf.reply(ctx, chatID, msgNoBots, nil)
		return
	}

	kb := buildBotListKeyboard(bots, "mybots:select")
	bf.reply(ctx, chatID, headerMsg, kb)
}
