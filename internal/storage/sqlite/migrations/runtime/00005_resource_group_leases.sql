-- +goose Up
CREATE TABLE resource_group_fences (
    resource_group_id TEXT PRIMARY KEY,
    resource_group_generation INTEGER NOT NULL CHECK (resource_group_generation > 0),
    fencing_token INTEGER NOT NULL CHECK (fencing_token > 0)
) WITHOUT ROWID;

CREATE TABLE resource_group_leases (
    lease_id TEXT PRIMARY KEY,
    operation_id TEXT NOT NULL UNIQUE,
    resource_group_id TEXT NOT NULL,
    lease_kind TEXT NOT NULL CHECK (lease_kind IN ('operation', 'call')),
    purpose TEXT NOT NULL,
    holder TEXT NOT NULL,
    resource_group_generation INTEGER NOT NULL CHECK (resource_group_generation > 0),
    fencing_token INTEGER NOT NULL CHECK (fencing_token > 0),
    created_at_unix INTEGER NOT NULL CHECK (created_at_unix > 0),
    expires_at_unix INTEGER NOT NULL CHECK (expires_at_unix > created_at_unix),
    CHECK (length(lease_id) BETWEEN 16 AND 128),
    CHECK (length(operation_id) BETWEEN 1 AND 128),
    CHECK (length(resource_group_id) BETWEEN 1 AND 64),
    CHECK (length(purpose) BETWEEN 1 AND 64),
    CHECK (length(holder) BETWEEN 1 AND 128)
);

CREATE INDEX resource_group_leases_active_idx
    ON resource_group_leases(resource_group_id, expires_at_unix, lease_kind);

-- Used operation IDs are deliberately retained after lease expiry so delayed retries cannot reacquire a new fence.
-- A future bounded durable command-outcome ledger must assume dedupe ownership before these rows may be pruned.
CREATE TABLE resource_group_lease_operations (
    operation_id TEXT PRIMARY KEY,
    lease_id TEXT NOT NULL UNIQUE,
    resource_group_id TEXT NOT NULL,
    lease_kind TEXT NOT NULL CHECK (lease_kind IN ('operation', 'call')),
    purpose TEXT NOT NULL,
    holder TEXT NOT NULL,
    resource_group_generation INTEGER NOT NULL CHECK (resource_group_generation > 0),
    fencing_token INTEGER NOT NULL CHECK (fencing_token > 0),
    created_at_unix INTEGER NOT NULL CHECK (created_at_unix > 0),
    CHECK (length(operation_id) BETWEEN 1 AND 128),
    CHECK (length(lease_id) BETWEEN 16 AND 128),
    CHECK (length(resource_group_id) BETWEEN 1 AND 64),
    CHECK (length(purpose) BETWEEN 1 AND 64),
    CHECK (length(holder) BETWEEN 1 AND 128)
) WITHOUT ROWID;

UPDATE dataset_metadata SET schema_version = 5 WHERE singleton = 1;

-- +goose Down
DROP TABLE IF EXISTS resource_group_lease_operations;
DROP TABLE IF EXISTS resource_group_leases;
DROP TABLE IF EXISTS resource_group_fences;
UPDATE dataset_metadata SET schema_version = 4 WHERE singleton = 1;
