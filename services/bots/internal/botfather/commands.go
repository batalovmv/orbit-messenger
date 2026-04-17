package botfather

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/crypto"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/service"
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{2,31}$`)

// ── Simple commands ──────────────────────────────────────────────────────

func (bf *BotFather) cmdStart(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, param string) {
	if param == "" {
		bf.reply(ctx, chatID, msgWelcome, nil)
		return
	}
	bf.handleDeepLink(ctx, chatID, senderID, param)
}

func (bf *BotFather) cmdCancel(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID) {
	state, err := bf.state.GetState(ctx, senderID)
	if err != nil || state.Step == StepNone {
		bf.reply(ctx, chatID, msgNothingToCancel, nil)
		return
	}
	bf.state.ClearState(ctx, senderID)
	bf.reply(ctx, chatID, msgCancelled, nil)
}

// ── /newbot ──────────────────────────────────────────────────────────────

func (bf *BotFather) cmdNewBot(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID) {
	// Check bot limit
	bots, err := bf.getUserBots(ctx, senderID)
	if err != nil {
		bf.logger.Error("failed to count user bots", "error", err, "user_id", senderID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}
	if len(bots) >= MaxBotsPerUser {
		bf.reply(ctx, chatID, fmt.Sprintf(msgBotLimitReached, MaxBotsPerUser), nil)
		return
	}

	bf.state.SetState(ctx, senderID, &ConversationState{Step: StepNewBotAskName})
	bf.reply(ctx, chatID, msgNewBotAskName, nil)
}

// ── /mybots ──────────────────────────────────────────────────────────────

func (bf *BotFather) cmdMyBots(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID) {
	bf.sendBotList(ctx, chatID, senderID, "Твои боты:")
}

// ── /setname, /setdescription, /setwebhook ───────────────────────────────

func (bf *BotFather) cmdSetField(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, field string) {
	var selectStep, msg string
	switch field {
	case "setname":
		selectStep = StepSetNameSelectBot
		msg = msgSetNameSelectBot
	case "setdescription":
		selectStep = StepSetDescSelectBot
		msg = msgSetDescSelectBot
	case "setwebhook":
		selectStep = StepSetWebhookSelectBot
		msg = msgSetWebhookSelectBot
	}

	bots, err := bf.getUserBots(ctx, senderID)
	if err != nil {
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}
	if len(bots) == 0 {
		bf.reply(ctx, chatID, msgNoBots, nil)
		return
	}
	if len(bots) == 1 {
		// Skip bot selection, go straight to awaiting input
		bf.startFieldInput(ctx, chatID, senderID, field, bots[0].ID)
		return
	}

	bf.state.SetState(ctx, senderID, &ConversationState{Step: selectStep})
	kb := buildBotListKeyboard(bots, "mybots:select")
	bf.reply(ctx, chatID, msg, kb)
}

func (bf *BotFather) startFieldInput(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, field string, botID uuid.UUID) {
	bot, err := bf.svc.GetBot(ctx, botID)
	if err != nil || bot.OwnerID != senderID {
		bf.reply(ctx, chatID, msgBotNotFound, nil)
		return
	}
	if bot.IsSystem {
		bf.reply(ctx, chatID, msgSystemBotProtected, nil)
		return
	}

	var step, msg string
	switch field {
	case "setname":
		step = StepSetNameAwait
		msg = msgSetNameAwait
	case "setdescription":
		step = StepSetDescAwait
		msg = msgSetDescAwait
	case "setwebhook":
		step = StepSetWebhookAwait
		msg = msgSetWebhookAwait
	}

	bf.state.SetState(ctx, senderID, &ConversationState{Step: step, BotID: botID})
	bf.reply(ctx, chatID, msg, nil)
}

// ── /setcommands ─────────────────────────────────────────────────────────

func (bf *BotFather) cmdSetCommands(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID) {
	bots, err := bf.getUserBots(ctx, senderID)
	if err != nil {
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}
	if len(bots) == 0 {
		bf.reply(ctx, chatID, msgNoBots, nil)
		return
	}
	if len(bots) == 1 {
		bf.state.SetState(ctx, senderID, &ConversationState{Step: StepSetCmdsAwait, BotID: bots[0].ID})
		bf.reply(ctx, chatID, msgSetCmdsAwait, nil)
		return
	}

	bf.state.SetState(ctx, senderID, &ConversationState{Step: StepSetCmdsSelectBot})
	kb := buildBotListKeyboard(bots, "mybots:select")
	bf.reply(ctx, chatID, msgSetCmdsSelectBot, kb)
}

// ── /deletebot ───────────────────────────────────────────────────────────

func (bf *BotFather) cmdDeleteBot(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID) {
	bots, err := bf.getUserBots(ctx, senderID)
	if err != nil {
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}
	if len(bots) == 0 {
		bf.reply(ctx, chatID, msgNoBots, nil)
		return
	}

	bf.state.SetState(ctx, senderID, &ConversationState{Step: StepDeleteSelectBot})
	kb := buildBotListKeyboard(bots, "mybots:select")
	bf.reply(ctx, chatID, msgDeleteSelectBot, kb)
}

// ── /token ───────────────────────────────────────────────────────────────

func (bf *BotFather) cmdToken(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID) {
	bots, err := bf.getUserBots(ctx, senderID)
	if err != nil {
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}
	if len(bots) == 0 {
		bf.reply(ctx, chatID, msgNoBots, nil)
		return
	}
	if len(bots) == 1 {
		bf.showTokenActions(ctx, chatID, senderID, bots[0].ID)
		return
	}

	bf.state.SetState(ctx, senderID, &ConversationState{Step: StepTokenSelectBot})
	kb := buildBotListKeyboard(bots, "mybots:select")
	bf.reply(ctx, chatID, msgTokenSelectBot, kb)
}

func (bf *BotFather) showTokenActions(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, botID uuid.UUID) {
	bot, err := bf.svc.GetBot(ctx, botID)
	if err != nil || bot.OwnerID != senderID {
		bf.reply(ctx, chatID, msgBotNotFound, nil)
		return
	}
	if bot.IsSystem {
		bf.reply(ctx, chatID, msgSystemBotProtected, nil)
		return
	}

	bf.state.SetState(ctx, senderID, &ConversationState{Step: StepTokenActions, BotID: botID})
	kb := buildTokenActionsKeyboard(botID.String())
	bf.reply(ctx, chatID, fmt.Sprintf("Токен бота @%s:", bot.Username), kb)
}

// ── Stateful input handler ───────────────────────────────────────────────

func (bf *BotFather) handleStatefulInput(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, text string, state *ConversationState) {
	switch state.Step {
	// /newbot flow
	case StepNewBotAskName:
		bf.stateNewBotName(ctx, chatID, senderID, text)
	case StepNewBotAskUsername:
		bf.stateNewBotUsername(ctx, chatID, senderID, text, state.Data)

	// /setname flow
	case StepSetNameAwait:
		bf.stateSetName(ctx, chatID, senderID, text, state.BotID)

	// /setdescription flow
	case StepSetDescAwait:
		bf.stateSetDescription(ctx, chatID, senderID, text, state.BotID)

	// /setwebhook flow
	case StepSetWebhookAwait:
		bf.stateSetWebhook(ctx, chatID, senderID, text, state.BotID)
	case StepSetWebhookAwaitSecret:
		bf.stateSetWebhookSecret(ctx, chatID, senderID, text, state.BotID, state.Data)

	// /setcommands flow
	case StepSetCmdsAwait:
		bf.stateSetCommands(ctx, chatID, senderID, text, state.BotID)

	// /setabouttext flow
	case StepSetAboutAwait:
		bf.stateSetAbout(ctx, chatID, senderID, text, state.BotID)

	// /setinline placeholder flow
	case StepSetInlineAwaitPlaceholder:
		bf.stateSetInlinePlaceholder(ctx, chatID, senderID, text, state.BotID)

	// /setmenubutton web_app flow
	case StepSetMenuAwaitText:
		bf.stateMenuButtonText(ctx, chatID, senderID, text, state.BotID)
	case StepSetMenuAwaitURL:
		bf.stateMenuButtonURL(ctx, chatID, senderID, text, state.BotID, state.Data)

	// /setuserpic — expects a photo, not text.
	case StepSetUserpicAwait:
		bf.reply(ctx, chatID, msgSetUserpicNotPhoto, nil)
		return

	// Inline keyboard steps — advise user to tap buttons.
	case StepIntegrationSelectBot, StepIntegrationSelectConnector,
		StepSetPrivacyChoice, StepSetInlineChoice, StepSetJoinGroupsChoice,
		StepSetMenuChoice, StepRevokeConfirm:
		bf.reply(ctx, chatID, "Используй кнопки выше для выбора. Или /cancel для отмены.", nil)
		return

	default:
		// Stale or unknown step — clear and let user start over
		bf.state.ClearState(ctx, senderID)
		bf.reply(ctx, chatID, msgUnknownCommand, nil)
	}
}

// ── State handlers ───────────────────────────────────────────────────────

func (bf *BotFather) stateNewBotName(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, name string) {
	name = strings.TrimSpace(name)
	if len(name) == 0 || len(name) > 64 {
		bf.reply(ctx, chatID, "Имя должно быть от 1 до 64 символов. Попробуй ещё раз.", nil)
		return
	}

	bf.state.SetState(ctx, senderID, &ConversationState{
		Step: StepNewBotAskUsername,
		Data: name,
	})
	bf.reply(ctx, chatID, msgNewBotAskUsername, nil)
}

func (bf *BotFather) stateNewBotUsername(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, username string, displayName string) {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	username = strings.ToLower(username)

	if !usernameRegex.MatchString(username) {
		bf.reply(ctx, chatID, msgNewBotInvalidUsername, nil)
		return
	}

	// Reserved usernames
	if username == BotFatherUsername {
		bf.reply(ctx, chatID, fmt.Sprintf(msgNewBotUsernameTaken, username), nil)
		return
	}

	bot, rawToken, err := bf.svc.CreateBot(ctx, senderID, model.CreateBotRequest{
		Username:    username,
		DisplayName: displayName,
	})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "Conflict") {
			bf.reply(ctx, chatID, fmt.Sprintf(msgNewBotUsernameTaken, username), nil)
			return
		}
		bf.logger.Error("failed to create bot via botfather", "error", err, "user_id", senderID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}

	bf.state.ClearState(ctx, senderID)
	bf.reply(ctx, chatID, msgNewBotCreated(bot.Username, rawToken), nil)
}

func (bf *BotFather) stateSetName(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, name string, botID uuid.UUID) {
	name = strings.TrimSpace(name)
	if len(name) == 0 || len(name) > 64 {
		bf.reply(ctx, chatID, "Имя должно быть от 1 до 64 символов.", nil)
		return
	}

	_, err := bf.svc.UpdateBot(ctx, senderID, "", botID, service.UpdateBotInput{
		DisplayName: &name,
	})
	if err != nil {
		bf.logger.Error("failed to update bot name", "error", err, "bot_id", botID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}

	bot, _ := bf.svc.GetBot(ctx, botID)
	bf.state.ClearState(ctx, senderID)
	if bot != nil {
		bf.reply(ctx, chatID, msgSetNameDone(bot.Username), nil)
	} else {
		bf.reply(ctx, chatID, "Имя обновлено.", nil)
	}
}

func (bf *BotFather) stateSetDescription(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, desc string, botID uuid.UUID) {
	desc = strings.TrimSpace(desc)
	if len(desc) > 512 {
		bf.reply(ctx, chatID, "Описание слишком длинное (максимум 512 символов).", nil)
		return
	}

	_, err := bf.svc.UpdateBot(ctx, senderID, "", botID, service.UpdateBotInput{
		Description: &desc,
	})
	if err != nil {
		bf.logger.Error("failed to update bot description", "error", err, "bot_id", botID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}

	bot, _ := bf.svc.GetBot(ctx, botID)
	bf.state.ClearState(ctx, senderID)
	if bot != nil {
		bf.reply(ctx, chatID, msgSetDescDone(bot.Username), nil)
	} else {
		bf.reply(ctx, chatID, "Описание обновлено.", nil)
	}
}

func (bf *BotFather) stateSetWebhook(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, url string, botID uuid.UUID) {
	url = strings.TrimSpace(url)

	if strings.ToLower(url) == "clear" || url == "" {
		_, err := bf.svc.SetWebhook(ctx, botID, nil, nil)
		if err != nil {
			bf.reply(ctx, chatID, msgInternalError, nil)
			return
		}
		bf.state.ClearState(ctx, senderID)
		bf.reply(ctx, chatID, msgSetWebhookCleared, nil)
		return
	}

	if !strings.HasPrefix(url, "https://") {
		bf.reply(ctx, chatID, msgSetWebhookInvalid, nil)
		return
	}

	// Save URL in state.Data and ask for optional secret
	bf.state.SetState(ctx, senderID, &ConversationState{
		Step:  StepSetWebhookAwaitSecret,
		BotID: botID,
		Data:  url,
	})
	bf.reply(ctx, chatID, msgSetWebhookAskSecret, nil)
}

func (bf *BotFather) stateSetWebhookSecret(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, text string, botID uuid.UUID, webhookURL string) {
	text = strings.TrimSpace(text)

	var secretEnc *string
	if strings.ToLower(text) != "skip" && text != "-" && text != "" {
		encrypted, err := crypto.Encrypt(text, bf.encryptionKey)
		if err != nil {
			bf.logger.Error("failed to encrypt webhook secret", "error", err)
			bf.reply(ctx, chatID, msgInternalError, nil)
			return
		}
		secretEnc = &encrypted
	}

	_, err := bf.svc.SetWebhook(ctx, botID, &webhookURL, secretEnc)
	if err != nil {
		bf.logger.Error("failed to set webhook", "error", err, "bot_id", botID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}

	bot, _ := bf.svc.GetBot(ctx, botID)
	bf.state.ClearState(ctx, senderID)
	username := "bot"
	if bot != nil {
		username = bot.Username
	}
	bf.reply(ctx, chatID, msgSetWebhookDone(username, webhookURL), nil)
}

func (bf *BotFather) stateSetCommands(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, text string, botID uuid.UUID) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	var commands []model.BotCommand

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			// Try with just " - " or "-"
			parts = strings.SplitN(line, "-", 2)
			if len(parts) != 2 {
				bf.reply(ctx, chatID, msgSetCmdsInvalid, nil)
				return
			}
		}

		cmd := strings.TrimSpace(strings.TrimPrefix(parts[0], "/"))
		desc := strings.TrimSpace(parts[1])

		if cmd == "" || desc == "" {
			bf.reply(ctx, chatID, msgSetCmdsInvalid, nil)
			return
		}

		commands = append(commands, model.BotCommand{
			BotID:       botID,
			Command:     cmd,
			Description: desc,
		})
	}

	if len(commands) == 0 {
		bf.reply(ctx, chatID, msgSetCmdsInvalid, nil)
		return
	}

	if err := bf.svc.SetCommands(ctx, botID, commands); err != nil {
		bf.logger.Error("failed to set commands", "error", err, "bot_id", botID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}

	bot, _ := bf.svc.GetBot(ctx, botID)
	bf.state.ClearState(ctx, senderID)
	username := ""
	if bot != nil {
		username = bot.Username
	}
	bf.reply(ctx, chatID, msgSetCmdsDone(username, len(commands)), nil)
}

// ── Callback handlers ────────────────────────────────────────────────────

func (bf *BotFather) callbackBotSelected(ctx context.Context, chatID uuid.UUID, callerID uuid.UUID, botID uuid.UUID) {
	bot, err := bf.svc.GetBot(ctx, botID)
	if err != nil || bot.OwnerID != callerID {
		bf.reply(ctx, chatID, msgBotNotFound, nil)
		return
	}
	if bot.IsSystem {
		bf.reply(ctx, chatID, msgSystemBotProtected, nil)
		return
	}

	// Check if there's an active state that needs bot selection
	state, _ := bf.state.GetState(ctx, callerID)
	if state != nil && state.Step != StepNone {
		bf.handleBotSelection(ctx, chatID, callerID, botID, state)
		return
	}

	// Default: show management menu
	kb := buildManagementMenu(botID.String())
	bf.reply(ctx, chatID, msgBotSelected(bot.Username), kb)
}

func (bf *BotFather) handleBotSelection(ctx context.Context, chatID uuid.UUID, callerID uuid.UUID, botID uuid.UUID, state *ConversationState) {
	switch state.Step {
	case StepSetNameSelectBot:
		bf.startFieldInput(ctx, chatID, callerID, "setname", botID)
	case StepSetDescSelectBot:
		bf.startFieldInput(ctx, chatID, callerID, "setdescription", botID)
	case StepSetWebhookSelectBot:
		bf.startFieldInput(ctx, chatID, callerID, "setwebhook", botID)
	case StepSetCmdsSelectBot:
		bf.state.SetState(ctx, callerID, &ConversationState{Step: StepSetCmdsAwait, BotID: botID})
		bf.reply(ctx, chatID, msgSetCmdsAwait, nil)
	case StepDeleteSelectBot:
		bot, _ := bf.svc.GetBot(ctx, botID)
		if bot == nil {
			bf.reply(ctx, chatID, msgBotNotFound, nil)
			return
		}
		bf.state.SetState(ctx, callerID, &ConversationState{Step: StepDeleteConfirm, BotID: botID})
		kb := buildConfirmKeyboard(
			fmt.Sprintf("delete:yes:%s", botID),
			"delete:cancel",
		)
		bf.reply(ctx, chatID, fmt.Sprintf(msgDeleteConfirm, bot.Username), kb)
	case StepTokenSelectBot:
		bf.showTokenActions(ctx, chatID, callerID, botID)
	case StepIntegrationSelectBot:
		bf.showConnectorList(ctx, chatID, callerID, botID)

	case StepSetAboutSelectBot:
		bf.state.SetState(ctx, callerID, &ConversationState{Step: StepSetAboutAwait, BotID: botID})
		bf.reply(ctx, chatID, msgSetAboutAwait, nil)
	case StepSetPrivacySelectBot:
		bot, _ := bf.svc.GetBot(ctx, botID)
		if bot != nil {
			bf.showPrivacyToggle(ctx, chatID, callerID, bot)
		}
	case StepSetInlineSelectBot:
		bot, _ := bf.svc.GetBot(ctx, botID)
		if bot != nil {
			bf.showInlineToggle(ctx, chatID, callerID, bot)
		}
	case StepSetJoinGroupsSelectBot:
		bot, _ := bf.svc.GetBot(ctx, botID)
		if bot != nil {
			bf.showJoinGroupsToggle(ctx, chatID, callerID, bot)
		}
	case StepSetMenuSelectBot:
		bot, _ := bf.svc.GetBot(ctx, botID)
		if bot != nil {
			bf.showMenuTypeSelector(ctx, chatID, callerID, bot)
		}
	case StepRevokeSelectBot:
		bot, _ := bf.svc.GetBot(ctx, botID)
		if bot == nil {
			bf.reply(ctx, chatID, msgBotNotFound, nil)
			return
		}
		bf.state.SetState(ctx, callerID, &ConversationState{Step: StepRevokeConfirm, BotID: botID})
		kb := buildConfirmKeyboard(
			fmt.Sprintf("revoke:yes:%s", botID),
			"revoke:cancel",
		)
		bf.reply(ctx, chatID, fmt.Sprintf(msgRevokeConfirm, bot.Username), kb)

	case StepSetUserpicSelectBot:
		bf.state.SetState(ctx, callerID, &ConversationState{Step: StepSetUserpicAwait, BotID: botID})
		bf.reply(ctx, chatID, msgSetUserpicAwait, nil)

	default:
		// Show management menu
		bot, _ := bf.svc.GetBot(ctx, botID)
		if bot != nil {
			kb := buildManagementMenu(botID.String())
			bf.reply(ctx, chatID, msgBotSelected(bot.Username), kb)
		}
	}
}

func (bf *BotFather) callbackManage(ctx context.Context, chatID uuid.UUID, callerID uuid.UUID, action string, parts []string) {
	if action == "back" {
		bf.cmdMyBots(ctx, chatID, callerID)
		return
	}

	if len(parts) < 3 {
		return
	}
	botID, err := uuid.Parse(parts[2])
	if err != nil {
		return
	}

	bot, err := bf.svc.GetBot(ctx, botID)
	if err != nil || bot.OwnerID != callerID {
		bf.reply(ctx, chatID, msgBotNotFound, nil)
		return
	}
	if bot.IsSystem {
		bf.reply(ctx, chatID, msgSystemBotProtected, nil)
		return
	}

	switch action {
	case "setname":
		bf.startFieldInput(ctx, chatID, callerID, "setname", botID)
	case "setdesc":
		bf.startFieldInput(ctx, chatID, callerID, "setdescription", botID)
	case "setcmds":
		bf.state.SetState(ctx, callerID, &ConversationState{Step: StepSetCmdsAwait, BotID: botID})
		bf.reply(ctx, chatID, msgSetCmdsAwait, nil)
	case "setwebhook":
		bf.startFieldInput(ctx, chatID, callerID, "setwebhook", botID)
	case "token":
		bf.showTokenActions(ctx, chatID, callerID, botID)
	case "delete":
		bf.state.SetState(ctx, callerID, &ConversationState{Step: StepDeleteConfirm, BotID: botID})
		kb := buildConfirmKeyboard(
			fmt.Sprintf("delete:yes:%s", botID),
			"delete:cancel",
		)
		bf.reply(ctx, chatID, fmt.Sprintf(msgDeleteConfirm, bot.Username), kb)
	case "integration":
		bf.showConnectorList(ctx, chatID, callerID, botID)
	}
}

func (bf *BotFather) callbackToken(ctx context.Context, chatID uuid.UUID, callerID uuid.UUID, action string, parts []string) {
	if len(parts) < 3 {
		return
	}
	botID, err := uuid.Parse(parts[2])
	if err != nil {
		return
	}

	bot, err := bf.svc.GetBot(ctx, botID)
	if err != nil || bot.OwnerID != callerID {
		bf.reply(ctx, chatID, msgBotNotFound, nil)
		return
	}

	switch action {
	case "show":
		// Show token prefix
		tokens, err := bf.tokenStore.ListByBot(ctx, botID)
		if err != nil || len(tokens) == 0 {
			bf.reply(ctx, chatID, msgBotNotFound, nil)
			return
		}
		bf.state.ClearState(ctx, callerID)
		bf.reply(ctx, chatID, msgTokenPrefix(bot.Username, tokens[0].TokenPrefix), nil)

	case "rotate":
		bf.state.SetState(ctx, callerID, &ConversationState{Step: StepTokenConfirm, BotID: botID})
		kb := buildConfirmKeyboard(
			fmt.Sprintf("token:confirmrotate:%s", botID),
			"token:cancelrotate",
		)
		bf.reply(ctx, chatID, msgTokenConfirmRotate, kb)

	case "confirmrotate":
		rawToken, err := bf.svc.RotateToken(ctx, callerID, "", botID)
		if err != nil {
			bf.logger.Error("failed to rotate token", "error", err, "bot_id", botID)
			bf.reply(ctx, chatID, msgInternalError, nil)
			return
		}
		bf.state.ClearState(ctx, callerID)
		bf.reply(ctx, chatID, msgTokenRotated(bot.Username, rawToken), nil)

	case "cancelrotate":
		bf.state.ClearState(ctx, callerID)
		bf.reply(ctx, chatID, msgDeleteCancelled, nil)
	}
}

func (bf *BotFather) callbackDelete(ctx context.Context, chatID uuid.UUID, callerID uuid.UUID, action string, parts []string) {
	switch action {
	case "yes":
		if len(parts) < 3 {
			return
		}
		botID, err := uuid.Parse(parts[2])
		if err != nil {
			return
		}

		bot, err := bf.svc.GetBot(ctx, botID)
		if err != nil {
			bf.reply(ctx, chatID, msgBotNotFound, nil)
			return
		}

		if err := bf.svc.DeleteBot(ctx, callerID, "", botID); err != nil {
			bf.logger.Error("failed to delete bot", "error", err, "bot_id", botID)
			bf.reply(ctx, chatID, msgInternalError, nil)
			return
		}

		bf.state.ClearState(ctx, callerID)
		bf.reply(ctx, chatID, msgDeleteDone(bot.Username), nil)

	case "cancel":
		bf.state.ClearState(ctx, callerID)
		bf.reply(ctx, chatID, msgDeleteCancelled, nil)
	}
}

// ── Deep links ───────────────────────────────────────────────────────────

func (bf *BotFather) handleDeepLink(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, param string) {
	// Parse param: "{username}", "{username}-intro", "{username}-commands"
	var username, action string

	if idx := strings.LastIndex(param, "-"); idx > 0 {
		suffix := param[idx+1:]
		if suffix == "intro" || suffix == "commands" {
			username = param[:idx]
			action = suffix
		} else {
			username = param
		}
	} else {
		username = param
	}

	bot, err := bf.botStore.GetByUsername(ctx, username)
	if err != nil || bot == nil || bot.OwnerID != senderID {
		bf.reply(ctx, chatID, msgBotNotFound, nil)
		return
	}
	if bot.IsSystem {
		bf.reply(ctx, chatID, msgSystemBotProtected, nil)
		return
	}

	switch action {
	case "intro":
		bf.startFieldInput(ctx, chatID, senderID, "setdescription", bot.ID)
	case "commands":
		bf.state.SetState(ctx, senderID, &ConversationState{Step: StepSetCmdsAwait, BotID: bot.ID})
		bf.reply(ctx, chatID, msgSetCmdsAwait, nil)
	default:
		// Show management menu
		kb := buildManagementMenu(bot.ID.String())
		bf.reply(ctx, chatID, msgBotSelected(bot.Username), kb)
	}
}
