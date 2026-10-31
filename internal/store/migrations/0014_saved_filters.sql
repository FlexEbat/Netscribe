-- +goose Up
-- The views a person keeps for the inventory, the services and the containers pages.
CREATE TABLE saved_filters (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	page       TEXT NOT NULL,
	name       TEXT NOT NULL COLLATE NOCASE,
	query      TEXT NOT NULL,
	created_at TEXT NOT NULL,
	UNIQUE (user_id, page, name)
);

-- +goose Down
DROP TABLE saved_filters;
