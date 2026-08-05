-- +goose Up
CREATE TABLE mihomo_runtime_selection (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    selected_subscription_id TEXT NOT NULL DEFAULT '',
    running_subscription_id TEXT NOT NULL DEFAULT '',
    updated_at_utc TEXT NOT NULL
);
INSERT INTO mihomo_runtime_selection(singleton, selected_subscription_id, running_subscription_id, updated_at_utc)
VALUES (1, '', '', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
UPDATE dataset_metadata SET schema_version = 15 WHERE singleton = 1;

-- +goose Down
DROP TABLE mihomo_runtime_selection;
UPDATE dataset_metadata SET schema_version = 14 WHERE singleton = 1;
