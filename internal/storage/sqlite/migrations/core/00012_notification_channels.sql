-- +goose Up
CREATE TABLE notification_channels (
    id TEXT PRIMARY KEY CHECK (id GLOB 'channel_[A-Za-z0-9_-]*'),
    provider TEXT NOT NULL CHECK (provider IN ('wecom', 'feishu')),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
    webhook_ciphertext BLOB NOT NULL CHECK (length(webhook_ciphertext) BETWEEN 32 AND 8192),
    webhook_hint TEXT NOT NULL CHECK (length(webhook_hint) BETWEEN 1 AND 255),
    signing_secret_ciphertext BLOB,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    event_kinds TEXT NOT NULL CHECK (length(event_kinds) BETWEEN 2 AND 512),
    last_delivery_at_utc TEXT,
    last_delivery_status TEXT NOT NULL CHECK (last_delivery_status IN ('never', 'success', 'failed')),
    last_error_code TEXT NOT NULL DEFAULT '' CHECK (length(last_error_code) <= 64),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL
);
UPDATE dataset_metadata SET schema_version = 12 WHERE singleton = 1;

-- +goose Down
DROP TABLE notification_channels;
UPDATE dataset_metadata SET schema_version = 11 WHERE singleton = 1;
