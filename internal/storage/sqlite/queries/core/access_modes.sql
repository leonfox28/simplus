-- name: GetSubscriptionProfileAccessMode :one
SELECT access_mode
FROM subscription_profile_access_modes
WHERE subscription_profile_id = ?;

-- name: PutSubscriptionProfileAccessMode :exec
INSERT INTO subscription_profile_access_modes (
    subscription_profile_id,
    access_mode,
    updated_at_utc
) VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
ON CONFLICT(subscription_profile_id) DO UPDATE SET
    access_mode = excluded.access_mode,
    updated_at_utc = excluded.updated_at_utc;
