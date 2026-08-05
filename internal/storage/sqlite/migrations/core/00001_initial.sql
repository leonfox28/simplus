-- +goose Up
CREATE TABLE dataset_metadata (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    dataset TEXT NOT NULL CHECK (dataset = 'core'),
    schema_version INTEGER NOT NULL CHECK (schema_version >= 1)
);
INSERT INTO dataset_metadata (singleton, dataset, schema_version) VALUES (1, 'core', 1);

CREATE TABLE installation_state (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    state TEXT NOT NULL CHECK (state IN ('uninitialized', 'ready', 'maintenance')),
    instance_default_locale TEXT NOT NULL CHECK (instance_default_locale IN ('zh-CN', 'en-US')),
    created_at_utc TEXT NOT NULL
);
INSERT INTO installation_state (singleton, state, instance_default_locale, created_at_utc)
VALUES (1, 'uninitialized', 'en-US', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

-- +goose Down
DROP TABLE installation_state;
DROP TABLE dataset_metadata;
