-- +goose Up
-- Network interfaces read from a device over SNMP. One row per interface and address,
-- an interface without an address has an empty ip. The links table already exists
-- (migration 0003), because people draw links by hand before a scan finds any.
CREATE TABLE interfaces (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	name      TEXT NOT NULL,
	mac       TEXT NOT NULL DEFAULT '',            -- lower case aa:bb:cc:dd:ee:ff, empty when unknown
	ip        TEXT NOT NULL DEFAULT '',
	up        INTEGER NOT NULL DEFAULT 1,          -- 0 or 1
	UNIQUE (device_id, name, ip)
);

-- +goose Down
DROP TABLE interfaces;
