-- +goose Up
ALTER TABLE installation_state ADD COLUMN initialized_at_utc TEXT;
ALTER TABLE installation_state ADD COLUMN instance_generation INTEGER NOT NULL DEFAULT 1 CHECK (instance_generation >= 1);
UPDATE dataset_metadata SET schema_version = 7 WHERE singleton = 1;

-- +goose Down
ALTER TABLE installation_state DROP COLUMN instance_generation;
ALTER TABLE installation_state DROP COLUMN initialized_at_utc;
UPDATE dataset_metadata SET schema_version = 6 WHERE singleton = 1;
