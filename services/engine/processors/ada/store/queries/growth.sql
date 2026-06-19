-- name: InsertGrowthMeasurement :one
INSERT INTO growth_measurements (
    measured_at, weight_oz, length_in, head_circumference_in,
    source, weight_pct, length_pct, head_pct, logged_by, test
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id;

-- name: GetLatestWeight :one
SELECT id, measured_at, weight_oz, weight_pct, source
FROM growth_measurements
WHERE weight_oz IS NOT NULL AND deleted_at IS NULL
ORDER BY measured_at DESC
LIMIT 1;

-- name: GetLatestLength :one
SELECT id, measured_at, length_in, length_pct, source
FROM growth_measurements
WHERE length_in IS NOT NULL AND deleted_at IS NULL
ORDER BY measured_at DESC
LIMIT 1;

-- name: GetLatestHeadCircumference :one
SELECT id, measured_at, head_circumference_in, head_pct, source
FROM growth_measurements
WHERE head_circumference_in IS NOT NULL AND deleted_at IS NULL
ORDER BY measured_at DESC
LIMIT 1;

-- name: GetAllGrowthMeasurements :many
SELECT id, measured_at, weight_oz, length_in, head_circumference_in,
       source, weight_pct, length_pct, head_pct, logged_by
FROM growth_measurements
WHERE deleted_at IS NULL
ORDER BY measured_at DESC;

-- name: UpdateGrowthMeasurement :exec
-- Full-resolution edit of a growth measurement by id (#78). Percentiles are
-- recomputed by the caller from the new value + age.
UPDATE growth_measurements
SET measured_at = $2, weight_oz = $3, length_in = $4, head_circumference_in = $5,
    source = $6, weight_pct = $7, length_pct = $8, head_pct = $9, logged_by = $10
WHERE id = $1 AND deleted_at IS NULL;

-- name: SoftDeleteGrowthMeasurement :exec
UPDATE growth_measurements SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL;
