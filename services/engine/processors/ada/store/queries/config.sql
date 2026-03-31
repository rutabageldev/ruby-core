-- name: GetConfig :one
SELECT value, updated_at FROM ada_config
WHERE key = @key;

-- name: UpsertConfig :exec
INSERT INTO ada_config (key, value, updated_at)
VALUES (@key, @value, NOW())
ON CONFLICT (key) DO UPDATE
    SET value      = EXCLUDED.value,
        updated_at = EXCLUDED.updated_at;
