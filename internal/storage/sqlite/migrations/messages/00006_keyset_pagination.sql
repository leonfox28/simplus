-- +goose Up
CREATE INDEX sms_messages_page_idx
    ON sms_messages(created_at_unix_ms DESC, message_id DESC);
CREATE INDEX sms_messages_conversation_page_idx
    ON sms_messages(line_id, remote_address, created_at_unix_ms DESC, message_id DESC);
UPDATE dataset_metadata SET schema_version = 6 WHERE singleton = 1;

-- +goose Down
DROP INDEX sms_messages_conversation_page_idx;
DROP INDEX sms_messages_page_idx;
UPDATE dataset_metadata SET schema_version = 5 WHERE singleton = 1;
