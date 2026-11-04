-- +goose Up
-- The IPv6 address of a device, one per device, in canonical form. A device is still
-- identified by its MAC, so the address is a second column and not a second identity.
ALTER TABLE devices ADD COLUMN ip6 TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX devices_ip6 ON devices(ip6) WHERE ip6 <> '';

-- A service remembers the address it answered on: empty for the IPv4 address of the device,
-- the IPv6 address for a port that answers only there. The unique key gains that column, and
-- SQLite cannot change a unique key in place, so the table is rebuilt.
CREATE TABLE services_new (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	device_id     INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	ip            TEXT NOT NULL DEFAULT '',
	port          INTEGER NOT NULL,
	proto         TEXT NOT NULL,                   -- tcp, udp
	name          TEXT NOT NULL DEFAULT '',
	banner        TEXT NOT NULL DEFAULT '',
	first_seen_at TEXT NOT NULL,
	last_seen_at  TEXT NOT NULL,
	UNIQUE (device_id, ip, port, proto)
);
INSERT INTO services_new (id, device_id, ip, port, proto, name, banner, first_seen_at, last_seen_at)
SELECT id, device_id, '', port, proto, name, banner, first_seen_at, last_seen_at FROM services;
DROP TABLE services;
ALTER TABLE services_new RENAME TO services;

-- +goose Down
CREATE TABLE services_old (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	device_id     INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	port          INTEGER NOT NULL,
	proto         TEXT NOT NULL,
	name          TEXT NOT NULL DEFAULT '',
	banner        TEXT NOT NULL DEFAULT '',
	first_seen_at TEXT NOT NULL,
	last_seen_at  TEXT NOT NULL,
	UNIQUE (device_id, port, proto)
);
INSERT OR IGNORE INTO services_old (id, device_id, port, proto, name, banner, first_seen_at, last_seen_at)
SELECT id, device_id, port, proto, name, banner, first_seen_at, last_seen_at FROM services ORDER BY ip, id;
DROP TABLE services;
ALTER TABLE services_old RENAME TO services;
DROP INDEX devices_ip6;
ALTER TABLE devices DROP COLUMN ip6;
