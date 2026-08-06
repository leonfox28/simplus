-- +goose Up
CREATE TABLE managed_lines (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 16 AND 64 AND substr(id, 1, 5) = 'line_'),
    managed_modem_id TEXT NOT NULL REFERENCES managed_modems(id) ON DELETE RESTRICT,
    sim_slot_index INTEGER NOT NULL CHECK (sim_slot_index BETWEEN 0 AND 255),
    subscription_identity_fingerprint TEXT NOT NULL CHECK (length(subscription_identity_fingerprint) = 64),
    subscription_display_hint TEXT NOT NULL CHECK (length(subscription_display_hint) BETWEEN 1 AND 64),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 120),
    access_mode TEXT NOT NULL CHECK (access_mode IN ('cellular-native', 'host-vowifi-only', 'hold-rf-off')),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL,
    UNIQUE (managed_modem_id, subscription_identity_fingerprint)
);
UPDATE dataset_metadata SET schema_version = 20 WHERE singleton = 1;

-- +goose Down
DROP TABLE managed_lines;
UPDATE dataset_metadata SET schema_version = 19 WHERE singleton = 1;
