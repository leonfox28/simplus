-- +goose Up
CREATE TABLE notification_deliveries (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 event_key TEXT NOT NULL,
 channel_id TEXT NOT NULL,
 event_kind TEXT NOT NULL,
 object_id TEXT NOT NULL,
 connection_kind TEXT NOT NULL DEFAULT '',
 message TEXT NOT NULL CHECK(length(message) BETWEEN 1 AND 4000),
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','delivering','delivered','failed','cancelled')),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts >= 0),
 next_at INTEGER NOT NULL,
 observed_at INTEGER NOT NULL,
 last_error TEXT NOT NULL DEFAULT '',
 UNIQUE(event_key, channel_id)
);
CREATE INDEX notification_deliveries_due ON notification_deliveries(state,next_at,id);
CREATE INDEX notification_deliveries_order ON notification_deliveries(channel_id,object_id,connection_kind,id);
CREATE TABLE notification_subscriptions (
 channel_id TEXT NOT NULL,
 event_kind TEXT NOT NULL,
 subscribed_at INTEGER NOT NULL,
 PRIMARY KEY(channel_id,event_kind)
);
INSERT INTO notification_subscriptions(channel_id,event_kind,subscribed_at)
SELECT c.id,j.value,CAST(unixepoch(c.created_at_utc,'subsec')*1000 AS INTEGER)
FROM (SELECT id,enabled,event_kinds,created_at_utc FROM notification_channels
 UNION ALL SELECT id,enabled,event_kinds,created_at_utc FROM feishu_app_notification_channels) c,
 json_each(c.event_kinds) j WHERE c.enabled=1;
CREATE TABLE connection_checkpoints (
 object_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('vowifi','cellular')),
 connected INTEGER NOT NULL CHECK(connected IN (0,1)),
 revision INTEGER NOT NULL,
 observed_at INTEGER NOT NULL,
 PRIMARY KEY(object_id,kind)
);
CREATE TABLE connection_cursors (
 source TEXT PRIMARY KEY,
 instance TEXT NOT NULL,
 sequence INTEGER NOT NULL CHECK(sequence>=0),
 gaps INTEGER NOT NULL DEFAULT 0
);
UPDATE dataset_metadata SET schema_version=2 WHERE singleton=1;
-- +goose Down
DROP TABLE connection_cursors;
DROP TABLE connection_checkpoints;
DROP TABLE notification_deliveries;
DROP TABLE notification_subscriptions;
UPDATE dataset_metadata SET schema_version=1 WHERE singleton=1;
