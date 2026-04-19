-- name: UpsertCaretaker :exec
INSERT INTO caretakers (id, ha_user_id, display_name, username, notify_service, updated_at)
VALUES (gen_random_uuid(), @ha_user_id, @display_name, @username, @notify_service, NOW())
ON CONFLICT (ha_user_id) DO UPDATE
    SET display_name   = EXCLUDED.display_name,
        username       = EXCLUDED.username,
        notify_service = EXCLUDED.notify_service,
        deleted_at     = NULL,
        updated_at     = NOW();

-- name: SoftDeleteCaretaker :exec
UPDATE caretakers
SET deleted_at = NOW(), updated_at = NOW()
WHERE ha_user_id = @ha_user_id
  AND deleted_at IS NULL;

-- name: UpdateCaretakerStatus :exec
UPDATE caretakers
SET is_caretaker = @is_caretaker, updated_at = NOW()
WHERE ha_user_id = @ha_user_id
  AND deleted_at IS NULL;

-- name: GetActiveCaretakers :many
SELECT ha_user_id, display_name, notify_service
FROM caretakers
WHERE deleted_at IS NULL
  AND is_caretaker = true
  AND notify_service IS NOT NULL
ORDER BY display_name;

-- name: GetAllCaretakers :many
SELECT ha_user_id, display_name, username, is_caretaker, notify_service
FROM caretakers
WHERE deleted_at IS NULL
ORDER BY display_name;

-- name: GetCaretakerHAUserIDs :many
SELECT ha_user_id FROM caretakers
WHERE deleted_at IS NULL;
