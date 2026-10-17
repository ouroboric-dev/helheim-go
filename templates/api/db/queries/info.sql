-- name: GetInfo :one
SELECT value FROM app_info WHERE key = $1;

-- name: SetInfo :exec
INSERT INTO app_info (key, value) VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
