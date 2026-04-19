-- name: UpsertProfile :exec
-- Inserts the birth profile. ON CONFLICT (singleton) DO NOTHING makes this
-- idempotent — if ada.born fires more than once, the first write wins and
-- subsequent calls are silently ignored. birth_at is immutable once set.
INSERT INTO ada_profile (id, birth_at, singleton)
VALUES (gen_random_uuid(), @birth_at, true)
ON CONFLICT (singleton) DO NOTHING;

-- name: GetProfile :one
SELECT id, birth_at, created_at FROM ada_profile
LIMIT 1;
