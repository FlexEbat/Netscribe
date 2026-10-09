-- +goose Up
CREATE TABLE users (
	id                   INTEGER PRIMARY KEY AUTOINCREMENT,
	username             TEXT NOT NULL UNIQUE COLLATE NOCASE,
	display_name         TEXT NOT NULL DEFAULT '',
	password_hash        TEXT NOT NULL,            -- argon2id, PHC format
	role                 TEXT NOT NULL,            -- viewer, operator, admin
	disabled             INTEGER NOT NULL DEFAULT 0,
	must_change_password INTEGER NOT NULL DEFAULT 0,
	failed_logins        INTEGER NOT NULL DEFAULT 0,
	locked_until         TEXT,
	created_at           TEXT NOT NULL,
	last_login_at        TEXT
);

CREATE TABLE sessions (
	id_hash      TEXT PRIMARY KEY,                 -- SHA-256 of the cookie value, the value itself is never stored
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	csrf_token   TEXT NOT NULL,
	created_at   TEXT NOT NULL,
	last_seen_at TEXT NOT NULL,
	expires_at   TEXT NOT NULL,                    -- absolute lifetime
	ip           TEXT NOT NULL DEFAULT '',
	user_agent   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE audit_log (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	at        TEXT NOT NULL,
	user_id   INTEGER,                             -- NULL when the user is unknown
	username  TEXT NOT NULL DEFAULT '',
	action    TEXT NOT NULL,
	entity    TEXT NOT NULL DEFAULT '',
	entity_id TEXT NOT NULL DEFAULT '',
	result    TEXT NOT NULL,                       -- ok, denied, error
	ip        TEXT NOT NULL DEFAULT '',
	detail    TEXT NOT NULL DEFAULT ''             -- one line, no secrets
);
CREATE INDEX audit_log_at ON audit_log(at);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE sessions;
DROP TABLE users;
