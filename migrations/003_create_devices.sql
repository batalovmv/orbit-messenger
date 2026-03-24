CREATE TABLE devices (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_name     TEXT,
    device_type     TEXT CHECK (device_type IN ('web', 'desktop', 'ios', 'android')),
    identity_key    BYTEA,
    push_token      TEXT,
    push_type       TEXT CHECK (push_type IN ('vapid', 'fcm', 'apns')),
    last_active_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
