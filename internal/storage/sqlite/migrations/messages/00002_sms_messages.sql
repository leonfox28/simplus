-- +goose Up
CREATE TABLE sms_messages (
    message_id TEXT PRIMARY KEY CHECK (length(message_id) BETWEEN 16 AND 128),
    operation_id TEXT NOT NULL UNIQUE CHECK (length(operation_id) BETWEEN 16 AND 128),
    direction TEXT NOT NULL CHECK (direction IN ('outbound', 'inbound')),
    line_id TEXT NOT NULL CHECK (length(line_id) BETWEEN 1 AND 64),
    remote_address TEXT NOT NULL CHECK (length(remote_address) BETWEEN 1 AND 21),
    body TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 1600),
    status TEXT NOT NULL CHECK (status IN ('queued', 'sent', 'failed', 'received')),
    provider_message_id TEXT NOT NULL DEFAULT '' CHECK (length(provider_message_id) <= 128),
    error_code TEXT NOT NULL DEFAULT '' CHECK (length(error_code) <= 64),
    created_at_unix_ms INTEGER NOT NULL CHECK (created_at_unix_ms > 0),
    updated_at_unix_ms INTEGER NOT NULL CHECK (updated_at_unix_ms >= created_at_unix_ms),
    sent_at_unix_ms INTEGER,
    CHECK (
        (status = 'queued' AND direction = 'outbound' AND provider_message_id = '' AND error_code = '' AND sent_at_unix_ms IS NULL)
        OR (status = 'sent' AND direction = 'outbound' AND provider_message_id <> '' AND error_code = '' AND sent_at_unix_ms >= created_at_unix_ms)
        OR (status = 'failed' AND direction = 'outbound' AND provider_message_id = '' AND error_code <> '' AND sent_at_unix_ms IS NULL)
        OR (status = 'received' AND direction = 'inbound' AND provider_message_id <> '' AND error_code = '' AND sent_at_unix_ms IS NULL)
    )
) WITHOUT ROWID;

CREATE INDEX sms_messages_created_at_idx
    ON sms_messages(created_at_unix_ms DESC, message_id);
CREATE INDEX sms_messages_line_created_at_idx
    ON sms_messages(line_id, created_at_unix_ms DESC, message_id);
CREATE UNIQUE INDEX sms_messages_inbound_source_idx
    ON sms_messages(line_id, provider_message_id)
    WHERE direction = 'inbound' AND provider_message_id <> '';

UPDATE dataset_metadata SET schema_version = 2 WHERE singleton = 1;

-- +goose Down
DROP INDEX sms_messages_inbound_source_idx;
DROP INDEX sms_messages_line_created_at_idx;
DROP INDEX sms_messages_created_at_idx;
DROP TABLE sms_messages;
UPDATE dataset_metadata SET schema_version = 1 WHERE singleton = 1;
