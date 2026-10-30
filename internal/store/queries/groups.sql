-- name: InsertGroup :one
INSERT INTO device_groups (name, description, vlan, ranges, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListGroups :many
SELECT * FROM device_groups ORDER BY id;

-- name: GetGroup :one
SELECT * FROM device_groups WHERE id = ?;

-- name: UpdateGroup :execrows
UPDATE device_groups
SET name = ?, description = ?, vlan = ?, ranges = ?, version = version + 1
WHERE id = ? AND version = ?;

-- name: DeleteGroup :execrows
DELETE FROM device_groups WHERE id = ?;

-- name: SetDeviceGroup :execrows
UPDATE devices SET group_id = ?, version = version + 1 WHERE id = ?;
