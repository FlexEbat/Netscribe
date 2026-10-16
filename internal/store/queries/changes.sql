-- name: InsertChange :exec
INSERT INTO changes (scan_id, kind, entity_id, summary, at) VALUES (?, ?, ?, ?, ?);

-- name: ListChanges :many
SELECT * FROM changes ORDER BY id DESC LIMIT ?;

-- name: ListChangesByScan :many
SELECT * FROM changes WHERE scan_id = ? ORDER BY id;

-- name: CountDoneScansExcept :one
SELECT COUNT(*) FROM scans WHERE status = 'done' AND id <> ?;

-- name: LatestDoneScan :one
SELECT * FROM scans WHERE status = 'done' ORDER BY id DESC LIMIT 1;

-- name: PruneScans :execrows
DELETE FROM scans
WHERE status <> 'running' AND id NOT IN (SELECT id FROM scans ORDER BY id DESC LIMIT ?);
