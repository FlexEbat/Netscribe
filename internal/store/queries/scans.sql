-- name: InsertScan :one
INSERT INTO scans (target, status, started_at) VALUES (?, 'running', ?) RETURNING *;

-- name: GetScan :one
SELECT * FROM scans WHERE id = ?;

-- name: FinishScan :execrows
UPDATE scans SET status = ?, finished_at = ?, error = ? WHERE id = ? AND status = 'running';

-- name: SetScanCounts :exec
UPDATE scans SET device_count = ?, link_count = ?, container_count = ? WHERE id = ?;

-- name: ListScans :many
SELECT * FROM scans ORDER BY id DESC LIMIT ?;
