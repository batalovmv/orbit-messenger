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
	StepSetWebhookSelectBot  = "setwebhook:select_bot"
	StepSetWebhookAwait      = "setwebhook:await_value"
	StepSetWebhookAwaitSecret = "setwebhook:await_secret"

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

	// /setabouttext
	StepSetAboutSelectBot = "setabouttext:select_bot"
	StepSetAboutAwait     = "setabouttext:await_value"

	// /setprivacy
	StepSetPrivacySelectBot = "setprivacy:select_bot"
	StepSetPrivacyChoice    = "setprivacy:choice"

	// /setinline
	StepSetInlineSelectBot         = "setinline:select_bot"
	StepSetInlineChoice            = "setinline:choice"
	StepSetInlineAwaitPlaceholder  = "setinline:await_placeholder"

	// /setjoingroups
	StepSetJoinGroupsSelectBot = "setjoingroups:select_bot"
	StepSetJoinGroupsChoice    = "setjoingroups:choice"

	// /setmenubutton
	StepSetMenuSelectBot   = "setmenubutton:select_bot"
	StepSetMenuChoice      = "setmenubutton:choice"
	StepSetMenuAwaitText   = "setmenubutton:await_text"
	StepSetMenuAwaitURL    = "setmenubutton:await_url"

	// /revoke
	StepRevokeSelectBot = "revoke:select_bot"
	StepRevokeConfirm   = "revoke:confirm"

	// /setuserpic
	StepSetUserpicSelectBot = "setuserpic:select_bot"
	StepSetUserpicAwait     = "setuserpic:await_photo"
)

// ConversationState holds the current BotFather conversation state for a user.
type ConversationState struct {
	Step  string    `json:"step"`
	BotID uuid.UUID `json:"bot_id,omitempty"`
	Data  string    `json:"data,omitempty"` // temporary storage (e.g. bot name during /newbot)
}
