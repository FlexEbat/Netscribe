-- name: GetContainer :one
SELECT * FROM containers WHERE id = ?;

-- name: InsertContainer :exec
INSERT INTO containers (id, device_id, name, image, state, compose_project, ports, networks, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateContainer :exec
UPDATE containers
SET device_id = ?, name = ?, image = ?, state = ?, compose_project = ?, ports = ?, networks = ?, last_seen_at = ?
WHERE id = ?;

-- name: DeleteContainer :execrows
DELETE FROM containers WHERE id = ?;

-- name: DeleteDeviceContainersBefore :execrows
DELETE FROM containers WHERE device_id = ? AND last_seen_at < ?;

-- name: ListContainers :many
SELECT * FROM containers ORDER BY device_id, name;

-- name: CountContainers :one
SELECT COUNT(*) FROM containers;
