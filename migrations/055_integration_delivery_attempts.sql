-- Phase 8F.1: Per-delivery retry attempts history.
-- Each webhook outbound attempt gets its own row so admins can inspect the
-- full timeline (HTTP status, response body snippet, error) rather than only
-- the latest error stored in integration_deliveries.last_error.

CREATE TABLE integration_delivery_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    delivery_id UUID NOT NULL REFERENCES integration_deliveries(id) ON DELETE CASCADE,
    attempt_no INT NOT NULL,
    status TEXT NOT NULL,
    response_status INT,
    response_body_snippet TEXT,
    error TEXT,
    ran_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_delivery_attempts_delivery ON integration_delivery_attempts (delivery_id, attempt_no);
