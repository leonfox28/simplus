-- +goose Up
CREATE TABLE management_tls (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    mode TEXT NOT NULL CHECK (mode IN ('loopback-only', 'local-ca', 'imported')),
    listen_host TEXT NOT NULL CHECK (length(listen_host) BETWEEN 1 AND 255),
    listen_port INTEGER NOT NULL CHECK (listen_port BETWEEN 1 AND 65535),
    subject_alternative_names TEXT NOT NULL,
    ca_certificate_pem BLOB NOT NULL,
    leaf_certificate_pem BLOB NOT NULL,
    encrypted_ca_private_key BLOB NOT NULL,
    encrypted_leaf_private_key BLOB NOT NULL,
    root_fingerprint_sha256 TEXT NOT NULL CHECK (length(root_fingerprint_sha256) IN (0, 95)),
    leaf_not_after_utc TEXT,
    confirmed INTEGER NOT NULL CHECK (confirmed IN (0, 1)),
    configured_at_utc TEXT NOT NULL,
    CHECK (
        (mode = 'loopback-only'
            AND listen_host IN ('127.0.0.1', '::1', 'localhost')
            AND length(subject_alternative_names) = 0
            AND length(ca_certificate_pem) = 0
            AND length(leaf_certificate_pem) = 0
            AND length(encrypted_ca_private_key) = 0
            AND length(encrypted_leaf_private_key) = 0
            AND length(root_fingerprint_sha256) = 0
            AND leaf_not_after_utc IS NULL
            AND confirmed = 1)
        OR
        (mode IN ('local-ca', 'imported')
            AND length(subject_alternative_names) > 0
            AND length(ca_certificate_pem) > 0
            AND length(leaf_certificate_pem) > 0
            AND length(encrypted_leaf_private_key) > 0
            AND length(root_fingerprint_sha256) = 95
            AND leaf_not_after_utc IS NOT NULL)
    )
);

UPDATE dataset_metadata SET schema_version = 5 WHERE singleton = 1;

-- +goose Down
DROP TABLE management_tls;
UPDATE dataset_metadata SET schema_version = 4 WHERE singleton = 1;
