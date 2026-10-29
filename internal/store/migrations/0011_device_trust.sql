-- +goose Up
-- A device is trusted when a person marked it as known. Everything that exists when this
-- runs counts as known, so an upgrade does not turn the whole network into "new" devices.
ALTER TABLE devices ADD COLUMN trusted INTEGER NOT NULL DEFAULT 0;
UPDATE devices SET trusted = 1;

-- +goose Down
ALTER TABLE devices DROP COLUMN trusted;
