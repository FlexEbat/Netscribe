-- +goose Up
-- The operating system nmap guessed for a device, for example "Linux 5.3 - 5.4".
-- It stays until a later nmap run says something else.
ALTER TABLE devices ADD COLUMN os_guess TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE devices DROP COLUMN os_guess;
