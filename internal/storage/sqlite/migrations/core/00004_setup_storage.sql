-- +goose Up
CREATE TABLE setup_storage (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    data_root TEXT NOT NULL CHECK (substr(data_root, 1, 1) = '/' AND length(data_root) <= 4096),
    recordings_root TEXT NOT NULL CHECK (substr(recordings_root, 1, 1) = '/' AND length(recordings_root) <= 4096),
    data_device INTEGER NOT NULL CHECK (data_device >= 0),
    data_inode INTEGER NOT NULL CHECK (data_inode > 0),
    recordings_device INTEGER NOT NULL CHECK (recordings_device >= 0),
    recordings_inode INTEGER NOT NULL CHECK (recordings_inode > 0),
    configured_at_utc TEXT NOT NULL
);

UPDATE dataset_metadata SET schema_version = 4 WHERE singleton = 1;

-- +goose Down
DROP TABLE setup_storage;
UPDATE dataset_metadata SET schema_version = 3 WHERE singleton = 1;
