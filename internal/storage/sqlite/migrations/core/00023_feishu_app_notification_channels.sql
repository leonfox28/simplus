-- +goose Up
CREATE TABLE feishu_app_notification_channels (
    id TEXT PRIMARY KEY CHECK (
        length(id) = 30
        AND id GLOB 'channel_[A-Za-z0-9_-]*'
        AND substr(id, 9) NOT GLOB '*[^A-Za-z0-9_-]*'
    ),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
    app_id_ciphertext BLOB NOT NULL CHECK (length(app_id_ciphertext) BETWEEN 32 AND 8192),
    app_secret_ciphertext BLOB NOT NULL CHECK (length(app_secret_ciphertext) BETWEEN 32 AND 8192),
    recipient_open_id_ciphertext BLOB NOT NULL CHECK (length(recipient_open_id_ciphertext) BETWEEN 32 AND 8192),
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    event_kinds TEXT NOT NULL CHECK (
        length(event_kinds) BETWEEN 2 AND 512
        AND json_valid(event_kinds)
        AND json_type(event_kinds) = 'array'
        AND json_array_length(event_kinds) BETWEEN 1 AND 5
    ),
    last_delivery_at_utc TEXT,
    last_delivery_status TEXT NOT NULL CHECK (last_delivery_status IN ('never', 'success', 'failed')),
    last_error_code TEXT NOT NULL DEFAULT '' CHECK (length(last_error_code) <= 64),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL
);
UPDATE dataset_metadata SET schema_version = 23 WHERE singleton = 1;

-- +goose Down
DROP TABLE feishu_app_notification_channels;
UPDATE dataset_metadata SET schema_version = 22 WHERE singleton = 1;
