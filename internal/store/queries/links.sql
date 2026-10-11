-- name: InsertLink :one
INSERT INTO links (a_device_id, a_port, b_device_id, b_port, source, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetLink :one
SELECT * FROM links WHERE id = ?;

-- name: ListLinks :many
SELECT * FROM links ORDER BY id;

-- name: DeleteLink :execrows
DELETE FROM links WHERE id = ?;
