-- name: ListAllDeviceTags :many
SELECT device_id, tag FROM device_tags ORDER BY device_id, tag;

-- name: ListDeviceTags :many
SELECT tag FROM device_tags WHERE device_id = ? ORDER BY tag;

-- name: CountDeviceTags :one
SELECT COUNT(*) FROM device_tags WHERE device_id = ?;

-- name: DeleteDeviceTags :exec
DELETE FROM device_tags WHERE device_id = ?;

-- name: DeleteDeviceTag :exec
DELETE FROM device_tags WHERE device_id = ? AND tag = ?;

-- name: InsertDeviceTag :exec
INSERT OR IGNORE INTO device_tags (device_id, tag) VALUES (?, ?);

-- name: ListAllDeviceFields :many
SELECT device_id, name, value FROM device_fields ORDER BY device_id, seq;

-- name: ListDeviceFields :many
SELECT name, value FROM device_fields WHERE device_id = ? ORDER BY seq;

-- name: DeleteDeviceFields :exec
DELETE FROM device_fields WHERE device_id = ?;

-- name: InsertDeviceField :exec
INSERT INTO device_fields (device_id, name, value, seq) VALUES (?, ?, ?, ?);
