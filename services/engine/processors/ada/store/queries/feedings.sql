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

-- name: GetTodayFeedingAggregates :one
SELECT
    COUNT(DISTINCT f.id)::int                                        AS count,
    COALESCE(SUM(d.amount_oz), 0)::float8                            AS total_oz,
    COALESCE(SUM(d.breast_milk_oz), 0)::float8                       AS breast_milk_oz,
    COALESCE(SUM(d.formula_oz), 0)::float8                           AS formula_oz
FROM feedings f
LEFT JOIN feeding_bottle_detail d ON d.feeding_id = f.id
WHERE f.deleted_at IS NULL
  AND f.timestamp >= NOW()::date;
