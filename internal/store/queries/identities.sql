-- name: GetUserByIdentity :one
SELECT u.* FROM users u
JOIN user_identities i ON i.user_id = u.id
WHERE i.issuer = ? AND i.subject = ?;

-- name: InsertIdentity :exec
INSERT INTO user_identities (issuer, subject, user_id, email, created_at)
VALUES (?, ?, ?, ?, ?);
