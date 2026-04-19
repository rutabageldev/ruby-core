-- name: InsertFeeding :one
INSERT INTO feedings (timestamp, source, logged_by)
VALUES (@timestamp, @source, @logged_by)
RETURNING id;

-- name: InsertFeedingBottleDetail :exec
INSERT INTO feeding_bottle_detail (feeding_id, amount_oz, breast_milk_oz, formula_oz, duration_s)
VALUES (@feeding_id, @amount_oz, @breast_milk_oz, @formula_oz, @duration_s);

-- name: InsertFeedingSegment :exec
INSERT INTO feeding_segments (feeding_id, side, started_at, ended_at, duration_s)
VALUES (@feeding_id, @side, @started_at, @ended_at, @duration_s);

-- name: GetLastFeeding :one
SELECT
    f.timestamp,
    f.source,
    EXISTS(SELECT 1 FROM feeding_bottle_detail d WHERE d.feeding_id = f.id) AS has_bottle_detail
FROM feedings f
WHERE f.deleted_at IS NULL
ORDER BY f.timestamp DESC LIMIT 1;

-- name: GetLastFeedingID :one
SELECT id FROM feedings
WHERE deleted_at IS NULL
ORDER BY timestamp DESC LIMIT 1;

-- name: GetLast24hFeedings :many
-- Returns all feedings since @boundary with per-side breast durations
-- and bottle amounts. Left/right duration_s are 0 for non-breast sessions.
-- COALESCE handles NULL oz columns from LEFT JOIN on non-bottle feedings.
SELECT
    f.id,
    f.timestamp,
    f.source,
    COALESCE(d.amount_oz, 0)::float8       AS amount_oz,
    COALESCE(d.breast_milk_oz, 0)::float8  AS breast_milk_oz,
    COALESCE(d.formula_oz, 0)::float8      AS formula_oz,
    COALESCE(
        SUM(CASE WHEN fs.side = 'left'  THEN fs.duration_s END), 0
    )::int AS left_duration_s,
    COALESCE(
        SUM(CASE WHEN fs.side = 'right' THEN fs.duration_s END), 0
    )::int AS right_duration_s
FROM feedings f
LEFT JOIN feeding_bottle_detail d ON d.feeding_id = f.id
LEFT JOIN feeding_segments fs     ON fs.feeding_id = f.id
WHERE f.deleted_at IS NULL
  AND f.timestamp >= @boundary
GROUP BY
    f.id, f.timestamp, f.source,
    d.amount_oz, d.breast_milk_oz, d.formula_oz
ORDER BY f.timestamp DESC;

-- name: GetTodayFeedingAggregates :one
SELECT
    COUNT(DISTINCT f.id)::int                                        AS count,
    COALESCE(SUM(d.amount_oz), 0)::float8                            AS total_oz,
    COALESCE(SUM(d.breast_milk_oz), 0)::float8                       AS breast_milk_oz,
    COALESCE(SUM(d.formula_oz), 0)::float8                           AS formula_oz
FROM feedings f
LEFT JOIN feeding_bottle_detail d ON d.feeding_id = f.id
WHERE f.deleted_at IS NULL
  AND f.timestamp >= @boundary;
