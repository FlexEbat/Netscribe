-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: CountEnabledAdmins :one
SELECT count(*) FROM users WHERE role = 'admin' AND disabled = 0;

-- name: InsertUser :one
INSERT INTO users (username, display_name, password_hash, role, disabled, must_change_password, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetUserByName :one
SELECT * FROM users WHERE username = ?;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: ListUsers :many
SELECT * FROM users ORDER BY username;

-- name: SetUserRoleAndDisabled :exec
UPDATE users SET role = ?, disabled = ? WHERE id = ?;

-- name: SetUserPassword :execrows
UPDATE users
SET password_hash = ?, must_change_password = ?, failed_logins = 0, locked_until = NULL
WHERE id = ?;

-- name: RecordLoginSuccess :exec
UPDATE users SET failed_logins = 0, locked_until = NULL, last_login_at = ? WHERE id = ?;

-- name: RecordLoginFailure :exec
UPDATE users SET failed_logins = ?, locked_until = ? WHERE id = ?;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = ?;
