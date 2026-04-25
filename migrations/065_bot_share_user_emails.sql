-- Add share_user_emails opt-in to bots so corporate identity (user.email)
-- can be injected into Update.from when the bot owner explicitly opts in.
-- Defaults to FALSE for safety: existing bots see no behaviour change.
ALTER TABLE bots
    ADD COLUMN IF NOT EXISTS share_user_emails BOOLEAN NOT NULL DEFAULT FALSE;
