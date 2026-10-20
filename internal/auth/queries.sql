-- name: hasOwner :one
SELECT EXISTS(SELECT 1 FROM users);

-- name: createOwner :exec
INSERT INTO users (email, password_hash) VALUES (?, ?);

-- name: findUser :one
SELECT id, password_hash FROM users WHERE email = ?;

-- name: createSession :exec
INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?);

-- name: sessionExists :one
SELECT EXISTS(SELECT 1 FROM sessions WHERE token_hash = ? AND expires_at > ?);

-- name: deleteSession :exec
DELETE FROM sessions WHERE token_hash = ?;
