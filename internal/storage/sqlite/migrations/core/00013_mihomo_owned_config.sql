-- +goose Up
ALTER TABLE mihomo_subscriptions ADD COLUMN url_plaintext TEXT NOT NULL DEFAULT '';
ALTER TABLE mihomo_subscription_nodes ADD COLUMN proxy_yaml TEXT NOT NULL DEFAULT '';
ALTER TABLE mihomo_subscription_nodes ADD COLUMN country_code TEXT NOT NULL DEFAULT '';
ALTER TABLE mihomo_subscription_nodes ADD COLUMN country_name TEXT NOT NULL DEFAULT '';
ALTER TABLE mihomo_egress_profiles ADD COLUMN selection_type TEXT NOT NULL DEFAULT 'node';
ALTER TABLE mihomo_egress_profiles ADD COLUMN selected_country_code TEXT NOT NULL DEFAULT '';
ALTER TABLE mihomo_egress_profiles ADD COLUMN source_cidr TEXT NOT NULL DEFAULT '';
UPDATE dataset_metadata SET schema_version = 13 WHERE singleton = 1;

-- +goose Down
UPDATE dataset_metadata SET schema_version = 12 WHERE singleton = 1;
