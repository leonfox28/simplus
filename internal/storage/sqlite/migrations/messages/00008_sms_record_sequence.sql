-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys = OFF;
BEGIN IMMEDIATE;

CREATE TABLE sms_messages_v8 (
    record_sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id TEXT NOT NULL UNIQUE CHECK (length(message_id) BETWEEN 16 AND 128),
    operation_id TEXT NOT NULL UNIQUE CHECK (length(operation_id) BETWEEN 16 AND 128),
    direction TEXT NOT NULL CHECK (direction IN ('outbound', 'inbound')),
    line_id TEXT NOT NULL CHECK (length(line_id) BETWEEN 1 AND 64),
    remote_address TEXT NOT NULL CHECK (length(remote_address) BETWEEN 1 AND 21),
    body TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 1600),
    status TEXT NOT NULL CHECK (status IN ('queued', 'unconfirmed', 'sent', 'failed', 'received')),
    provider_message_id TEXT NOT NULL DEFAULT '' CHECK (length(provider_message_id) <= 128),
    error_code TEXT NOT NULL DEFAULT '' CHECK (length(error_code) <= 64),
    created_at_unix_ms INTEGER NOT NULL CHECK (created_at_unix_ms > 0),
    updated_at_unix_ms INTEGER NOT NULL CHECK (updated_at_unix_ms >= created_at_unix_ms),
    sent_at_unix_ms INTEGER,
    CHECK (
        (status = 'queued' AND direction = 'outbound' AND provider_message_id = '' AND error_code = '' AND sent_at_unix_ms IS NULL)
        OR (status = 'unconfirmed' AND direction = 'outbound' AND error_code <> '' AND sent_at_unix_ms IS NULL)
        OR (status = 'sent' AND direction = 'outbound' AND provider_message_id <> '' AND error_code = '' AND sent_at_unix_ms >= created_at_unix_ms)
        OR (status = 'failed' AND direction = 'outbound' AND error_code <> '' AND sent_at_unix_ms IS NULL)
        OR (status = 'received' AND direction = 'inbound' AND provider_message_id <> '' AND error_code = '' AND sent_at_unix_ms IS NULL)
    )
);

INSERT INTO sms_messages_v8 (
    record_sequence, message_id, operation_id, direction, line_id, remote_address, body, status,
    provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
)
SELECT ROW_NUMBER() OVER (
           ORDER BY CASE direction WHEN 'inbound' THEN updated_at_unix_ms ELSE created_at_unix_ms END,
                    created_at_unix_ms,
                    message_id
       ),
       message_id, operation_id, direction, line_id, remote_address, body, status,
       provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
FROM sms_messages;

DROP TABLE sms_messages;
ALTER TABLE sms_messages_v8 RENAME TO sms_messages;

CREATE UNIQUE INDEX sms_messages_inbound_source_idx
    ON sms_messages(line_id, provider_message_id)
    WHERE direction = 'inbound' AND provider_message_id <> '';
CREATE INDEX sms_messages_remote_sequence_idx
    ON sms_messages(remote_address, record_sequence DESC);
CREATE INDEX sms_messages_line_remote_sequence_idx
    ON sms_messages(line_id, remote_address, record_sequence DESC);

UPDATE dataset_metadata SET schema_version = 8 WHERE singleton = 1;
COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
PRAGMA foreign_keys = OFF;
BEGIN IMMEDIATE;

CREATE TABLE sms_messages_v7 (
    message_id TEXT PRIMARY KEY CHECK (length(message_id) BETWEEN 16 AND 128),
    operation_id TEXT NOT NULL UNIQUE CHECK (length(operation_id) BETWEEN 16 AND 128),
    direction TEXT NOT NULL CHECK (direction IN ('outbound', 'inbound')),
    line_id TEXT NOT NULL CHECK (length(line_id) BETWEEN 1 AND 64),
    remote_address TEXT NOT NULL CHECK (length(remote_address) BETWEEN 1 AND 21),
    body TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 1600),
    status TEXT NOT NULL CHECK (status IN ('queued', 'unconfirmed', 'sent', 'failed', 'received')),
    provider_message_id TEXT NOT NULL DEFAULT '' CHECK (length(provider_message_id) <= 128),
    error_code TEXT NOT NULL DEFAULT '' CHECK (length(error_code) <= 64),
    created_at_unix_ms INTEGER NOT NULL CHECK (created_at_unix_ms > 0),
    updated_at_unix_ms INTEGER NOT NULL CHECK (updated_at_unix_ms >= created_at_unix_ms),
    sent_at_unix_ms INTEGER,
    CHECK (
        (status = 'queued' AND direction = 'outbound' AND provider_message_id = '' AND error_code = '' AND sent_at_unix_ms IS NULL)
        OR (status = 'unconfirmed' AND direction = 'outbound' AND error_code <> '' AND sent_at_unix_ms IS NULL)
        OR (status = 'sent' AND direction = 'outbound' AND provider_message_id <> '' AND error_code = '' AND sent_at_unix_ms >= created_at_unix_ms)
        OR (status = 'failed' AND direction = 'outbound' AND error_code <> '' AND sent_at_unix_ms IS NULL)
        OR (status = 'received' AND direction = 'inbound' AND provider_message_id <> '' AND error_code = '' AND sent_at_unix_ms IS NULL)
    )
) WITHOUT ROWID;

INSERT INTO sms_messages_v7 (
    message_id, operation_id, direction, line_id, remote_address, body, status,
    provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
)
SELECT message_id, operation_id, direction, line_id, remote_address, body, status,
       provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
FROM sms_messages
ORDER BY record_sequence;

DROP TABLE sms_messages;
ALTER TABLE sms_messages_v7 RENAME TO sms_messages;

CREATE INDEX sms_messages_created_at_idx
    ON sms_messages(created_at_unix_ms DESC, message_id);
CREATE INDEX sms_messages_line_created_at_idx
    ON sms_messages(line_id, created_at_unix_ms DESC, message_id);
CREATE UNIQUE INDEX sms_messages_inbound_source_idx
    ON sms_messages(line_id, provider_message_id)
    WHERE direction = 'inbound' AND provider_message_id <> '';
CREATE INDEX sms_messages_page_idx
    ON sms_messages(created_at_unix_ms DESC, message_id DESC);
CREATE INDEX sms_messages_conversation_page_idx
    ON sms_messages(line_id, remote_address, created_at_unix_ms DESC, message_id DESC);
CREATE INDEX sms_messages_remote_page_idx
    ON sms_messages(remote_address, created_at_unix_ms DESC, message_id DESC);

UPDATE dataset_metadata SET schema_version = 7 WHERE singleton = 1;
COMMIT;
PRAGMA foreign_keys = ON;
