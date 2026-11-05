-- +goose Up
-- Why the classifier chose the kind of a device: a score from 0 to 100 and at most five short
-- phrases as a JSON array. For an unknown kind the phrases name the candidates.
ALTER TABLE devices ADD COLUMN kind_score INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN kind_reasons TEXT NOT NULL DEFAULT '[]';

-- The certificate a TLS service presented, read during a handshake and never validated.
-- Empty strings mean no certificate was read.
ALTER TABLE services ADD COLUMN tls_subject TEXT NOT NULL DEFAULT '';
ALTER TABLE services ADD COLUMN tls_sans TEXT NOT NULL DEFAULT '[]';
ALTER TABLE services ADD COLUMN tls_issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE services ADD COLUMN tls_not_before TEXT NOT NULL DEFAULT '';
ALTER TABLE services ADD COLUMN tls_not_after TEXT NOT NULL DEFAULT '';
ALTER TABLE services ADD COLUMN tls_fingerprint TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE services DROP COLUMN tls_fingerprint;
ALTER TABLE services DROP COLUMN tls_not_after;
ALTER TABLE services DROP COLUMN tls_not_before;
ALTER TABLE services DROP COLUMN tls_issuer;
ALTER TABLE services DROP COLUMN tls_sans;
ALTER TABLE services DROP COLUMN tls_subject;
ALTER TABLE devices DROP COLUMN kind_reasons;
ALTER TABLE devices DROP COLUMN kind_score;
