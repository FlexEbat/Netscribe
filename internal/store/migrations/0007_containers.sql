-- +goose Up
-- Docker containers of a host, any state. The collector keeps the table equal to what the
-- Docker daemon lists: a container that is gone from the daemon is gone from here.
CREATE TABLE containers (
	id              TEXT PRIMARY KEY,              -- full Docker id
	device_id       INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	name            TEXT NOT NULL,
	image           TEXT NOT NULL,
	state           TEXT NOT NULL,                 -- running, exited, paused, restarting, created, dead
	compose_project TEXT NOT NULL DEFAULT '',
	ports           TEXT NOT NULL DEFAULT '[]',    -- JSON, array of PortMapping
	networks        TEXT NOT NULL DEFAULT '[]',    -- JSON, array of ContainerNetwork
	first_seen_at   TEXT NOT NULL,
	last_seen_at    TEXT NOT NULL
);

CREATE INDEX containers_device_id ON containers (device_id);

-- +goose Down
DROP TABLE containers;
