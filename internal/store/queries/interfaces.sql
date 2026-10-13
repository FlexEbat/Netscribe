-- name: InsertInterface :exec
INSERT INTO interfaces (device_id, name, mac, ip, up)
VALUES (?, ?, ?, ?, ?);

-- name: ListInterfacesByDevice :many
SELECT * FROM interfaces WHERE device_id = ? ORDER BY name, ip, id;

-- name: DeleteDeviceInterfaces :exec
DELETE FROM interfaces WHERE device_id = ?;
