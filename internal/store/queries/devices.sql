-- name: GetDeviceByID :one
SELECT * FROM devices WHERE id = ?;

-- name: GetDeviceByMAC :one
SELECT * FROM devices WHERE mac = ?;

-- name: GetDeviceByIPWithoutMAC :one
SELECT * FROM devices WHERE mac = '' AND ip = ?;

-- name: GetDeviceByIP :one
SELECT * FROM devices WHERE ip = ? ORDER BY online DESC, last_seen_at DESC, id DESC LIMIT 1;

-- name: InsertDevice :one
INSERT INTO devices (mac, ip, hostname, vendor, kind, description, os_guess, source, online, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
RETURNING id;

-- name: UpdateDeviceFromScan :exec
UPDATE devices
SET mac = ?, ip = ?, hostname = ?, vendor = ?, kind = ?, source = ?, description = ?, os_guess = ?, online = 1, last_seen_at = ?
WHERE id = ?;

-- name: ListDevices :many
SELECT * FROM devices;

-- name: ListOnlineDeviceAddrs :many
SELECT id, ip FROM devices WHERE online = 1 AND manual = 0;

-- name: SetDeviceOffline :exec
UPDATE devices SET online = 0 WHERE id = ?;

-- name: SetDevicePosition :execrows
UPDATE devices SET x = ?, y = ? WHERE id = ?;

-- name: InsertManualDevice :one
INSERT INTO devices (mac, ip, hostname, vendor, kind, description, source, online, manual, trusted, label, notes, kind_locked, updated_by, first_seen_at, last_seen_at, group_id)
VALUES (?, ?, ?, '', ?, '', 'manual', 1, 1, 1, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateDeviceEdit :execrows
UPDATE devices
SET label = ?, notes = ?, kind = ?, kind_locked = ?, trusted = ?, group_id = ?, version = version + 1, updated_by = ?
WHERE id = ? AND version = ?;

-- name: DeleteDevice :execrows
DELETE FROM devices WHERE id = ?;

-- name: ApplyDeviceIdentity :execrows
-- An OS guess replaces the old one when it is not empty. The kind changes only while it
-- is unknown and was not set by hand.
UPDATE devices SET
  os_guess = CASE WHEN sqlc.arg(os_guess) <> '' THEN sqlc.arg(os_guess) ELSE os_guess END,
  kind = CASE
    WHEN kind = 'unknown' AND kind_locked = 0 AND sqlc.arg(kind) <> '' AND sqlc.arg(kind) <> 'unknown'
    THEN sqlc.arg(kind) ELSE kind END
WHERE id = sqlc.arg(id);
