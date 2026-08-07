-- +goose Up
CREATE INDEX call_records_page_idx
    ON call_records(created_at_unix_ms DESC, call_id DESC);
UPDATE dataset_metadata SET schema_version = 3 WHERE singleton = 1;

-- +goose Down
DROP INDEX call_records_page_idx;
UPDATE dataset_metadata SET schema_version = 2 WHERE singleton = 1;
