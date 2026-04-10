-- Phase 8: Integration connectors, routing rules, and delivery tracking

CREATE TABLE integration_connectors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('inbound_webhook', 'outbound_webhook', 'polling')),
    bot_id UUID REFERENCES bots(id) ON DELETE SET NULL,
    config JSONB NOT NULL DEFAULT '{}',
    secret_hash TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE integration_routes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connector_id UUID NOT NULL REFERENCES integration_connectors(id) ON DELETE CASCADE,
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    event_filter TEXT,
    template TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (connector_id, chat_id)
);

CREATE TABLE integration_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connector_id UUID NOT NULL REFERENCES integration_connectors(id) ON DELETE CASCADE,
    route_id UUID REFERENCES integration_routes(id) ON DELETE SET NULL,
    external_event_id TEXT,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'delivered', 'failed', 'dead_letter')),
    orbit_message_id UUID,
    correlation_key TEXT,
    attempt_count INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5,
    last_error TEXT,
    next_retry_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_deliveries_pending ON integration_deliveries (next_retry_at)
    WHERE status IN ('pending', 'failed');
CREATE INDEX idx_deliveries_correlation ON integration_deliveries (connector_id, correlation_key)
    WHERE correlation_key IS NOT NULL;
CREATE INDEX idx_deliveries_external ON integration_deliveries (connector_id, external_event_id)
    WHERE external_event_id IS NOT NULL;
CREATE INDEX idx_deliveries_connector ON integration_deliveries (connector_id, created_at DESC);
