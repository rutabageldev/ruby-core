-- Emergency card rows (ROADMAP-0011 effort 0011.4). Standing config — eventTest
-- only (not pre-birth-forced), so real contacts entered before birth survive the
-- clean-slate. ids are dashboard-provided strings (ec- prefixed). Live-field rows
-- carry only field_key; their value resolves client-side off existing sensors.

-- name: UpsertEmergencyRow :exec
-- A new row appends at the end; an edit keeps the row's existing position. The
-- explicit order is set by ada.emergency.reorder.
INSERT INTO emergency_rows (id, sort_order, type, label, name, phone, address, field_key, logged_by, test)
VALUES (@id,
        COALESCE((SELECT MAX(sort_order) + 1 FROM emergency_rows WHERE deleted_at IS NULL), 0),
        @type, @label, @name, @phone, @address, @field_key, @logged_by, @test)
ON CONFLICT (id) DO UPDATE
    SET type       = EXCLUDED.type,
        label      = EXCLUDED.label,
        name       = EXCLUDED.name,
        phone      = EXCLUDED.phone,
        address    = EXCLUDED.address,
        field_key  = EXCLUDED.field_key,
        logged_by  = EXCLUDED.logged_by,
        deleted_at = NULL;

-- name: SoftDeleteEmergencyRow :exec
UPDATE emergency_rows SET deleted_at = NOW() WHERE id = @id AND deleted_at IS NULL;

-- name: ReorderEmergencyRows :exec
-- Set sort_order to each id's position in the supplied ordered list.
UPDATE emergency_rows AS e
SET sort_order = v.ord - 1
FROM unnest(@ids::text[]) WITH ORDINALITY AS v(id, ord)
WHERE e.id = v.id AND e.deleted_at IS NULL;

-- name: ListEmergencyRows :many
SELECT id, sort_order, type, label, name, phone, address, field_key
FROM emergency_rows
WHERE deleted_at IS NULL
ORDER BY sort_order, created_at;
