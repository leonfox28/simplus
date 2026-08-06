-- +goose Up
CREATE TABLE managed_modems_v19 (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 16 AND 64 AND substr(id, 1, 6) = 'modem_'),
    legacy_hardware_device_id TEXT NOT NULL DEFAULT '' CHECK (length(legacy_hardware_device_id) <= 64),
    equipment_identity_fingerprint TEXT NOT NULL DEFAULT '' CHECK (equipment_identity_fingerprint = '' OR length(equipment_identity_fingerprint) = 64),
    usb_serial_fingerprint TEXT NOT NULL DEFAULT '' CHECK (usb_serial_fingerprint = '' OR length(usb_serial_fingerprint) = 64),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 120),
    model TEXT NOT NULL CHECK (length(model) BETWEEN 1 AND 120),
    transport TEXT NOT NULL CHECK (transport IN ('simulated', 'usb', 'uart')),
    capability_mask INTEGER NOT NULL CHECK (capability_mask BETWEEN 0 AND 16383),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL
);
INSERT INTO managed_modems_v19 (
    id, legacy_hardware_device_id, equipment_identity_fingerprint, usb_serial_fingerprint,
    display_name, model, transport, capability_mask, created_at_utc, updated_at_utc
)
SELECT id, hardware_device_id, '', '', display_name, model, transport, capability_mask, created_at_utc, updated_at_utc
FROM managed_modems;
DROP TABLE managed_modems;
ALTER TABLE managed_modems_v19 RENAME TO managed_modems;
CREATE UNIQUE INDEX managed_modems_equipment_identity_unique
    ON managed_modems(equipment_identity_fingerprint)
    WHERE equipment_identity_fingerprint <> '';
UPDATE dataset_metadata SET schema_version = 19 WHERE singleton = 1;

-- +goose Down
CREATE TABLE managed_modems_v18 (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 16 AND 64 AND substr(id, 1, 6) = 'modem_'),
    hardware_device_id TEXT NOT NULL UNIQUE CHECK (length(hardware_device_id) BETWEEN 1 AND 64),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 120),
    model TEXT NOT NULL CHECK (length(model) BETWEEN 1 AND 120),
    transport TEXT NOT NULL CHECK (transport IN ('simulated', 'usb', 'uart')),
    capability_mask INTEGER NOT NULL CHECK (capability_mask BETWEEN 0 AND 16383),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL
);
INSERT INTO managed_modems_v18 (
    id, hardware_device_id, display_name, model, transport, capability_mask, created_at_utc, updated_at_utc
)
SELECT id,
       CASE WHEN legacy_hardware_device_id <> '' THEN legacy_hardware_device_id ELSE 'migrated-' || substr(id, 7) END,
       display_name, model, transport, capability_mask, created_at_utc, updated_at_utc
FROM managed_modems;
DROP TABLE managed_modems;
ALTER TABLE managed_modems_v18 RENAME TO managed_modems;
UPDATE dataset_metadata SET schema_version = 18 WHERE singleton = 1;
