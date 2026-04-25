-- Copyright (C) 2024 MST Corp. All rights reserved.
-- SPDX-License-Identifier: GPL-3.0-or-later

CREATE TABLE bot_approval_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    requester_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    approval_type TEXT NOT NULL CHECK (char_length(approval_type) BETWEEN 1 AND 64),
    subject TEXT NOT NULL CHECK (char_length(subject) BETWEEN 1 AND 200),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected','cancelled')),
    version INT NOT NULL DEFAULT 1,
    decided_by UUID REFERENCES users(id) ON DELETE SET NULL,
    decided_at TIMESTAMPTZ,
    decision_note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_bot_approvals_chat ON bot_approval_requests(chat_id, status);
CREATE INDEX idx_bot_approvals_requester ON bot_approval_requests(requester_id, status);
CREATE INDEX idx_bot_approvals_pending ON bot_approval_requests(status) WHERE status = 'pending';
CREATE TRIGGER trg_bot_approvals_updated_at
    BEFORE UPDATE ON bot_approval_requests
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();
