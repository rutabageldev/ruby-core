-- name: InsertDiaper :exec
INSERT INTO diapers (timestamp, type, logged_by)
VALUES (@timestamp, @type, @logged_by);

-- name: GetLastDiaper :one
SELECT timestamp, type FROM diapers
WHERE deleted_at IS NULL
ORDER BY timestamp DESC LIMIT 1;

-- name: GetTodayDiaperAggregates :one
SELECT
    COUNT(*)::int                                        AS total,
    COUNT(*) FILTER (WHERE type = 'wet')::int            AS wet,
    COUNT(*) FILTER (WHERE type = 'dirty')::int          AS dirty,
    COUNT(*) FILTER (WHERE type = 'mixed')::int           AS mixed
FROM diapers
WHERE deleted_at IS NULL
  AND timestamp >= NOW()::date;
