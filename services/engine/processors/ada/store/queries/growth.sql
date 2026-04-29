-- name: InsertGrowthMeasurement :one
INSERT INTO growth_measurements (
    measured_at, weight_oz, length_in, head_circumference_in,
    source, weight_pct, length_pct, head_pct, logged_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
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
