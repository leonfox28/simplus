-- +goose Up
ALTER TABLE mihomo_egress_profiles ADD COLUMN line_id TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX mihomo_egress_profiles_line_unique ON mihomo_egress_profiles(line_id) WHERE line_id <> '';
UPDATE dataset_metadata SET schema_version = 14 WHERE singleton = 1;

-- +goose Down
DROP INDEX mihomo_egress_profiles_line_unique;
UPDATE dataset_metadata SET schema_version = 13 WHERE singleton = 1;
