-- +goose Up
-- Open TCP ports of a device. A port that was not seen in a finished scan of the device
-- is deleted by the scan, so a row always means "answered last time".
CREATE TABLE services (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	device_id     INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	port          INTEGER NOT NULL,
	proto         TEXT NOT NULL,                   -- tcp, udp
	name          TEXT NOT NULL DEFAULT '',
	first_seen_at TEXT NOT NULL,
	last_seen_at  TEXT NOT NULL,
	UNIQUE (device_id, port, proto)
);

-- +goose Down
DROP TABLE services;
