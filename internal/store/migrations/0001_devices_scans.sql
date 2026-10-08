-- +goose Up
-- Columns for manual input (manual, label, notes, kind_locked, version, updated_by)
-- arrive with the slice that needs them.
CREATE TABLE devices (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	mac           TEXT NOT NULL DEFAULT '',        -- lower case aa:bb:cc:dd:ee:ff, empty when unknown
	ip            TEXT NOT NULL DEFAULT '',
	hostname      TEXT NOT NULL DEFAULT '',
	vendor        TEXT NOT NULL DEFAULT '',
	kind          TEXT NOT NULL DEFAULT 'unknown', -- router, switch, ap, firewall, server, nas, printer, camera, iot, host, unknown
	description   TEXT NOT NULL DEFAULT '',        -- sysDescr or uname
	source        TEXT NOT NULL,                   -- arp, snmp, ssh, local, manual
	online        INTEGER NOT NULL DEFAULT 1,      -- 0 or 1
	first_seen_at TEXT NOT NULL,
	last_seen_at  TEXT NOT NULL,
	x             REAL,                            -- canvas position, NULL until moved
	y             REAL
);
CREATE UNIQUE INDEX devices_mac ON devices(mac) WHERE mac <> '';
CREATE UNIQUE INDEX devices_ip_nomac ON devices(ip) WHERE mac = '' AND ip <> '';

CREATE TABLE scans (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	target          TEXT NOT NULL,                 -- targets, comma separated
	status          TEXT NOT NULL,                 -- running, done, failed
	started_at      TEXT NOT NULL,
	finished_at     TEXT,
	error           TEXT NOT NULL DEFAULT '',
	device_count    INTEGER NOT NULL DEFAULT 0,
	link_count      INTEGER NOT NULL DEFAULT 0,
	container_count INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE scans;
DROP TABLE devices;
