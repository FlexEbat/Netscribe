-- name: InsertAudit :exec
INSERT INTO audit_log (at, user_id, username, action, entity, entity_id, result, ip, detail)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListAudit :many
SELECT * FROM audit_log
WHERE (sqlc.arg(before_id) = 0 OR id < sqlc.arg(before_id))
  AND (sqlc.arg(action) = '' OR action = sqlc.arg(action))
  AND (sqlc.arg(username) = '' OR username = sqlc.arg(username))
  AND (sqlc.arg(result) = '' OR result = sqlc.arg(result))
  AND (sqlc.arg(from_at) = '' OR at >= sqlc.arg(from_at))
  AND (sqlc.arg(to_at) = '' OR at < sqlc.arg(to_at))
ORDER BY id DESC
LIMIT sqlc.arg(max_rows);

-- name: PruneAudit :exec
DELETE FROM audit_log WHERE at < ?;
