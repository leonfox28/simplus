-- +goose Up
CREATE TABLE managed_modems (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 16 AND 64 AND substr(id, 1, 6) = 'modem_'),
    hardware_device_id TEXT NOT NULL UNIQUE CHECK (length(hardware_device_id) BETWEEN 1 AND 64),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 120),
    model TEXT NOT NULL CHECK (length(model) BETWEEN 1 AND 120),
    transport TEXT NOT NULL CHECK (transport IN ('simulated', 'usb', 'uart')),
    capability_mask INTEGER NOT NULL CHECK (capability_mask BETWEEN 0 AND 16383),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL
);
UPDATE dataset_metadata SET schema_version = 18 WHERE singleton = 1;

-- +goose Down
DROP TABLE managed_modems;
UPDATE dataset_metadata SET schema_version = 17 WHERE singleton = 1;
