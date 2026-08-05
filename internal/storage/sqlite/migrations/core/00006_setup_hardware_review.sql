-- +goose Up
CREATE TABLE setup_hardware_review (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    inventory_digest_sha256 TEXT NOT NULL CHECK (length(inventory_digest_sha256) = 64),
    device_count INTEGER NOT NULL CHECK (device_count >= 0),
    line_count INTEGER NOT NULL CHECK (line_count >= 0),
    reviewed_at_utc TEXT NOT NULL
);

UPDATE dataset_metadata SET schema_version = 6 WHERE singleton = 1;

-- +goose Down
DROP TABLE setup_hardware_review;
UPDATE dataset_metadata SET schema_version = 5 WHERE singleton = 1;
