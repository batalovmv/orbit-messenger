-- Phase 8: Bot identity, tokens, commands, and chat installations

CREATE TABLE bots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    owner_id UUID NOT NULL REFERENCES users(id),
    description TEXT,
    short_description TEXT,
    is_system BOOLEAN NOT NULL DEFAULT false,
    is_inline BOOLEAN NOT NULL DEFAULT false,
    webhook_url TEXT,
    webhook_secret_hash TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_bots_owner ON bots (owner_id);
CREATE INDEX idx_bots_active ON bots (is_active) WHERE is_active = true;

CREATE TABLE bot_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    token_prefix TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_bot_tokens_hash ON bot_tokens (token_hash) WHERE is_active = true;
CREATE INDEX idx_bot_tokens_bot ON bot_tokens (bot_id);

CREATE TABLE bot_commands (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    command TEXT NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (bot_id, command)
);

CREATE TABLE bot_installations (
    bot_id UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    installed_by UUID NOT NULL REFERENCES users(id),
    scopes BIGINT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bot_id, chat_id)
);

CREATE INDEX idx_bot_installations_chat ON bot_installations (chat_id) WHERE is_active = true;
