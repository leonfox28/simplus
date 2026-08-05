-- +goose Up
-- Runtime-only administrator browser sessions. These rows survive ordinary
-- service restarts but are never backed up or restored.
CREATE TABLE administrator_sessions (
    token_hash BLOB PRIMARY KEY CHECK (length(token_hash) = 32),
    csrf_hash BLOB NOT NULL CHECK (length(csrf_hash) = 32),
    username TEXT NOT NULL,
    session_generation INTEGER NOT NULL CHECK (session_generation >= 1),
    created_at_unix INTEGER NOT NULL,
    expires_at_unix INTEGER NOT NULL,
    last_seen_at_unix INTEGER NOT NULL
) WITHOUT ROWID;

CREATE INDEX administrator_sessions_expiry_idx
    ON administrator_sessions(expires_at_unix);
UPDATE dataset_metadata SET schema_version = 4 WHERE singleton = 1;

-- +goose Down
DROP INDEX administrator_sessions_expiry_idx;
DROP TABLE administrator_sessions;
UPDATE dataset_metadata SET schema_version = 3 WHERE singleton = 1;
