-- +goose Up
CREATE TABLE administrators (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    username TEXT NOT NULL CHECK (
        length(username) BETWEEN 3 AND 32
        AND username = lower(username)
        AND username NOT GLOB '*[^a-z0-9._-]*'
        AND substr(username, 1, 1) GLOB '[a-z0-9]'
    ),
    password_hash TEXT NOT NULL CHECK (
        length(password_hash) BETWEEN 64 AND 512
        AND password_hash LIKE '$argon2id$%'
    ),
    password_version INTEGER NOT NULL CHECK (password_version >= 1),
    session_generation INTEGER NOT NULL CHECK (session_generation >= 1),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL
);

UPDATE dataset_metadata SET schema_version = 3 WHERE singleton = 1;

-- +goose Down
DROP TABLE administrators;
UPDATE dataset_metadata SET schema_version = 2 WHERE singleton = 1;
