-- +goose Up
CREATE TABLE call_records (
    call_id TEXT PRIMARY KEY CHECK (length(call_id) BETWEEN 16 AND 128),
    operation_id TEXT NOT NULL UNIQUE CHECK (length(operation_id) BETWEEN 16 AND 128),
    line_id TEXT NOT NULL CHECK (length(line_id) BETWEEN 1 AND 64),
    remote_address TEXT NOT NULL CHECK (length(remote_address) BETWEEN 3 AND 21),
    direction TEXT NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    state TEXT NOT NULL CHECK (state IN ('incoming', 'dialing', 'active', 'ended', 'failed')),
    end_reason TEXT NOT NULL DEFAULT '' CHECK (length(end_reason) <= 64),
    created_at_unix_ms INTEGER NOT NULL CHECK (created_at_unix_ms > 0),
    updated_at_unix_ms INTEGER NOT NULL CHECK (updated_at_unix_ms >= created_at_unix_ms),
    answered_at_unix_ms INTEGER,
    ended_at_unix_ms INTEGER
) WITHOUT ROWID;

CREATE INDEX call_records_created_at_idx ON call_records(created_at_unix_ms DESC, call_id);
UPDATE dataset_metadata SET schema_version = 2 WHERE singleton = 1;

-- +goose Down
DROP INDEX call_records_created_at_idx;
DROP TABLE call_records;
UPDATE dataset_metadata SET schema_version = 1 WHERE singleton = 1;
