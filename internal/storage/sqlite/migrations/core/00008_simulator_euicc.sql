-- +goose Up
CREATE TABLE simulator_euicc_profiles (
    profile_id TEXT PRIMARY KEY CHECK (profile_id IN ('simulator-euicc-profile-a', 'simulator-euicc-profile-b')),
    display_name TEXT NOT NULL,
    display_identity_hint TEXT NOT NULL,
    active INTEGER NOT NULL CHECK (active IN (0, 1))
) WITHOUT ROWID;
CREATE UNIQUE INDEX simulator_euicc_one_active_idx ON simulator_euicc_profiles(active) WHERE active = 1;
INSERT INTO simulator_euicc_profiles VALUES
 ('simulator-euicc-profile-a', 'Simulator eUICC Profile A', 'ICCID •••• 2001', 1),
 ('simulator-euicc-profile-b', 'Simulator eUICC Profile B', 'ICCID •••• 2002', 0);
UPDATE dataset_metadata SET schema_version = 8 WHERE singleton = 1;

-- +goose Down
DROP INDEX simulator_euicc_one_active_idx;
DROP TABLE simulator_euicc_profiles;
UPDATE dataset_metadata SET schema_version = 7 WHERE singleton = 1;
