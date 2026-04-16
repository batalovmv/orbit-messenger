package botfather

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// cmdSetIntegration starts the /setintegration flow.
func (bf *BotFather) cmdSetIntegration(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID) {
	if bf.integrationsClient == nil {
		bf.reply(ctx, chatID, msgIntegrationNotAvailable, nil)
		return
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
		bf.showConnectorList(ctx, chatID, senderID, bots[0].ID)
		return
	}

	bf.state.SetState(ctx, senderID, &ConversationState{Step: StepIntegrationSelectBot})
	kb := buildBotListKeyboard(bots, "mybots:select")
	bf.reply(ctx, chatID, msgIntegrationSelectBot, kb)
}

// showConnectorList fetches connectors and shows them as inline keyboard.
func (bf *BotFather) showConnectorList(ctx context.Context, chatID uuid.UUID, senderID uuid.UUID, botID uuid.UUID) {
	bot, err := bf.svc.GetBot(ctx, botID)
	if err != nil || bot.OwnerID != senderID {
		bf.reply(ctx, chatID, msgBotNotFound, nil)
		return
	}
	if bot.IsSystem {
		bf.reply(ctx, chatID, msgSystemBotProtected, nil)
		return
	}

	connectors, err := bf.integrationsClient.ListConnectors(ctx)
	if err != nil {
		bf.logger.Error("failed to list connectors", "error", err)
		bf.reply(ctx, chatID, msgIntegrationNotAvailable, nil)
		return
	}

	// Filter only active connectors
	var active []connectorInfoWrapper
	for _, c := range connectors {
		if c.IsActive {
			active = append(active, connectorInfoWrapper(c))
		}
	}

	if len(active) == 0 {
		bf.reply(ctx, chatID, msgIntegrationNoConnectors, nil)
		return
	}

	bf.state.SetState(ctx, senderID, &ConversationState{Step: StepIntegrationSelectConnector, BotID: botID})
	kb := buildConnectorListKeyboard(connectors, "integration:select")
	bf.reply(ctx, chatID, fmt.Sprintf("Выбери коннектор для привязки к @%s:", bot.Username), kb)
}

// connectorInfoWrapper exists just to avoid import issues
type connectorInfoWrapper = struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	DisplayName string     `json:"display_name"`
	Type        string     `json:"type"`
	BotID       *uuid.UUID `json:"bot_id,omitempty"`
	IsActive    bool       `json:"is_active"`
	CreatedBy   uuid.UUID  `json:"created_by"`
}

// callbackIntegration handles integration-related callbacks.
func (bf *BotFather) callbackIntegration(ctx context.Context, chatID uuid.UUID, callerID uuid.UUID, action string, parts []string) {
	if bf.integrationsClient == nil {
		bf.reply(ctx, chatID, msgIntegrationNotAvailable, nil)
		return
	}

	if action != "select" || len(parts) < 3 {
		return
	}

	connectorIDStr := parts[2]

	// Get the bot from state
	state, err := bf.state.GetState(ctx, callerID)
	if err != nil || state.Step != StepIntegrationSelectConnector || state.BotID == uuid.Nil {
		bf.reply(ctx, chatID, "Сессия истекла. Начни заново: /setintegration", nil)
		return
	}

	bot, err := bf.svc.GetBot(ctx, state.BotID)
	if err != nil || bot.OwnerID != callerID {
		bf.reply(ctx, chatID, msgBotNotFound, nil)
		return
	}

	// Handle "clear" — unlink bot from any connector
	if connectorIDStr == "clear" {
		// Find connectors linked to this bot and clear them
		connectors, err := bf.integrationsClient.ListConnectors(ctx)
		if err != nil {
			bf.reply(ctx, chatID, msgIntegrationNotAvailable, nil)
			return
		}
		cleared := 0
		for _, c := range connectors {
			if c.BotID != nil && *c.BotID == bot.ID {
				if err := bf.integrationsClient.UpdateConnectorBotID(ctx, c.ID, nil); err != nil {
					bf.logger.Error("failed to clear connector bot_id", "error", err, "connector_id", c.ID)
				} else {
					cleared++
				}
			}
		}
		bf.state.ClearState(ctx, callerID)
		if cleared > 0 {
			bf.reply(ctx, chatID, msgIntegrationCleared(bot.Username), nil)
		} else {
			bf.reply(ctx, chatID, fmt.Sprintf("@%s не привязан ни к одному коннектору.", bot.Username), nil)
		}
		return
	}

	connectorID, err := uuid.Parse(connectorIDStr)
	if err != nil {
		return
	}

	// Link bot to connector (using bot's ID from bots table — FK references bots.id)
	botID := bot.ID
	if err := bf.integrationsClient.UpdateConnectorBotID(ctx, connectorID, &botID); err != nil {
		bf.logger.Error("failed to update connector bot_id", "error", err, "connector_id", connectorID)
		bf.reply(ctx, chatID, msgInternalError, nil)
		return
	}

	// Find connector name for the response
	connectors, _ := bf.integrationsClient.ListConnectors(ctx)
	connectorName := connectorID.String()
	for _, c := range connectors {
		if c.ID == connectorID {
			connectorName = c.DisplayName
			if connectorName == "" {
				connectorName = c.Name
			}
			break
		}
	}

	bf.state.ClearState(ctx, callerID)
	bf.reply(ctx, chatID, msgIntegrationDone(bot.Username, connectorName), nil)
}
