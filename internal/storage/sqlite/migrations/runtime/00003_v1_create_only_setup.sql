-- +goose Up
CREATE TABLE setup_sessions_v1 (
    token_hash BLOB PRIMARY KEY CHECK (length(token_hash) = 32),
    created_at_unix INTEGER NOT NULL CHECK (created_at_unix > 0),
    expires_at_unix INTEGER NOT NULL CHECK (expires_at_unix > created_at_unix),
    selected_flow TEXT NOT NULL CHECK (selected_flow = 'create-new'),
    updated_at_unix INTEGER NOT NULL CHECK (updated_at_unix >= created_at_unix)
) WITHOUT ROWID;

INSERT INTO setup_sessions_v1 (
    token_hash, created_at_unix, expires_at_unix, selected_flow, updated_at_unix
)
SELECT token_hash, created_at_unix, expires_at_unix, 'create-new', updated_at_unix
FROM setup_sessions;

DROP INDEX setup_sessions_expiry_idx;
DROP TABLE setup_sessions;
ALTER TABLE setup_sessions_v1 RENAME TO setup_sessions;
CREATE INDEX setup_sessions_expiry_idx ON setup_sessions (expires_at_unix);
UPDATE dataset_metadata SET schema_version = 3 WHERE singleton = 1;

-- +goose Down
CREATE TABLE setup_sessions_v2 (
    token_hash BLOB PRIMARY KEY CHECK (length(token_hash) = 32),
    created_at_unix INTEGER NOT NULL CHECK (created_at_unix > 0),
    expires_at_unix INTEGER NOT NULL CHECK (expires_at_unix > created_at_unix),
    selected_flow TEXT CHECK (selected_flow IS NULL OR selected_flow IN ('create-new', 'restore')),
    updated_at_unix INTEGER NOT NULL CHECK (updated_at_unix >= created_at_unix)
) WITHOUT ROWID;

INSERT INTO setup_sessions_v2
SELECT token_hash, created_at_unix, expires_at_unix, selected_flow, updated_at_unix
FROM setup_sessions;

DROP INDEX setup_sessions_expiry_idx;
DROP TABLE setup_sessions;
ALTER TABLE setup_sessions_v2 RENAME TO setup_sessions;
CREATE INDEX setup_sessions_expiry_idx ON setup_sessions (expires_at_unix);
UPDATE dataset_metadata SET schema_version = 2 WHERE singleton = 1;
