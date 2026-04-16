package botfather

import "github.com/google/uuid"

// Conversation steps
const (
	StepNone = ""

	// /newbot
	StepNewBotAskName     = "newbot:ask_name"
	StepNewBotAskUsername = "newbot:ask_username"

	// /setname
	StepSetNameSelectBot = "setname:select_bot"
	StepSetNameAwait     = "setname:await_value"

	// /setdescription
	StepSetDescSelectBot = "setdescription:select_bot"
	StepSetDescAwait     = "setdescription:await_value"

	// /setwebhook
	StepSetWebhookSelectBot = "setwebhook:select_bot"
	StepSetWebhookAwait     = "setwebhook:await_value"

	// /setcommands
	StepSetCmdsSelectBot = "setcommands:select_bot"
	StepSetCmdsAwait     = "setcommands:await_commands"

	// /deletebot
	StepDeleteSelectBot = "deletebot:select_bot"
	StepDeleteConfirm   = "deletebot:confirm"

	// /token
	StepTokenSelectBot = "token:select_bot"
	StepTokenActions   = "token:actions"
	StepTokenConfirm   = "token:confirm_rotate"

	// /setintegration
	StepIntegrationSelectBot       = "integration:select_bot"
	StepIntegrationSelectConnector = "integration:select_connector"
)

// ConversationState holds the current BotFather conversation state for a user.
type ConversationState struct {
	Step  string    `json:"step"`
	BotID uuid.UUID `json:"bot_id,omitempty"`
	Data  string    `json:"data,omitempty"` // temporary storage (e.g. bot name during /newbot)
}
