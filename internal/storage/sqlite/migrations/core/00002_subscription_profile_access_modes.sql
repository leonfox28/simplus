-- +goose Up
CREATE TABLE subscription_profile_access_modes (
    subscription_profile_id TEXT PRIMARY KEY
        CHECK (
            length(subscription_profile_id) BETWEEN 1 AND 64
            AND subscription_profile_id NOT GLOB '*[^a-z0-9-]*'
            AND subscription_profile_id GLOB '[a-z0-9]*'
        ),
    access_mode TEXT NOT NULL
        CHECK (access_mode IN ('cellular-native', 'host-vowifi-only', 'hold-rf-off')),
    updated_at_utc TEXT NOT NULL
);
UPDATE dataset_metadata SET schema_version = 2 WHERE singleton = 1;

-- +goose Down
DROP TABLE subscription_profile_access_modes;
UPDATE dataset_metadata SET schema_version = 1 WHERE singleton = 1;
