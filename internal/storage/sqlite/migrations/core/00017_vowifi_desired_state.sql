-- +goose Up
CREATE TABLE vowifi_line_desires (
    line_id TEXT PRIMARY KEY CHECK (length(line_id) BETWEEN 1 AND 64),
    desired_active INTEGER NOT NULL CHECK (desired_active IN (0, 1)),
    updated_at_utc TEXT NOT NULL
);
UPDATE dataset_metadata SET schema_version = 17 WHERE singleton = 1;

-- +goose Down
DROP TABLE vowifi_line_desires;
UPDATE dataset_metadata SET schema_version = 16 WHERE singleton = 1;
