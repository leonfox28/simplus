-- name: EnqueueNotification :exec
INSERT INTO notification_deliveries(event_key,channel_id,event_kind,object_id,connection_kind,message,next_at,observed_at)
SELECT sqlc.arg(event_key) AS event_key, c.id AS channel_id, sqlc.arg(event_kind) AS event_kind, sqlc.arg(object_id) AS object_id, sqlc.arg(connection_kind) AS connection_kind,
 CASE WHEN c.provider='feishu' AND sqlc.arg(feishu_message)<>'' THEN sqlc.arg(feishu_message) ELSE sqlc.arg(message) END AS message,
 sqlc.arg(now) AS next_at,sqlc.arg(observed_at) AS observed_at
FROM (SELECT id,provider,enabled,event_kinds FROM notification_channels
 UNION ALL SELECT id,'feishu' AS provider,enabled,event_kinds FROM feishu_app_notification_channels) AS c
WHERE c.enabled=1 AND EXISTS (SELECT 1 FROM notification_subscriptions s WHERE s.channel_id=c.id AND s.event_kind=sqlc.arg(event_kind) AND s.subscribed_at<=sqlc.arg(subscription_at)) AND EXISTS (SELECT 1 FROM json_each(c.event_kinds) WHERE value=sqlc.arg(event_kind))
ON CONFLICT(event_key,channel_id) DO NOTHING;

-- name: ClaimNotificationDeliveries :many
UPDATE notification_deliveries SET state='delivering', attempts=attempts+1, next_at=sqlc.arg(lease_until)
WHERE id IN (
 SELECT d.id FROM notification_deliveries d
 WHERE d.state IN ('pending','delivering') AND d.next_at<=sqlc.arg(now)
 AND NOT EXISTS(SELECT 1 FROM notification_deliveries previous
   WHERE previous.channel_id=d.channel_id AND previous.object_id=d.object_id
   AND previous.connection_kind=d.connection_kind AND previous.id<d.id
   AND previous.state IN ('pending','delivering'))
 ORDER BY d.id LIMIT sqlc.arg(batch_limit)
)
RETURNING id,channel_id,event_kind,object_id,connection_kind,message,attempts;

-- name: FinishNotificationDelivery :exec
UPDATE notification_deliveries SET state=sqlc.arg(state),next_at=sqlc.arg(next_at),last_error=sqlc.arg(last_error)
WHERE id=sqlc.arg(id) AND state='delivering' AND attempts=sqlc.arg(attempts);

-- name: ResetInterruptedNotifications :exec
UPDATE notification_deliveries SET state='pending',next_at=sqlc.arg(now) WHERE state='delivering';

-- name: CountNotificationDeliveries :one
SELECT CAST(COALESCE(SUM(CASE WHEN state IN ('pending','delivering') THEN 1 ELSE 0 END),0) AS INTEGER) AS pending,
 CAST(COALESCE(SUM(CASE WHEN state='failed' THEN 1 ELSE 0 END),0) AS INTEGER) AS failed
FROM notification_deliveries WHERE channel_id=?;

-- name: CancelChannelNotifications :exec
UPDATE notification_deliveries SET state='cancelled',last_error='SUBSCRIPTION_CANCELLED'
WHERE channel_id=sqlc.arg(channel_id) AND state IN ('pending','delivering')
AND (sqlc.arg(enabled)=0 OR event_kind NOT IN (SELECT value FROM json_each(sqlc.arg(event_kinds))));

-- name: ReadConnectionCheckpoint :one
SELECT connected,revision,observed_at FROM connection_checkpoints WHERE object_id=? AND kind=?;

-- name: WriteConnectionCheckpoint :exec
INSERT INTO connection_checkpoints(object_id,kind,connected,revision,observed_at) VALUES(?,?,?,?,?)
ON CONFLICT(object_id,kind) DO UPDATE SET connected=excluded.connected,revision=excluded.revision,observed_at=excluded.observed_at;

-- name: ReadConnectionCursor :one
SELECT instance,sequence,gaps FROM connection_cursors WHERE source=?;

-- name: WriteConnectionCursor :exec
INSERT INTO connection_cursors(source,instance,sequence,gaps) VALUES(?,?,?,?)
ON CONFLICT(source) DO UPDATE SET instance=excluded.instance,sequence=excluded.sequence,gaps=excluded.gaps;

-- name: RemoveInactiveNotificationSubscriptions :exec
DELETE FROM notification_subscriptions WHERE channel_id=sqlc.arg(channel_id)
AND (sqlc.arg(enabled)=0 OR event_kind NOT IN(SELECT value FROM json_each(sqlc.arg(event_kinds))));

-- name: AddNotificationSubscriptions :exec
INSERT INTO notification_subscriptions(channel_id,event_kind,subscribed_at)
SELECT sqlc.arg(channel_id),value,sqlc.arg(subscribed_at) FROM json_each(sqlc.arg(event_kinds)) WHERE sqlc.arg(enabled)=1
ON CONFLICT(channel_id,event_kind) DO NOTHING;

-- name: DeleteNotificationSubscriptions :exec
DELETE FROM notification_subscriptions WHERE channel_id=?;
