-- name: InsertTummySession :exec
INSERT INTO tummy_time_sessions (start_time, end_time, duration_s, logged_by, test)
VALUES (@start_time, @end_time, @duration_s, @logged_by, @test);

-- name: GetTodayTummyAggregates :one
SELECT
    COALESCE(SUM(duration_s) / 60, 0)::int AS total_minutes,
    COUNT(*)::int                           AS sessions
FROM tummy_time_sessions
WHERE deleted_at IS NULL
  AND start_time >= @boundary;

-- name: GetLast24hTummy :many
-- Returns tummy time sessions since @since (a rolling-24h boundary), newest-first,
-- matching the shape of the other *_history sensors.
SELECT id, start_time, end_time, duration_s, logged_by
FROM tummy_time_sessions
WHERE deleted_at IS NULL
  AND start_time >= @since
ORDER BY start_time DESC;

-- name: UpdateTummySession :exec
UPDATE tummy_time_sessions
SET start_time = @start_time, end_time = @end_time, duration_s = @duration_s, logged_by = @logged_by
WHERE id = @id AND deleted_at IS NULL;

-- name: SoftDeleteTummySession :exec
UPDATE tummy_time_sessions SET deleted_at = NOW() WHERE id = @id AND deleted_at IS NULL;

-- name: DeleteAllTummy :exec
DELETE FROM tummy_time_sessions;
