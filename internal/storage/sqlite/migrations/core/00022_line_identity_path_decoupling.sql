-- +goose NO TRANSACTION

-- +goose Up
PRAGMA foreign_keys = OFF;
BEGIN IMMEDIATE;

-- Versions 13-15 stored an obsolete per-Line Mihomo profile. Preserve an
-- enabled country choice only when the newer Line binding has no value.
INSERT OR IGNORE INTO line_egress_bindings (line_id, mode, country_code, updated_at_utc)
SELECT profile.line_id, 'mihomo-country', profile.selected_country_code, profile.updated_at_utc
FROM mihomo_egress_profiles AS profile
JOIN managed_lines AS line ON line.id = profile.line_id
WHERE profile.enabled = 1
  AND profile.selection_type = 'country'
  AND length(profile.selected_country_code) = 2
  AND profile.selected_country_code = upper(profile.selected_country_code);

-- Older Host VoWiFi Lines treated a missing binding as implicit direct.
-- Materialize that choice before absence becomes the fail-closed
-- "unconfigured" state.
INSERT INTO line_egress_bindings (line_id, mode, country_code, updated_at_utc)
SELECT line.id, 'direct', '', line.updated_at_utc
FROM managed_lines AS line
LEFT JOIN line_egress_bindings AS binding ON binding.line_id = line.id
WHERE line.access_mode = 'host-vowifi-only' AND binding.line_id IS NULL;

CREATE TABLE managed_lines_next (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 16 AND 64 AND substr(id, 1, 5) = 'line_'),
    managed_modem_id TEXT NOT NULL REFERENCES managed_modems(id) ON DELETE RESTRICT,
    sim_slot_index INTEGER NOT NULL CHECK (sim_slot_index BETWEEN 0 AND 255),
    subscription_identity_fingerprint TEXT NOT NULL CHECK (length(subscription_identity_fingerprint) = 64),
    subscription_display_hint TEXT NOT NULL CHECK (length(subscription_display_hint) BETWEEN 1 AND 64),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 120),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL,
    UNIQUE (managed_modem_id, subscription_identity_fingerprint)
);
INSERT INTO managed_lines_next (
    id, managed_modem_id, sim_slot_index, subscription_identity_fingerprint,
    subscription_display_hint, display_name, created_at_utc, updated_at_utc
)
SELECT id, managed_modem_id, sim_slot_index, subscription_identity_fingerprint,
       subscription_display_hint, display_name, created_at_utc, updated_at_utc
FROM managed_lines;
DROP TABLE managed_lines;
ALTER TABLE managed_lines_next RENAME TO managed_lines;
DROP TABLE subscription_profile_access_modes;
DROP TABLE simulator_access_paths;
DROP TABLE mihomo_egress_profiles;
UPDATE dataset_metadata SET schema_version = 22 WHERE singleton = 1;

COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
PRAGMA foreign_keys = OFF;
BEGIN IMMEDIATE;

CREATE TABLE subscription_profile_access_modes (
    subscription_profile_id TEXT PRIMARY KEY,
    access_mode TEXT NOT NULL CHECK (access_mode IN ('cellular-native', 'host-vowifi-only', 'hold-rf-off')),
    updated_at_utc TEXT NOT NULL
) WITHOUT ROWID;

CREATE TABLE simulator_access_paths (
    line_id TEXT PRIMARY KEY REFERENCES managed_lines(id) ON DELETE CASCADE,
    mode TEXT NOT NULL CHECK (mode IN ('direct', 'mihomo-required')),
    mihomo_state TEXT NOT NULL CHECK (mihomo_state IN ('running', 'stopped', 'failed'))
) WITHOUT ROWID;

CREATE TABLE mihomo_egress_profiles (
    id TEXT PRIMARY KEY CHECK (id GLOB 'egress_[A-Za-z0-9_-]*'),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
    subscription_id TEXT NOT NULL REFERENCES mihomo_subscriptions(id) ON DELETE RESTRICT,
    selected_node_id TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL,
    selection_type TEXT NOT NULL DEFAULT 'node',
    selected_country_code TEXT NOT NULL DEFAULT '',
    source_cidr TEXT NOT NULL DEFAULT '',
    line_id TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX mihomo_egress_profiles_line_unique
    ON mihomo_egress_profiles(line_id) WHERE line_id <> '';

CREATE TABLE managed_lines_previous (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 16 AND 64 AND substr(id, 1, 5) = 'line_'),
    managed_modem_id TEXT NOT NULL REFERENCES managed_modems(id) ON DELETE RESTRICT,
    sim_slot_index INTEGER NOT NULL CHECK (sim_slot_index BETWEEN 0 AND 255),
    subscription_identity_fingerprint TEXT NOT NULL CHECK (length(subscription_identity_fingerprint) = 64),
    subscription_display_hint TEXT NOT NULL CHECK (length(subscription_display_hint) BETWEEN 1 AND 64),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 120),
    access_mode TEXT NOT NULL CHECK (access_mode IN ('cellular-native', 'host-vowifi-only', 'hold-rf-off')),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL,
    UNIQUE (managed_modem_id, subscription_identity_fingerprint)
);
INSERT INTO managed_lines_previous (
    id, managed_modem_id, sim_slot_index, subscription_identity_fingerprint,
    subscription_display_hint, display_name, access_mode, created_at_utc, updated_at_utc
)
SELECT line.id, line.managed_modem_id, line.sim_slot_index, line.subscription_identity_fingerprint,
       line.subscription_display_hint, line.display_name,
       CASE WHEN binding.line_id IS NOT NULL OR desire.line_id IS NOT NULL
            THEN 'host-vowifi-only' ELSE 'hold-rf-off' END,
       line.created_at_utc, line.updated_at_utc
FROM managed_lines AS line
LEFT JOIN line_egress_bindings AS binding ON binding.line_id = line.id
LEFT JOIN vowifi_line_desires AS desire ON desire.line_id = line.id;
DROP TABLE managed_lines;
ALTER TABLE managed_lines_previous RENAME TO managed_lines;
UPDATE dataset_metadata SET schema_version = 21 WHERE singleton = 1;

COMMIT;
PRAGMA foreign_keys = ON;
