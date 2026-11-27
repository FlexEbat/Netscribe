-- +goose Up
-- How many completed scans in a row did not see the device. A device goes offline when the
-- number reaches scan.offlineAfter, so one lost packet does not write an offline change
-- (slice 46). A scan that sees the device sets it back to 0.
ALTER TABLE devices ADD COLUMN missed_scans INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE devices DROP COLUMN missed_scans;
