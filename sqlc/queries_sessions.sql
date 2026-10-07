-- name: CreateSession :one
INSERT INTO sessions (jti, user_id, expires_at, user_agent, client_ip)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, jti, user_id, issued_at, expires_at, revoked_at, user_agent, client_ip;

-- name: GetSessionByJTI :one
SELECT id, jti, user_id, issued_at, expires_at, revoked_at, user_agent, client_ip
FROM sessions
WHERE jti = $1
LIMIT 1;

-- name: RevokeSession :exec
UPDATE sessions
SET revoked_at = CURRENT_TIMESTAMP
WHERE jti = $1
  AND revoked_at IS NULL;

-- name: RevokeAllUserSessions :exec
UPDATE sessions
SET revoked_at = CURRENT_TIMESTAMP
WHERE user_id = $1
  AND revoked_at IS NULL;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions
WHERE expires_at < CURRENT_TIMESTAMP
   OR revoked_at IS NOT NULL;