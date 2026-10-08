-- name: GetDeviceByMAC :one
SELECT * FROM devices WHERE mac = ?;

-- name: GetDeviceByIPWithoutMAC :one
SELECT * FROM devices WHERE mac = '' AND ip = ?;

-- name: GetDeviceByIP :one
SELECT * FROM devices WHERE ip = ? ORDER BY online DESC, last_seen_at DESC, id DESC LIMIT 1;

-- name: InsertDevice :one
INSERT INTO devices (mac, ip, hostname, vendor, kind, description, source, online, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
RETURNING id;

-- name: UpdateDeviceFromScan :exec
UPDATE devices
SET mac = ?, ip = ?, hostname = ?, vendor = ?, kind = ?, description = ?, online = 1, last_seen_at = ?
WHERE id = ?;

-- name: ListDevices :many
SELECT * FROM devices;

-- name: ListOnlineDeviceAddrs :many
SELECT id, ip FROM devices WHERE online = 1;

-- name: SetDeviceOffline :exec
UPDATE devices SET online = 0 WHERE id = ?;
