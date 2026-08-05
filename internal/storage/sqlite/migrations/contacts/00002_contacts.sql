-- +goose Up
CREATE TABLE contacts (
    contact_id TEXT PRIMARY KEY CHECK (length(contact_id) BETWEEN 16 AND 128),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
    phone_number TEXT NOT NULL UNIQUE CHECK (length(phone_number) BETWEEN 3 AND 21),
    created_at_unix_ms INTEGER NOT NULL CHECK (created_at_unix_ms > 0),
    updated_at_unix_ms INTEGER NOT NULL CHECK (updated_at_unix_ms >= created_at_unix_ms)
) WITHOUT ROWID;

CREATE INDEX contacts_display_name_idx
    ON contacts(display_name COLLATE NOCASE, contact_id);

UPDATE dataset_metadata SET schema_version = 2 WHERE singleton = 1;

-- +goose Down
DROP INDEX contacts_display_name_idx;
DROP TABLE contacts;
UPDATE dataset_metadata SET schema_version = 1 WHERE singleton = 1;
