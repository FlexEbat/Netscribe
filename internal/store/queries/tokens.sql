-- name: InsertToken :one
INSERT INTO api_tokens (user_id, name, prefix, token_hash, scope, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetTokenByHash :one
SELECT * FROM api_tokens WHERE token_hash = ?;

-- name: ListTokens :many
SELECT * FROM api_tokens WHERE user_id = ? ORDER BY id DESC;

-- name: RevokeToken :execrows
UPDATE api_tokens SET revoked = 1 WHERE id = ? AND user_id = ?;

-- name: TouchToken :exec
UPDATE api_tokens SET last_used_at = ? WHERE id = ?;

-- name: CountActiveTokens :one
SELECT COUNT(*) FROM api_tokens WHERE user_id = ? AND revoked = 0 AND expires_at > ?;
