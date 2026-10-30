-- +goose Up
-- A group names a part of the network: a site, a VLAN, a range. "groups" is an SQLite
-- keyword, so the table is called device_groups.
CREATE TABLE device_groups (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
	description TEXT NOT NULL DEFAULT '',
	vlan        INTEGER,
	ranges      TEXT NOT NULL DEFAULT '[]',
	version     INTEGER NOT NULL DEFAULT 1,
	created_at  TEXT NOT NULL
);

ALTER TABLE devices ADD COLUMN group_id INTEGER REFERENCES device_groups(id) ON DELETE SET NULL;
CREATE INDEX devices_group ON devices(group_id);

-- +goose Down
DROP INDEX devices_group;
ALTER TABLE devices DROP COLUMN group_id;
DROP TABLE device_groups;
