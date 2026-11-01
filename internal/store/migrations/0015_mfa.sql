-- +goose Up
-- The second factor of an account. The secret is sealed with a key that is not in the
-- database, so a copy of the database alone does not hold it. A row with no confirmed_at
-- is a setup that was begun and not finished: it does not count.
CREATE TABLE user_totp (
	user_id       INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	secret_sealed BLOB NOT NULL,
	created_at    TEXT NOT NULL,
	confirmed_at  TEXT,
	last_step     INTEGER NOT NULL DEFAULT 0
);

-- One use each. The code is stored as a hash, never in clear.
CREATE TABLE recovery_codes (
	id      INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	hash    TEXT NOT NULL,
	used_at TEXT
);
CREATE INDEX recovery_codes_user ON recovery_codes(user_id);

-- +goose Down
DROP TABLE recovery_codes;
DROP TABLE user_totp;
