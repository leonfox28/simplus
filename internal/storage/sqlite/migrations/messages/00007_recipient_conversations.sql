-- +goose Up
CREATE INDEX sms_messages_remote_page_idx
    ON sms_messages(remote_address, created_at_unix_ms DESC, message_id DESC);

CREATE TABLE sms_message_unread (
    unread_id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id TEXT NOT NULL UNIQUE
        REFERENCES sms_messages(message_id) ON DELETE CASCADE,
    remote_address TEXT NOT NULL CHECK (length(remote_address) BETWEEN 1 AND 21)
);

CREATE INDEX sms_message_unread_remote_idx
    ON sms_message_unread(remote_address, unread_id);

UPDATE dataset_metadata SET schema_version = 7 WHERE singleton = 1;

-- +goose Down
DROP INDEX sms_message_unread_remote_idx;
DROP TABLE sms_message_unread;
DROP INDEX sms_messages_remote_page_idx;
UPDATE dataset_metadata SET schema_version = 6 WHERE singleton = 1;
