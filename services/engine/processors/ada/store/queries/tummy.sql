-- name: InsertTummySession :exec
INSERT INTO tummy_time_sessions (start_time, end_time, duration_s, logged_by)
VALUES (@start_time, @end_time, @duration_s, @logged_by);

-- name: GetTodayTummyAggregates :one
SELECT
    COALESCE(SUM(duration_s) / 60, 0)::int AS total_minutes,
    COUNT(*)::int                           AS sessions
FROM tummy_time_sessions
WHERE deleted_at IS NULL
  AND start_time >= NOW()::date;
