-- +goose Up
-- One row for every scanned device in every completed scan. It answers "how often is the
-- device there" and "which addresses did it have". The rows go with their scan, so
-- history.keepScans is the limit. Nothing is backfilled: the history starts at the upgrade.
CREATE TABLE device_presence (
	scan_id   INTEGER NOT NULL REFERENCES scans(id) ON DELETE CASCADE,
	device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	online    INTEGER NOT NULL,
	ip        TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (scan_id, device_id)
) WITHOUT ROWID;

CREATE INDEX device_presence_device ON device_presence(device_id);

-- +goose Down
DROP TABLE device_presence;
