-- name: CreateUser :one
INSERT INTO users (email, password_hash, username, full_name, role, is_active)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, email, username, full_name, password_hash, is_active, role, last_login, created_at, updated_at;

-- name: GetUserByID :one
SELECT id, email, username, full_name, password_hash, is_active, role, last_login, created_at, updated_at
FROM users
WHERE id = $1
LIMIT 1;

-- name: GetUserByEmail :one
SELECT id, email, username, full_name, password_hash, is_active, role, last_login, created_at, updated_at
FROM users
WHERE email = $1
LIMIT 1;

-- name: GetUserByUsername :one
SELECT id, email, username, full_name, password_hash, is_active, role, last_login, created_at, updated_at
FROM users
WHERE username = $1
LIMIT 1;

-- name: UpdateUser :one
UPDATE users
SET username = $2,
    full_name = $3,
    email = $4,
    role = $5,
    is_active = $6,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING id, email, username, full_name, password_hash, is_active, role, last_login, created_at, updated_at;


-- name: DeleteUser :execrows
DELETE FROM users
WHERE id = $1;

-- name: ListUsers :many
SELECT id, email, username, full_name, is_active, role, last_login, created_at, updated_at
FROM users
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: ListUsersByID :many
SELECT id, email, username, full_name, is_active, role, last_login, created_at, updated_at
FROM users
ORDER BY id ASC
LIMIT $1 OFFSET $2;

-- name: GetActiveUsers :many
SELECT id, email, username, full_name, is_active, role, last_login, created_at, updated_at
FROM users
WHERE is_active = true
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: GetInactiveUsers :many
SELECT id, email, username, full_name, is_active, role, last_login, created_at, updated_at
FROM users
WHERE is_active = false
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: GetUsersByRole :many
SELECT id, email, username, full_name, is_active, role, last_login, created_at, updated_at
FROM users
WHERE role = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateUserLastLogin :exec
UPDATE users
SET last_login = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: UpdateUserIsActive :exec
UPDATE users
SET is_active = $2, updated_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: UpdateUserRole :one
UPDATE users
SET role = $2, updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING id, email, username, full_name, password_hash, is_active, role, last_login, created_at, updated_at;


-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: CountActiveUsers :one
SELECT COUNT(*) FROM users
WHERE is_active = true;

-- name: CountUsersByRole :one
SELECT COUNT(*) FROM users
WHERE role = $1;

-- name: GetUserByEmailAndPassword :one
SELECT id, email, username, full_name, is_active, role, last_login, created_at, updated_at
FROM users
WHERE email = $1 AND password_hash = $2
LIMIT 1;

-- name: SoftDeleteUser :exec
UPDATE users
SET is_active = false, updated_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: RestoreUser :exec
UPDATE users
SET is_active = true, updated_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: SearchUsers :many
SELECT id, email, username, full_name, is_active, role, last_login, created_at, updated_at
FROM users
WHERE username ILIKE '%' || $1 || '%'
   OR email ILIKE '%' || $1 || '%'
   OR full_name ILIKE '%' || $1 || '%'
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;