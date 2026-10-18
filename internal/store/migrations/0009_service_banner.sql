-- +goose Up
-- What a web service says about itself (the Server header and the like), cut to 200
-- characters and cleaned of control characters before it is stored.
ALTER TABLE services ADD COLUMN banner TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE services DROP COLUMN banner;
