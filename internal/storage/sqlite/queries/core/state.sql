-- name: GetInstallationState :one
SELECT state
FROM installation_state
WHERE singleton = 1;
