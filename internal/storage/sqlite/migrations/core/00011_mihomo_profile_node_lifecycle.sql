-- +goose Up
-- Version 10 used a node foreign key that prevented a subscription refresh
-- from replacing nodes. Profiles must survive and become fail-closed instead.
ALTER TABLE mihomo_egress_profiles RENAME TO mihomo_egress_profiles_v10;
CREATE TABLE mihomo_egress_profiles (
    id TEXT PRIMARY KEY CHECK (id GLOB 'egress_[A-Za-z0-9_-]*'),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
    subscription_id TEXT NOT NULL REFERENCES mihomo_subscriptions(id) ON DELETE RESTRICT,
    selected_node_id TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL
);
INSERT INTO mihomo_egress_profiles SELECT * FROM mihomo_egress_profiles_v10;
DROP TABLE mihomo_egress_profiles_v10;
UPDATE dataset_metadata SET schema_version = 11 WHERE singleton = 1;

-- +goose Down
ALTER TABLE mihomo_egress_profiles RENAME TO mihomo_egress_profiles_v11;
CREATE TABLE mihomo_egress_profiles (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    subscription_id TEXT NOT NULL REFERENCES mihomo_subscriptions(id) ON DELETE RESTRICT,
    selected_node_id TEXT NOT NULL,
    enabled INTEGER NOT NULL,
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL,
    FOREIGN KEY (subscription_id, selected_node_id) REFERENCES mihomo_subscription_nodes(subscription_id, node_id) ON DELETE RESTRICT
);
INSERT INTO mihomo_egress_profiles SELECT * FROM mihomo_egress_profiles_v11;
DROP TABLE mihomo_egress_profiles_v11;
UPDATE dataset_metadata SET schema_version = 10 WHERE singleton = 1;
