-- Phase 8F: HR bot template — time-off and sick-leave requests.
-- Minimal schema for 150-employee corporate messenger. No approval chains,
-- no manager hierarchy — a single "approver" role scoped to the HR chat.

CREATE TABLE IF NOT EXISTS bot_hr_requests (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id        UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    chat_id       UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_type  TEXT NOT NULL CHECK (request_type IN ('vacation', 'sick_leave', 'day_off')),
    start_date    DATE NOT NULL,
    end_date      DATE NOT NULL,
    reason        TEXT,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    approver_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    decision_note TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT bot_hr_requests_dates_ordered CHECK (end_date >= start_date)
);

CREATE INDEX IF NOT EXISTS idx_bot_hr_requests_user    ON bot_hr_requests(user_id, status);
CREATE INDEX IF NOT EXISTS idx_bot_hr_requests_chat    ON bot_hr_requests(chat_id, status);
CREATE INDEX IF NOT EXISTS idx_bot_hr_requests_pending ON bot_hr_requests(status) WHERE status = 'pending';
