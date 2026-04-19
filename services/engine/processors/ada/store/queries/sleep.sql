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
    COALESCE(EXTRACT(EPOCH FROM SUM(
        LEAST(COALESCE(end_time, NOW()), NOW()) - GREATEST(start_time, @boundary)
    )) / 3600, 0)::float8 AS total_hours,
    COUNT(*) FILTER (WHERE sleep_type = 'night')::int AS night_count,
    COUNT(*) FILTER (WHERE sleep_type = 'nap')::int   AS nap_count,
    COALESCE(EXTRACT(EPOCH FROM SUM(
        CASE WHEN sleep_type = 'night'
        THEN LEAST(COALESCE(end_time, NOW()), NOW()) - GREATEST(start_time, @boundary)
        END
    )) / 3600, 0)::float8 AS night_hours,
    COALESCE(EXTRACT(EPOCH FROM SUM(
        CASE WHEN sleep_type = 'nap'
        THEN LEAST(COALESCE(end_time, NOW()), NOW()) - GREATEST(start_time, @boundary)
        END
    )) / 3600, 0)::float8 AS nap_hours
FROM sleep_sessions
WHERE deleted_at IS NULL
  AND (end_time IS NULL OR end_time > @boundary);

-- name: GetTodaySleepSessions :many
-- Returns sleep sessions active since the bedtime boundary, newest-first.
-- COALESCE ensures end_time is always non-null; is_complete distinguishes
-- active sessions (end_time IS NULL) from completed ones.
SELECT
    id,
    start_time,
    COALESCE(end_time, NOW()) AS end_time,
    sleep_type,
    logged_by,
    (end_time IS NOT NULL)::bool AS is_complete
FROM sleep_sessions
WHERE deleted_at IS NULL
  AND (end_time IS NULL OR end_time > @boundary)
ORDER BY start_time DESC;
