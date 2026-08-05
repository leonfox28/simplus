-- +goose Up
CREATE TABLE mihomo_subscriptions (
    id TEXT PRIMARY KEY CHECK (id GLOB 'subscription_[A-Za-z0-9_-]*'),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
    url_ciphertext BLOB NOT NULL CHECK (length(url_ciphertext) BETWEEN 32 AND 8192),
    url_hint TEXT NOT NULL CHECK (length(url_hint) BETWEEN 1 AND 255),
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    last_refresh_at_utc TEXT,
    last_refresh_status TEXT NOT NULL CHECK (last_refresh_status IN ('never', 'success', 'failed')),
    node_count INTEGER NOT NULL DEFAULT 0 CHECK (node_count BETWEEN 0 AND 10000),
    last_error_code TEXT NOT NULL DEFAULT '' CHECK (length(last_error_code) <= 64),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL
);

CREATE TABLE mihomo_subscription_nodes (
    subscription_id TEXT NOT NULL REFERENCES mihomo_subscriptions(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL CHECK (node_id GLOB 'node_[A-Za-z0-9_-]*'),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 120),
    kind TEXT NOT NULL CHECK (length(kind) BETWEEN 1 AND 32),
    PRIMARY KEY (subscription_id, node_id)
);

CREATE TABLE mihomo_egress_profiles (
    id TEXT PRIMARY KEY CHECK (id GLOB 'egress_[A-Za-z0-9_-]*'),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
    subscription_id TEXT NOT NULL REFERENCES mihomo_subscriptions(id) ON DELETE RESTRICT,
    selected_node_id TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL
);

UPDATE dataset_metadata SET schema_version = 10 WHERE singleton = 1;

-- +goose Down
DROP TABLE mihomo_egress_profiles;
DROP TABLE mihomo_subscription_nodes;
DROP TABLE mihomo_subscriptions;
UPDATE dataset_metadata SET schema_version = 9 WHERE singleton = 1;
