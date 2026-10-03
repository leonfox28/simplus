-- +goose Up
CREATE TABLE dataset_metadata (
 singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
 dataset TEXT NOT NULL CHECK (dataset = 'control'),
 schema_version INTEGER NOT NULL CHECK (schema_version >= 1)
);
INSERT INTO dataset_metadata VALUES (1, 'control', 1);

CREATE TABLE administrators (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    username TEXT NOT NULL CHECK (
        length(username) BETWEEN 3 AND 32
        AND username = lower(username)
        AND username NOT GLOB '*[^a-z0-9._-]*'
        AND substr(username, 1, 1) GLOB '[a-z0-9]'
    ),
    password_hash TEXT NOT NULL CHECK (
        length(password_hash) BETWEEN 64 AND 512
        AND password_hash LIKE '$argon2id$%'
    ),
    password_version INTEGER NOT NULL CHECK (password_version >= 1),
    session_generation INTEGER NOT NULL CHECK (session_generation >= 1),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL
);

CREATE TABLE feishu_app_notification_channels (
    id TEXT PRIMARY KEY CHECK (
        length(id) = 30
        AND id GLOB 'channel_[A-Za-z0-9_-]*'
        AND substr(id, 9) NOT GLOB '*[^A-Za-z0-9_-]*'
    ),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
    app_id_ciphertext BLOB NOT NULL CHECK (length(app_id_ciphertext) BETWEEN 32 AND 8192),
    app_secret_ciphertext BLOB NOT NULL CHECK (length(app_secret_ciphertext) BETWEEN 32 AND 8192),
    recipient_open_id_ciphertext BLOB NOT NULL CHECK (length(recipient_open_id_ciphertext) BETWEEN 32 AND 8192),
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    event_kinds TEXT NOT NULL CHECK (
        length(event_kinds) BETWEEN 2 AND 512
        AND json_valid(event_kinds)
        AND json_type(event_kinds) = 'array'
        AND json_array_length(event_kinds) BETWEEN 1 AND 9
    ),
    last_delivery_at_utc TEXT,
    last_delivery_status TEXT NOT NULL CHECK (last_delivery_status IN ('never', 'success', 'failed')),
    last_error_code TEXT NOT NULL DEFAULT '' CHECK (length(last_error_code) <= 64),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL
);

CREATE TABLE installation_state (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    state TEXT NOT NULL CHECK (state IN ('uninitialized', 'ready', 'maintenance')),
    instance_default_locale TEXT NOT NULL CHECK (instance_default_locale IN ('zh-CN', 'en-US')),
    created_at_utc TEXT NOT NULL
, initialized_at_utc TEXT, instance_generation INTEGER NOT NULL DEFAULT 1 CHECK (instance_generation >= 1));

CREATE TABLE line_egress_bindings (
    line_id TEXT PRIMARY KEY CHECK (length(line_id) BETWEEN 1 AND 64),
    mode TEXT NOT NULL CHECK (mode IN ('direct', 'mihomo-country')),
    country_code TEXT NOT NULL CHECK (
        (mode = 'direct' AND country_code = '') OR
        (mode = 'mihomo-country' AND length(country_code) = 2 AND country_code = upper(country_code))
    ),
    updated_at_utc TEXT NOT NULL
);

CREATE TABLE "managed_lines" (
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

CREATE TABLE "managed_modems" (
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


CREATE TABLE mihomo_runtime_selection (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    selected_subscription_id TEXT NOT NULL DEFAULT '',
    running_subscription_id TEXT NOT NULL DEFAULT '',
    updated_at_utc TEXT NOT NULL
);

CREATE TABLE mihomo_subscription_nodes (
    subscription_id TEXT NOT NULL REFERENCES mihomo_subscriptions(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL CHECK (node_id GLOB 'node_[A-Za-z0-9_-]*'),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 120),
    kind TEXT NOT NULL CHECK (length(kind) BETWEEN 1 AND 32), proxy_yaml TEXT NOT NULL DEFAULT '', country_code TEXT NOT NULL DEFAULT '', country_name TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (subscription_id, node_id)
);

CREATE TABLE mihomo_subscriptions (
    id TEXT PRIMARY KEY CHECK (id GLOB 'subscription_[A-Za-z0-9_-]*'),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
    url_ciphertext BLOB NOT NULL CHECK (length(url_ciphertext) BETWEEN 32 AND 8192),
    url_hint TEXT NOT NULL CHECK (length(url_hint) BETWEEN 1 AND 255),
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    last_refresh_at_utc TEXT,
    last_refresh_status TEXT NOT NULL CHECK (last_refresh_status IN ('never', 'success', 'failed')),
    node_count INTEGER NOT NULL DEFAULT 0 CHECK (node_count BETWEEN 0 AND 10000),
    last_error_code TEXT NOT NULL DEFAULT '' CHECK (length(last_error_code) <= 64),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL
, url_plaintext TEXT NOT NULL DEFAULT '');

CREATE TABLE notification_channels (
    id TEXT PRIMARY KEY CHECK (id GLOB 'channel_[A-Za-z0-9_-]*'),
    provider TEXT NOT NULL CHECK (provider IN ('wecom', 'feishu')),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
    webhook_ciphertext BLOB NOT NULL CHECK (length(webhook_ciphertext) BETWEEN 32 AND 8192),
    webhook_hint TEXT NOT NULL CHECK (length(webhook_hint) BETWEEN 1 AND 255),
    signing_secret_ciphertext BLOB,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    event_kinds TEXT NOT NULL CHECK (length(event_kinds) BETWEEN 2 AND 512),
    last_delivery_at_utc TEXT,
    last_delivery_status TEXT NOT NULL CHECK (last_delivery_status IN ('never', 'success', 'failed')),
    last_error_code TEXT NOT NULL DEFAULT '' CHECK (length(last_error_code) <= 64),
    created_at_utc TEXT NOT NULL,
    updated_at_utc TEXT NOT NULL
);



CREATE TABLE simulator_euicc_profiles (
    profile_id TEXT PRIMARY KEY CHECK (profile_id IN ('simulator-euicc-profile-a', 'simulator-euicc-profile-b')),
    display_name TEXT NOT NULL,
    display_identity_hint TEXT NOT NULL,
    active INTEGER NOT NULL CHECK (active IN (0, 1))
) WITHOUT ROWID;

CREATE TABLE vowifi_line_desires (
    line_id TEXT PRIMARY KEY CHECK (length(line_id) BETWEEN 1 AND 64),
    desired_active INTEGER NOT NULL CHECK (desired_active IN (0, 1)),
    updated_at_utc TEXT NOT NULL
);

CREATE UNIQUE INDEX managed_modems_equipment_identity_unique
    ON managed_modems(equipment_identity_fingerprint)
    WHERE equipment_identity_fingerprint <> '';

CREATE UNIQUE INDEX simulator_euicc_one_active_idx ON simulator_euicc_profiles(active) WHERE active = 1;

CREATE TABLE contacts (
    contact_id TEXT PRIMARY KEY CHECK (length(contact_id) BETWEEN 16 AND 128),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
    phone_number TEXT NOT NULL UNIQUE CHECK (length(phone_number) BETWEEN 3 AND 21),
    created_at_unix_ms INTEGER NOT NULL CHECK (created_at_unix_ms > 0),
    updated_at_unix_ms INTEGER NOT NULL CHECK (updated_at_unix_ms >= created_at_unix_ms)
) WITHOUT ROWID;

CREATE INDEX contacts_display_name_idx
    ON contacts(display_name COLLATE NOCASE, contact_id);

CREATE TABLE sms_inbound_fragments (
    group_id TEXT NOT NULL CHECK (length(group_id) BETWEEN 16 AND 128),
    part INTEGER NOT NULL CHECK (part BETWEEN 1 AND 255),
    source_message_id TEXT NOT NULL CHECK (length(source_message_id) BETWEEN 16 AND 128),
    line_id TEXT NOT NULL CHECK (length(line_id) BETWEEN 1 AND 64),
    sender TEXT NOT NULL CHECK (length(sender) BETWEEN 1 AND 21),
    encoding TEXT NOT NULL CHECK (encoding IN ('gsm7', 'ucs2')),
    concat_reference INTEGER NOT NULL CHECK (concat_reference BETWEEN 0 AND 255),
    total INTEGER NOT NULL CHECK (total BETWEEN 2 AND 255 AND part <= total),
    unit_count INTEGER NOT NULL CHECK (unit_count BETWEEN 1 AND 255),
    user_data BLOB NOT NULL CHECK (length(user_data) BETWEEN 1 AND 140),
    received_at_unix_ms INTEGER NOT NULL CHECK (received_at_unix_ms > 0),
    PRIMARY KEY (group_id, part),
    UNIQUE (line_id, source_message_id)
) WITHOUT ROWID;

CREATE TABLE sms_message_unread (
    unread_id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id TEXT NOT NULL UNIQUE
        REFERENCES sms_messages(message_id) ON DELETE CASCADE,
    remote_address TEXT NOT NULL CHECK (length(remote_address) BETWEEN 1 AND 21)
);

CREATE TABLE "sms_messages" (
    record_sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id TEXT NOT NULL UNIQUE CHECK (length(message_id) BETWEEN 16 AND 128),
    operation_id TEXT NOT NULL UNIQUE CHECK (length(operation_id) BETWEEN 16 AND 128),
    direction TEXT NOT NULL CHECK (direction IN ('outbound', 'inbound')),
    line_id TEXT NOT NULL CHECK (length(line_id) BETWEEN 1 AND 64),
    remote_address TEXT NOT NULL CHECK (length(remote_address) BETWEEN 1 AND 21),
    body TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 1600),
    status TEXT NOT NULL CHECK (status IN ('queued', 'unconfirmed', 'sent', 'failed', 'received')),
    provider_message_id TEXT NOT NULL DEFAULT '' CHECK (length(provider_message_id) <= 128),
    error_code TEXT NOT NULL DEFAULT '' CHECK (length(error_code) <= 64),
    created_at_unix_ms INTEGER NOT NULL CHECK (created_at_unix_ms > 0),
    updated_at_unix_ms INTEGER NOT NULL CHECK (updated_at_unix_ms >= created_at_unix_ms),
    sent_at_unix_ms INTEGER,
    CHECK (
        (status = 'queued' AND direction = 'outbound' AND provider_message_id = '' AND error_code = '' AND sent_at_unix_ms IS NULL)
        OR (status = 'unconfirmed' AND direction = 'outbound' AND error_code <> '' AND sent_at_unix_ms IS NULL)
        OR (status = 'sent' AND direction = 'outbound' AND provider_message_id <> '' AND error_code = '' AND sent_at_unix_ms >= created_at_unix_ms)
        OR (status = 'failed' AND direction = 'outbound' AND error_code <> '' AND sent_at_unix_ms IS NULL)
        OR (status = 'received' AND direction = 'inbound' AND provider_message_id <> '' AND error_code = '' AND sent_at_unix_ms IS NULL)
    )
);

CREATE INDEX sms_inbound_fragments_group_candidate_idx
    ON sms_inbound_fragments(line_id, sender, encoding, concat_reference, total, received_at_unix_ms);

CREATE INDEX sms_inbound_fragments_received_at_idx
    ON sms_inbound_fragments(received_at_unix_ms);

CREATE INDEX sms_message_unread_remote_idx
    ON sms_message_unread(remote_address, unread_id);

CREATE UNIQUE INDEX sms_messages_inbound_source_idx
    ON sms_messages(line_id, provider_message_id)
    WHERE direction = 'inbound' AND provider_message_id <> '';

CREATE INDEX sms_messages_line_remote_sequence_idx
    ON sms_messages(line_id, remote_address, record_sequence DESC);

CREATE INDEX sms_messages_remote_sequence_idx
    ON sms_messages(remote_address, record_sequence DESC);

CREATE TABLE call_records (
    call_id TEXT PRIMARY KEY CHECK (length(call_id) BETWEEN 16 AND 128),
    operation_id TEXT NOT NULL UNIQUE CHECK (length(operation_id) BETWEEN 16 AND 128),
    line_id TEXT NOT NULL CHECK (length(line_id) BETWEEN 1 AND 64),
    remote_address TEXT NOT NULL CHECK (length(remote_address) BETWEEN 3 AND 21),
    direction TEXT NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    state TEXT NOT NULL CHECK (state IN ('incoming', 'dialing', 'active', 'ended', 'failed')),
    end_reason TEXT NOT NULL DEFAULT '' CHECK (length(end_reason) <= 64),
    created_at_unix_ms INTEGER NOT NULL CHECK (created_at_unix_ms > 0),
    updated_at_unix_ms INTEGER NOT NULL CHECK (updated_at_unix_ms >= created_at_unix_ms),
    answered_at_unix_ms INTEGER,
    ended_at_unix_ms INTEGER
) WITHOUT ROWID;

CREATE INDEX call_records_created_at_idx ON call_records(created_at_unix_ms DESC, call_id);

CREATE INDEX call_records_page_idx
    ON call_records(created_at_unix_ms DESC, call_id DESC);

CREATE TABLE administrator_sessions (
    token_hash BLOB PRIMARY KEY CHECK (length(token_hash) = 32),
    csrf_hash BLOB NOT NULL CHECK (length(csrf_hash) = 32),
    username TEXT NOT NULL,
    session_generation INTEGER NOT NULL CHECK (session_generation >= 1),
    created_at_unix INTEGER NOT NULL,
    expires_at_unix INTEGER NOT NULL,
    last_seen_at_unix INTEGER NOT NULL
) WITHOUT ROWID;



CREATE INDEX administrator_sessions_expiry_idx
    ON administrator_sessions(expires_at_unix);


INSERT INTO installation_state (singleton, state, instance_default_locale, created_at_utc)
VALUES (1, 'uninitialized', 'zh-CN', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
INSERT INTO mihomo_runtime_selection VALUES (1, '', '', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
INSERT INTO simulator_euicc_profiles VALUES
 ('simulator-euicc-profile-a', 'Simulator eUICC Profile A', 'ICCID •••• 2001', 1),
 ('simulator-euicc-profile-b', 'Simulator eUICC Profile B', 'ICCID •••• 2002', 0);

-- +goose Down
PRAGMA defer_foreign_keys = ON;
DROP TABLE administrator_sessions;
DROP TABLE call_records;
DROP TABLE sms_messages;
DROP TABLE sms_message_unread;
DROP TABLE sms_inbound_fragments;
DROP TABLE contacts;
DROP TABLE vowifi_line_desires;
DROP TABLE simulator_euicc_profiles;
DROP TABLE notification_channels;
DROP TABLE mihomo_subscriptions;
DROP TABLE mihomo_subscription_nodes;
DROP TABLE mihomo_runtime_selection;
DROP TABLE managed_modems;
DROP TABLE managed_lines;
DROP TABLE line_egress_bindings;
DROP TABLE installation_state;
DROP TABLE feishu_app_notification_channels;
DROP TABLE administrators;
DROP TABLE dataset_metadata;
