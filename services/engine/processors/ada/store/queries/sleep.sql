-- name: InsertSleepStart :one
INSERT INTO sleep_sessions (start_time, sleep_type, logged_by)
VALUES (@start_time, @sleep_type, @logged_by)
RETURNING id;

-- name: UpdateSleepEnd :exec
UPDATE sleep_sessions
SET end_time = @end_time
WHERE id = (
    SELECT id FROM sleep_sessions
    WHERE end_time IS NULL AND deleted_at IS NULL
    ORDER BY start_time DESC LIMIT 1
);

-- name: InsertSleepSession :exec
INSERT INTO sleep_sessions (start_time, end_time, sleep_type, logged_by)
VALUES (@start_time, @end_time, @sleep_type, @logged_by);

-- name: GetActiveSleepSession :one
SELECT start_time FROM sleep_sessions
WHERE end_time IS NULL AND deleted_at IS NULL
ORDER BY start_time DESC LIMIT 1;

-- name: GetLastSleepEnd :one
SELECT end_time FROM sleep_sessions
WHERE end_time IS NOT NULL AND deleted_at IS NULL
ORDER BY end_time DESC LIMIT 1;

-- name: GetTodaySleepAggregates :one
SELECT
    COALESCE(EXTRACT(EPOCH FROM SUM(end_time - start_time)) / 3600, 0)::float8 AS total_hours,
    COUNT(*) FILTER (WHERE sleep_type = 'nap')::int                             AS nap_count
FROM sleep_sessions
WHERE deleted_at IS NULL
  AND end_time IS NOT NULL
  AND start_time >= NOW()::date;

-- name: GetLast24hSleepSessions :many
-- Returns sleep sessions that started in the last 24 hours, newest-first.
-- end_time is zero-value (Valid=false) for active sessions. duration_s is
-- computed in Go from start_time and end_time to avoid a CASE expression
-- that sqlc cannot type statically.
SELECT id, start_time, end_time, sleep_type
FROM sleep_sessions
WHERE deleted_at IS NULL
  AND start_time >= NOW() - INTERVAL '24 hours'
ORDER BY start_time DESC;
