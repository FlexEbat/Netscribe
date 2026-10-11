-- +goose Up
-- Columns for input by people: manual devices, their label and notes, a kind that scans
-- may not change, and a version for optimistic locking.
ALTER TABLE devices ADD COLUMN manual      INTEGER NOT NULL DEFAULT 0;        -- 1: created by hand, scans neither delete nor mark offline
ALTER TABLE devices ADD COLUMN label       TEXT NOT NULL DEFAULT '';          -- a name from a person, shown instead of the hostname
ALTER TABLE devices ADD COLUMN notes       TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN kind_locked INTEGER NOT NULL DEFAULT 0;        -- 1: the kind was set by a person
ALTER TABLE devices ADD COLUMN version     INTEGER NOT NULL DEFAULT 1;        -- grows with every edit by a person
ALTER TABLE devices ADD COLUMN updated_by  INTEGER REFERENCES users(id) ON DELETE SET NULL;

-- Links are created here, not with the slice that discovers them, because people draw
-- links by hand first.
CREATE TABLE links (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	a_device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	a_port       TEXT NOT NULL DEFAULT '',
	b_device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	b_port       TEXT NOT NULL DEFAULT '',
	source       TEXT NOT NULL,                    -- lldp, fdb, manual
	last_seen_at TEXT NOT NULL,
	CHECK (a_device_id < b_device_id),
	UNIQUE (a_device_id, a_port, b_device_id, b_port)
);

-- +goose Down
DROP TABLE links;
