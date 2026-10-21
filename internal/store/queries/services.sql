-- name: GetServiceByKey :one
SELECT * FROM services WHERE device_id = ? AND port = ? AND proto = ?;

-- name: InsertService :exec
INSERT INTO services (device_id, port, proto, name, banner, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: TouchService :exec
UPDATE services SET name = ?, banner = ?, last_seen_at = ? WHERE id = ?;

-- name: DeleteDeviceServicesBefore :exec
DELETE FROM services WHERE device_id = ? AND last_seen_at < ?;

-- name: ListServices :many
SELECT * FROM services ORDER BY device_id, port, proto;

-- name: ListServicesByDevice :many
SELECT * FROM services WHERE device_id = ? ORDER BY port, proto;
