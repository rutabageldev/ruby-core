-- name: UpsertPerson :exec
INSERT INTO people (id, ha_user_id, display_name, username, updated_at)
VALUES (gen_random_uuid(), @ha_user_id, @display_name, @username, NOW())
ON CONFLICT (ha_user_id) DO UPDATE
    SET display_name = EXCLUDED.display_name,
        username     = EXCLUDED.username,
        deleted_at   = NULL,
        updated_at   = NOW();

-- name: SoftDeletePerson :exec
UPDATE people
SET deleted_at = NOW(), updated_at = NOW()
WHERE ha_user_id = @ha_user_id
  AND deleted_at IS NULL;

-- name: UpdateCaretakerStatus :exec
UPDATE people
SET is_caretaker = @is_caretaker, updated_at = NOW()
WHERE ha_user_id = @ha_user_id
  AND deleted_at IS NULL;

-- name: GetAllPeople :many
SELECT id, ha_user_id, display_name, username, is_caretaker
FROM people
WHERE deleted_at IS NULL
ORDER BY display_name;

-- name: GetActivePeopleWithChannels :many
SELECT
    p.id,
    p.display_name,
    pc.type,
    pc.address,
    pc.label
FROM people p
JOIN person_channels pc ON pc.person_id = p.id
WHERE p.deleted_at IS NULL
  AND p.is_caretaker = true
  AND pc.is_active = true
ORDER BY p.display_name, pc.type;

-- name: GetPersonHAUserIDs :many
SELECT ha_user_id FROM people
WHERE deleted_at IS NULL;

-- name: GetPersonByHAUserID :one
SELECT id, ha_user_id, display_name, username, is_caretaker
FROM people
WHERE ha_user_id = @ha_user_id
  AND deleted_at IS NULL;

-- name: GetChannelsForPerson :many
SELECT id, type, address, label, is_active
FROM person_channels
WHERE person_id = @person_id
ORDER BY type, created_at;

-- name: AddPersonChannel :exec
INSERT INTO person_channels (id, person_id, type, address, label)
VALUES (gen_random_uuid(), @person_id, @type, @address, @label)
ON CONFLICT (person_id, address) DO UPDATE
    SET label     = EXCLUDED.label,
        is_active = true;

-- name: RemovePersonChannel :exec
DELETE FROM person_channels
WHERE id = @id
  AND person_id = @person_id;

-- name: GetLinkedHAPushAddresses :many
SELECT address FROM person_channels
WHERE type = 'ha_push';
