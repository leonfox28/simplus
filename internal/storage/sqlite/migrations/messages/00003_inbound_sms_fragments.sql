-- +goose Up
CREATE TABLE sms_inbound_fragments (
    group_id TEXT NOT NULL CHECK (length(group_id) BETWEEN 16 AND 128),
    part INTEGER NOT NULL CHECK (part BETWEEN 1 AND 255),
    source_message_id TEXT NOT NULL CHECK (length(source_message_id) BETWEEN 16 AND 128),
    line_id TEXT NOT NULL CHECK (length(line_id) BETWEEN 1 AND 64),
    sender TEXT NOT NULL CHECK (length(sender) BETWEEN 1 AND 21),
    encoding TEXT NOT NULL CHECK (encoding IN ('gsm7', 'ucs2')),
    concat_reference INTEGER NOT NULL CHECK (concat_reference BETWEEN 0 AND 255),
    total INTEGER NOT NULL CHECK (total BETWEEN 2 AND 255 AND part <= total),
    unit_count INTEGER NOT NULL CHECK (unit_count BETWEEN 1 AND 255),
    user_data BLOB NOT NULL CHECK (length(user_data) BETWEEN 1 AND 140),
    received_at_unix_ms INTEGER NOT NULL CHECK (received_at_unix_ms > 0),
    PRIMARY KEY (group_id, part),
    UNIQUE (line_id, source_message_id)
) WITHOUT ROWID;

CREATE INDEX sms_inbound_fragments_received_at_idx
    ON sms_inbound_fragments(received_at_unix_ms);
CREATE INDEX sms_inbound_fragments_group_candidate_idx
    ON sms_inbound_fragments(line_id, sender, encoding, concat_reference, total, received_at_unix_ms);

UPDATE dataset_metadata SET schema_version = 3 WHERE singleton = 1;

-- +goose Down
DROP INDEX sms_inbound_fragments_group_candidate_idx;
DROP INDEX sms_inbound_fragments_received_at_idx;
DROP TABLE sms_inbound_fragments;
UPDATE dataset_metadata SET schema_version = 2 WHERE singleton = 1;
