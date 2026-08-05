-- +goose Up
CREATE TABLE setup_bootstrap_grant (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    token_hash BLOB NOT NULL CHECK (length(token_hash) = 32),
    created_at_unix INTEGER NOT NULL CHECK (created_at_unix > 0),
    expires_at_unix INTEGER NOT NULL CHECK (expires_at_unix > created_at_unix),
    consumed_at_unix INTEGER CHECK (consumed_at_unix IS NULL OR consumed_at_unix >= created_at_unix)
);

CREATE TABLE setup_sessions (
    token_hash BLOB PRIMARY KEY CHECK (length(token_hash) = 32),
    created_at_unix INTEGER NOT NULL CHECK (created_at_unix > 0),
    expires_at_unix INTEGER NOT NULL CHECK (expires_at_unix > created_at_unix),
    selected_flow TEXT CHECK (selected_flow IS NULL OR selected_flow IN ('create-new', 'restore')),
    updated_at_unix INTEGER NOT NULL CHECK (updated_at_unix >= created_at_unix)
) WITHOUT ROWID;

CREATE INDEX setup_sessions_expiry_idx ON setup_sessions (expires_at_unix);
UPDATE dataset_metadata SET schema_version = 2 WHERE singleton = 1;

-- +goose Down
DROP INDEX setup_sessions_expiry_idx;
DROP TABLE setup_sessions;
DROP TABLE setup_bootstrap_grant;
UPDATE dataset_metadata SET schema_version = 1 WHERE singleton = 1;
