-- +goose Up
-- A person who signs in through an identity provider (slice 36). The key is the issuer and
-- the subject the provider gives, never the e-mail address: an address can change hands.
-- A user created this way has an empty password_hash and cannot sign in with a password.
CREATE TABLE user_identities (
	issuer     TEXT NOT NULL,
	subject    TEXT NOT NULL,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	email      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	PRIMARY KEY (issuer, subject)
);
CREATE INDEX user_identities_user ON user_identities(user_id);

-- +goose Down
DROP TABLE user_identities;
