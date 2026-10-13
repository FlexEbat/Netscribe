-- +goose Up
CREATE TABLE api_tokens (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name         TEXT NOT NULL,
	prefix       TEXT NOT NULL,                    -- the first 8 characters, for the list
	token_hash   TEXT NOT NULL UNIQUE,             -- SHA-256 of the token
	scope        TEXT NOT NULL,                    -- read, write; never above the owner's role
	created_at   TEXT NOT NULL,
	expires_at   TEXT NOT NULL,
	last_used_at TEXT,
	revoked      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX api_tokens_user ON api_tokens(user_id);

-- +goose Down
DROP TABLE api_tokens;
