-- Prevent two concurrent active/ringing calls from being created for the same chat.
CREATE UNIQUE INDEX IF NOT EXISTS idx_calls_chat_active_unique
    ON calls (chat_id)
    WHERE status IN ('ringing', 'active');
