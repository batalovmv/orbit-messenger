-- Copyright (C) 2024 MST Corp. All rights reserved.
-- SPDX-License-Identifier: GPL-3.0-or-later

CREATE TABLE bot_audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id UUID NOT NULL REFERENCES users(id) ON DELETE SET NULL,
    bot_id UUID REFERENCES bots(id) ON DELETE SET NULL,
    action TEXT NOT NULL CHECK (action IN ('create','update','delete','token_rotate','install','uninstall','set_webhook','delete_webhook','set_commands')),
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_ip INET,
    user_agent TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_bot_audit_actor ON bot_audit_log(actor_id, created_at DESC);
CREATE INDEX idx_bot_audit_bot ON bot_audit_log(bot_id, created_at DESC);
