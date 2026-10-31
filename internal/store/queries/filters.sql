-- name: InsertFilter :one
INSERT INTO saved_filters (user_id, page, name, query, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListFilters :many
SELECT * FROM saved_filters WHERE user_id = ? AND page = ? ORDER BY id DESC;

-- name: CountFilters :one
SELECT COUNT(*) FROM saved_filters WHERE user_id = ? AND page = ?;

-- name: DeleteFilter :execrows
DELETE FROM saved_filters WHERE id = ? AND user_id = ?;
