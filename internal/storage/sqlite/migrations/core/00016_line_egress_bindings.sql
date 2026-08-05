-- +goose Up
CREATE TABLE line_egress_bindings (
    line_id TEXT PRIMARY KEY CHECK (length(line_id) BETWEEN 1 AND 64),
    mode TEXT NOT NULL CHECK (mode IN ('direct', 'mihomo-country')),
    country_code TEXT NOT NULL CHECK (
        (mode = 'direct' AND country_code = '') OR
        (mode = 'mihomo-country' AND length(country_code) = 2 AND country_code = upper(country_code))
    ),
    updated_at_utc TEXT NOT NULL
);
UPDATE dataset_metadata SET schema_version = 16 WHERE singleton = 1;

-- +goose Down
DROP TABLE line_egress_bindings;
UPDATE dataset_metadata SET schema_version = 15 WHERE singleton = 1;
