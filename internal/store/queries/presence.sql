-- name: InsertPresence :exec
INSERT INTO device_presence (scan_id, device_id, online, ip) VALUES (?, ?, ?, ?);

-- name: ListPresence :many
SELECT p.scan_id, s.started_at, p.online, p.ip
FROM device_presence p
JOIN scans s ON s.id = p.scan_id
WHERE p.device_id = ?
ORDER BY p.scan_id;
