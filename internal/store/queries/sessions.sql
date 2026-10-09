-- name: InsertSession :exec
INSERT INTO sessions (id_hash, user_id, csrf_token, created_at, last_seen_at, expires_at, ip, user_agent)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: TrimUserSessions :exec
DELETE FROM sessions
WHERE sessions.user_id = sqlc.arg(owner_id)
  AND sessions.id_hash NOT IN (
    SELECT newest.id_hash FROM sessions AS newest
    WHERE newest.user_id = sqlc.arg(owner_id)
    ORDER BY newest.created_at DESC, newest.rowid DESC
    LIMIT sqlc.arg(keep)
  );

-- name: GetSession :one
SELECT * FROM sessions WHERE id_hash = ?;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = ? WHERE id_hash = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id_hash = ?;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = ?;

-- name: PruneSessions :exec
DELETE FROM sessions WHERE expires_at <= ?;
