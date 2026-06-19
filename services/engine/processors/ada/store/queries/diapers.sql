-- name: InsertDiaper :exec
INSERT INTO diapers (timestamp, type, logged_by, test)
VALUES (@timestamp, @type, @logged_by, @test);

-- name: GetLastDiaper :one
SELECT timestamp, type FROM diapers
WHERE deleted_at IS NULL
ORDER BY timestamp DESC LIMIT 1;

-- name: GetTodayDiaperAggregates :one
SELECT
    COUNT(*)::int                                        AS total,
    COUNT(*) FILTER (WHERE type = 'wet')::int            AS wet,
    COUNT(*) FILTER (WHERE type = 'dirty')::int          AS dirty,
    COUNT(*) FILTER (WHERE type = 'mixed')::int          AS mixed
FROM diapers
WHERE deleted_at IS NULL
  AND timestamp >= @boundary;

-- name: GetTodayDiapers :many
-- Returns all diaper events since the bedtime boundary, ordered newest-first.
SELECT id, timestamp, type, logged_by
FROM diapers
WHERE deleted_at IS NULL
  AND timestamp >= @boundary
ORDER BY timestamp DESC;

-- name: UpdateDiaper :exec
UPDATE diapers
SET timestamp = @timestamp, type = @type, logged_by = @logged_by
WHERE id = @id AND deleted_at IS NULL;

-- name: SoftDeleteDiaper :exec
UPDATE diapers SET deleted_at = NOW() WHERE id = @id AND deleted_at IS NULL;

-- name: DeleteAllDiapers :exec
DELETE FROM diapers;
