-- +goose Up
-- What a scan changed in the network, one row per change. A scan that is pruned takes
-- its changes with it.
CREATE TABLE changes (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	scan_id   INTEGER NOT NULL REFERENCES scans(id) ON DELETE CASCADE,
	kind      TEXT NOT NULL,   -- device_added, device_online, device_offline, device_changed,
	                           -- link_added, link_removed, container_added, container_removed
	entity_id TEXT NOT NULL,
	summary   TEXT NOT NULL,
	at        TEXT NOT NULL
);

CREATE INDEX changes_scan_id ON changes (scan_id);

-- +goose Down
DROP TABLE changes;
