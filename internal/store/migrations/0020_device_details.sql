-- +goose Up
-- What a person knows about a device that a scan cannot find out (slice 49): who owns it,
-- where it stands, its serial number and the end of its warranty. A scan never writes them.
ALTER TABLE devices ADD COLUMN owner TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN location TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN serial TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN warranty_until TEXT NOT NULL DEFAULT '';

-- Tags a person put on a device, in lower case.
CREATE TABLE device_tags (
	device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	tag       TEXT NOT NULL,
	PRIMARY KEY (device_id, tag)
);
CREATE INDEX device_tags_tag ON device_tags(tag);

-- Fields of a person's own. seq keeps the order in which they were made.
CREATE TABLE device_fields (
	device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	name      TEXT NOT NULL,
	value     TEXT NOT NULL,
	seq       INTEGER NOT NULL,
	PRIMARY KEY (device_id, name)
);

-- +goose Down
DROP TABLE device_fields;
DROP TABLE device_tags;
ALTER TABLE devices DROP COLUMN warranty_until;
ALTER TABLE devices DROP COLUMN serial;
ALTER TABLE devices DROP COLUMN location;
ALTER TABLE devices DROP COLUMN owner;
