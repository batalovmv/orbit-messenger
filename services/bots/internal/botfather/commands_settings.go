package botfather

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/service"
)

// ── /setabouttext ────────────────────────────────────────────────────────

func (bf *BotFather) cmdSetAbout(ctx context.Context, chatID, senderID uuid.UUID) {
	bf.selectBotForField(ctx, chatID, senderID, StepSetAboutSelectBot, StepSetAboutAwait, msgSetAboutSelectBot, msgSetAboutAwait)
}

func (bf *BotFather) stateSetAbout(ctx context.Context, chatID, senderID uuid.UUID, text string, botID uuid.UUID) {
	text = strings.TrimSpace(text)
	if strings.EqualFold(text, "clear") {
		empty := ""
		if _, err := bf.svc.UpdateBot(ctx, senderID, "", botID, service.UpdateBotInput{AboutText: &empty}); err != nil {
			bf.logger.Error("failed to clear about_text", "error", err, "bot_id", botID)
			bf.reply(ctx, chatID, msgInternalError, nil)
			return
		}
		bot, _ := bf.svc.GetBot(ctx, botID)
		bf.state.ClearState(ctx, senderID)
		bf.reply(ctx, chatID, msgSetAboutDone(safeUsername(bot)), nil)
		return
	}
	if len(text) > 120 {
		bf.reply(ctx, chatID, msgSetAboutTooLong, nil)
		return
	}
	if _, err := bf.svc.UpdateBot(ctx, senderID, "", botID, service.UpdateBotInput{AboutText: &text}); err != nil {
		bf.logger.Error("failed to update about_text", "error", err, "bot_id", botID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}
	bot, _ := bf.svc.GetBot(ctx, botID)
	bf.state.ClearState(ctx, senderID)
	bf.reply(ctx, chatID, msgSetAboutDone(safeUsername(bot)), nil)
}

// ── /setprivacy ──────────────────────────────────────────────────────────

func (bf *BotFather) cmdSetPrivacy(ctx context.Context, chatID, senderID uuid.UUID) {
	bf.selectBotForToggle(ctx, chatID, senderID, StepSetPrivacySelectBot, func(bot *model.Bot) {
		bf.showPrivacyToggle(ctx, chatID, senderID, bot)
	})
}

func (bf *BotFather) showPrivacyToggle(ctx context.Context, chatID, senderID uuid.UUID, bot *model.Bot) {
	current := lblDisabled
	if bot.IsPrivacyEnabled {
		current = lblEnabled
	}
	bf.state.SetState(ctx, senderID, &ConversationState{Step: StepSetPrivacyChoice, BotID: bot.ID})
	kb := buildToggleKeyboard(lblEnabled, lblDisabled,
		fmt.Sprintf("setprivacy:on:%s", bot.ID),
		fmt.Sprintf("setprivacy:off:%s", bot.ID))
	bf.reply(ctx, chatID, fmt.Sprintf(msgSetPrivacyPrompt, current), kb)
}

func (bf *BotFather) applyPrivacyChoice(ctx context.Context, chatID, senderID, botID uuid.UUID, enabled bool) {
	if _, err := bf.svc.UpdateBot(ctx, senderID, "", botID, service.UpdateBotInput{IsPrivacyEnabled: &enabled}); err != nil {
		bf.logger.Error("failed to update privacy", "error", err, "bot_id", botID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}
	bot, _ := bf.svc.GetBot(ctx, botID)
	bf.state.ClearState(ctx, senderID)
	bf.reply(ctx, chatID, msgSetPrivacyDone(safeUsername(bot), enabled), nil)
}

// ── /setinline ───────────────────────────────────────────────────────────

func (bf *BotFather) cmdSetInline(ctx context.Context, chatID, senderID uuid.UUID) {
	bf.selectBotForToggle(ctx, chatID, senderID, StepSetInlineSelectBot, func(bot *model.Bot) {
		bf.showInlineToggle(ctx, chatID, senderID, bot)
	})
}

func (bf *BotFather) showInlineToggle(ctx context.Context, chatID, senderID uuid.UUID, bot *model.Bot) {
	current := lblDisabled
	if bot.IsInline {
		current = lblEnabled
	}
	bf.state.SetState(ctx, senderID, &ConversationState{Step: StepSetInlineChoice, BotID: bot.ID})
	kb := buildToggleKeyboard(lblEnabled, lblDisabled,
		fmt.Sprintf("setinline:on:%s", bot.ID),
		fmt.Sprintf("setinline:off:%s", bot.ID))
	bf.reply(ctx, chatID, fmt.Sprintf(msgSetInlinePrompt, current), kb)
}

func (bf *BotFather) applyInlineOff(ctx context.Context, chatID, senderID, botID uuid.UUID) {
	enabled := false
	empty := ""
	if _, err := bf.svc.UpdateBot(ctx, senderID, "", botID, service.UpdateBotInput{
		IsInline:          &enabled,
		InlinePlaceholder: &empty,
	}); err != nil {
		bf.logger.Error("failed to disable inline", "error", err, "bot_id", botID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}
	bot, _ := bf.svc.GetBot(ctx, botID)
	bf.state.ClearState(ctx, senderID)
	bf.reply(ctx, chatID, msgSetInlineOff(safeUsername(bot)), nil)
}

func (bf *BotFather) applyInlineOn(ctx context.Context, chatID, senderID, botID uuid.UUID) {
	bf.state.SetState(ctx, senderID, &ConversationState{Step: StepSetInlineAwaitPlaceholder, BotID: botID})
	bf.reply(ctx, chatID, msgSetInlineAskPlaceholder, nil)
}

func (bf *BotFather) stateSetInlinePlaceholder(ctx context.Context, chatID, senderID uuid.UUID, text string, botID uuid.UUID) {
	text = strings.TrimSpace(text)
	placeholder := text
	if strings.EqualFold(text, "skip") || text == "-" || text == "" {
		placeholder = ""
	}
	if len(placeholder) > 64 {
		bf.reply(ctx, chatID, msgSetInlinePlaceholderTooLong, nil)
		return
	}
	enabled := true
	if _, err := bf.svc.UpdateBot(ctx, senderID, "", botID, service.UpdateBotInput{
		IsInline:          &enabled,
		InlinePlaceholder: &placeholder,
	}); err != nil {
		bf.logger.Error("failed to enable inline", "error", err, "bot_id", botID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}
	bot, _ := bf.svc.GetBot(ctx, botID)
	bf.state.ClearState(ctx, senderID)
	bf.reply(ctx, chatID, msgSetInlineOn(safeUsername(bot)), nil)
}

// ── /setjoingroups ───────────────────────────────────────────────────────

func (bf *BotFather) cmdSetJoinGroups(ctx context.Context, chatID, senderID uuid.UUID) {
	bf.selectBotForToggle(ctx, chatID, senderID, StepSetJoinGroupsSelectBot, func(bot *model.Bot) {
		bf.showJoinGroupsToggle(ctx, chatID, senderID, bot)
	})
}

func (bf *BotFather) showJoinGroupsToggle(ctx context.Context, chatID, senderID uuid.UUID, bot *model.Bot) {
	current := lblDisallowed
	if bot.CanJoinGroups {
		current = lblAllowed
	}
	bf.state.SetState(ctx, senderID, &ConversationState{Step: StepSetJoinGroupsChoice, BotID: bot.ID})
	kb := buildToggleKeyboard(lblAllowed, lblDisallowed,
		fmt.Sprintf("setjoin:on:%s", bot.ID),
		fmt.Sprintf("setjoin:off:%s", bot.ID))
	bf.reply(ctx, chatID, fmt.Sprintf(msgSetJoinGroupsPrompt, current), kb)
}

func (bf *BotFather) applyJoinGroupsChoice(ctx context.Context, chatID, senderID, botID uuid.UUID, allowed bool) {
	if _, err := bf.svc.UpdateBot(ctx, senderID, "", botID, service.UpdateBotInput{CanJoinGroups: &allowed}); err != nil {
		bf.logger.Error("failed to update can_join_groups", "error", err, "bot_id", botID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}
	bot, _ := bf.svc.GetBot(ctx, botID)
	bf.state.ClearState(ctx, senderID)
	bf.reply(ctx, chatID, msgSetJoinGroupsDone(safeUsername(bot), allowed), nil)
}

// ── /setmenubutton ───────────────────────────────────────────────────────

func (bf *BotFather) cmdSetMenuButton(ctx context.Context, chatID, senderID uuid.UUID) {
	bf.selectBotForToggle(ctx, chatID, senderID, StepSetMenuSelectBot, func(bot *model.Bot) {
		bf.showMenuTypeSelector(ctx, chatID, senderID, bot)
	})
}

func (bf *BotFather) showMenuTypeSelector(ctx context.Context, chatID, senderID uuid.UUID, bot *model.Bot) {
	bf.state.SetState(ctx, senderID, &ConversationState{Step: StepSetMenuChoice, BotID: bot.ID})
	kb := buildMenuButtonTypeKeyboard(bot.ID.String())
	bf.reply(ctx, chatID, msgSetMenuPrompt, kb)
}

func (bf *BotFather) applyMenuDefault(ctx context.Context, chatID, senderID, botID uuid.UUID, kind string) {
	input := service.UpdateBotInput{MenuButton: &model.MenuButton{Type: kind}}
	if _, err := bf.svc.UpdateBot(ctx, senderID, "", botID, input); err != nil {
		bf.logger.Error("failed to set menu_button", "error", err, "bot_id", botID, "type", kind)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}
	bot, _ := bf.svc.GetBot(ctx, botID)
	bf.state.ClearState(ctx, senderID)
	bf.reply(ctx, chatID, msgSetMenuDone(safeUsername(bot), kind), nil)
}

func (bf *BotFather) applyMenuClear(ctx context.Context, chatID, senderID, botID uuid.UUID) {
	if _, err := bf.svc.UpdateBot(ctx, senderID, "", botID, service.UpdateBotInput{ClearMenuButton: true}); err != nil {
		bf.logger.Error("failed to clear menu_button", "error", err, "bot_id", botID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}
	bot, _ := bf.svc.GetBot(ctx, botID)
	bf.state.ClearState(ctx, senderID)
	bf.reply(ctx, chatID, msgSetMenuCleared(safeUsername(bot)), nil)
}

func (bf *BotFather) startMenuWebApp(ctx context.Context, chatID, senderID, botID uuid.UUID) {
	bf.state.SetState(ctx, senderID, &ConversationState{Step: StepSetMenuAwaitText, BotID: botID})
	bf.reply(ctx, chatID, msgSetMenuAskText, nil)
}

func (bf *BotFather) stateMenuButtonText(ctx context.Context, chatID, senderID uuid.UUID, text string, botID uuid.UUID) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 32 {
		bf.reply(ctx, chatID, msgSetMenuTextTooLong, nil)
		return
	}
	bf.state.SetState(ctx, senderID, &ConversationState{Step: StepSetMenuAwaitURL, BotID: botID, Data: text})
	bf.reply(ctx, chatID, msgSetMenuAskURL, nil)
}

func (bf *BotFather) stateMenuButtonURL(ctx context.Context, chatID, senderID uuid.UUID, url string, botID uuid.UUID, text string) {
	url = strings.TrimSpace(url)
	if !strings.HasPrefix(url, "https://") {
		bf.reply(ctx, chatID, msgSetMenuInvalidURL, nil)
		return
	}
	mb := &model.MenuButton{Type: "web_app", Text: text, WebAppURL: url}
	if _, err := bf.svc.UpdateBot(ctx, senderID, "", botID, service.UpdateBotInput{MenuButton: mb}); err != nil {
		bf.logger.Error("failed to save web_app menu_button", "error", err, "bot_id", botID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}
	bot, _ := bf.svc.GetBot(ctx, botID)
	bf.state.ClearState(ctx, senderID)
	bf.reply(ctx, chatID, msgSetMenuDone(safeUsername(bot), "web_app"), nil)
}

// ── /revoke ──────────────────────────────────────────────────────────────

func (bf *BotFather) cmdRevoke(ctx context.Context, chatID, senderID uuid.UUID) {
	bf.selectBotForToggle(ctx, chatID, senderID, StepRevokeSelectBot, func(bot *model.Bot) {
		bf.state.SetState(ctx, senderID, &ConversationState{Step: StepRevokeConfirm, BotID: bot.ID})
		kb := buildConfirmKeyboard(
			fmt.Sprintf("revoke:yes:%s", bot.ID),
			"revoke:cancel",
		)
		bf.reply(ctx, chatID, fmt.Sprintf(msgRevokeConfirm, bot.Username), kb)
	})
}

func (bf *BotFather) applyRevoke(ctx context.Context, chatID, senderID, botID uuid.UUID) {
	bot, err := bf.svc.GetBot(ctx, botID)
	if err != nil || bot == nil || bot.OwnerID != senderID {
		bf.reply(ctx, chatID, msgBotNotFound, nil)
		return
	}
	rawToken, err := bf.svc.RotateToken(ctx, senderID, "", botID)
	if err != nil {
		bf.logger.Error("failed to rotate during revoke", "error", err, "bot_id", botID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}
	// Clear webhook secret so old signatures stop validating.
	empty := ""
	if _, err := bf.svc.SetWebhook(ctx, botID, bot.WebhookURL, &empty); err != nil {
		bf.logger.Error("failed to clear webhook secret on revoke", "error", err, "bot_id", botID)
	}
	bf.state.ClearState(ctx, senderID)
	bf.reply(ctx, chatID, msgRevokeDone(bot.Username, rawToken), nil)
}

// ── Shared helpers ───────────────────────────────────────────────────────

// selectBotForField is used for free-text input settings like /setabouttext.
// When user has one bot it skips the picker; otherwise shows the inline list.
func (bf *BotFather) selectBotForField(ctx context.Context, chatID, senderID uuid.UUID,
	selectStep, awaitStep, msgSelect, msgAwait string,
) {
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
		bf.state.SetState(ctx, senderID, &ConversationState{Step: awaitStep, BotID: bots[0].ID})
		bf.reply(ctx, chatID, msgAwait, nil)
		return
	}
	bf.state.SetState(ctx, senderID, &ConversationState{Step: selectStep})
	bf.reply(ctx, chatID, msgSelect, buildBotListKeyboard(bots, "mybots:select"))
}

// selectBotForToggle is used for toggle settings (privacy/inline/joingroups/menu/revoke).
// After the bot is picked, `onPick` is invoked with the resolved bot.
func (bf *BotFather) selectBotForToggle(ctx context.Context, chatID, senderID uuid.UUID,
	selectStep string, onPick func(bot *model.Bot),
) {
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
		bot, _ := bf.svc.GetBot(ctx, bots[0].ID)
		if bot == nil {
			bf.reply(ctx, chatID, msgBotNotFound, nil)
			return
		}
		onPick(bot)
		return
	}
	bf.state.SetState(ctx, senderID, &ConversationState{Step: selectStep})
	bf.reply(ctx, chatID, msgSetPrivacySelectBot, buildBotListKeyboard(bots, "mybots:select"))
}

func safeUsername(bot *model.Bot) string {
	if bot == nil {
		return ""
	}
	return bot.Username
}

// ── Callback handlers ────────────────────────────────────────────────────

func (bf *BotFather) callbackPrivacy(ctx context.Context, chatID, callerID uuid.UUID, action string, parts []string) {
	if len(parts) < 3 {
		return
	}
	botID, err := uuid.Parse(parts[2])
	if err != nil {
		return
	}
	bf.applyPrivacyChoice(ctx, chatID, callerID, botID, action == "on")
}

func (bf *BotFather) callbackInline(ctx context.Context, chatID, callerID uuid.UUID, action string, parts []string) {
	if len(parts) < 3 {
		return
	}
	botID, err := uuid.Parse(parts[2])
	if err != nil {
		return
	}
	if action == "on" {
		bf.applyInlineOn(ctx, chatID, callerID, botID)
	} else {
		bf.applyInlineOff(ctx, chatID, callerID, botID)
	}
}

func (bf *BotFather) callbackJoinGroups(ctx context.Context, chatID, callerID uuid.UUID, action string, parts []string) {
	if len(parts) < 3 {
		return
	}
	botID, err := uuid.Parse(parts[2])
	if err != nil {
		return
	}
	bf.applyJoinGroupsChoice(ctx, chatID, callerID, botID, action == "on")
}

func (bf *BotFather) callbackSetMenu(ctx context.Context, chatID, callerID uuid.UUID, action string, parts []string) {
	if len(parts) < 3 {
		return
	}
	botID, err := uuid.Parse(parts[2])
	if err != nil {
		return
	}
	switch action {
	case "default", "commands":
		bf.applyMenuDefault(ctx, chatID, callerID, botID, action)
	case "webapp":
		bf.startMenuWebApp(ctx, chatID, callerID, botID)
	case "clear":
		bf.applyMenuClear(ctx, chatID, callerID, botID)
	}
}

func (bf *BotFather) callbackRevoke(ctx context.Context, chatID, callerID uuid.UUID, action string, parts []string) {
	switch action {
	case "yes":
		if len(parts) < 3 {
			return
		}
		botID, err := uuid.Parse(parts[2])
		if err != nil {
			return
		}
		bf.applyRevoke(ctx, chatID, callerID, botID)
	case "cancel":
		bf.state.ClearState(ctx, callerID)
		bf.reply(ctx, chatID, msgRevokeCancelled, nil)
	}
}
