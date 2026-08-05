-- +goose Up
CREATE TABLE dataset_metadata (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    dataset TEXT NOT NULL CHECK (dataset = 'messages'),
    schema_version INTEGER NOT NULL CHECK (schema_version >= 1)
);
INSERT INTO dataset_metadata (singleton, dataset, schema_version) VALUES (1, 'messages', 1);

-- +goose Down
DROP TABLE dataset_metadata;
