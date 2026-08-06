-- +goose Up
DROP TABLE simulator_vowifi_lines;
CREATE TABLE simulator_access_paths (
    line_id TEXT PRIMARY KEY REFERENCES managed_lines(id) ON DELETE CASCADE,
    mode TEXT NOT NULL CHECK (mode IN ('direct','mihomo-required')),
    mihomo_state TEXT NOT NULL CHECK (mihomo_state IN ('running','stopped','failed'))
) WITHOUT ROWID;
UPDATE dataset_metadata SET schema_version = 21 WHERE singleton = 1;

-- +goose Down
DROP TABLE simulator_access_paths;
CREATE TABLE simulator_vowifi_lines (
    line_id TEXT PRIMARY KEY CHECK (line_id IN ('simulator-line-1','simulator-line-2')),
    mode TEXT NOT NULL CHECK (mode IN ('direct','mihomo-required')),
    mihomo_state TEXT NOT NULL CHECK (mihomo_state IN ('running','stopped','failed'))
) WITHOUT ROWID;
INSERT INTO simulator_vowifi_lines VALUES ('simulator-line-1','direct','stopped'),('simulator-line-2','direct','stopped');
UPDATE dataset_metadata SET schema_version = 20 WHERE singleton = 1;
