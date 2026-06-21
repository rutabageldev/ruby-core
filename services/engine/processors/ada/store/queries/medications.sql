-- Medication registry + dosing routines (ROADMAP-0011 effort 0011.1).
-- ids are client-generated (dashboard), so writes are upserts by id; a re-upsert
-- revives a soft-deleted row (deleted_at = NULL). The test flag is set on insert
-- and left untouched on update, so an edit never reclassifies a real row as test.

-- name: UpsertMedication :exec
INSERT INTO medications (id, name, route, measure_unit, min_interval_hours, max_per_24h, active, logged_by, test)
VALUES (@id, @name, @route, @measure_unit, @min_interval_hours, @max_per_24h, @active, @logged_by, @test)
ON CONFLICT (id) DO UPDATE
    SET name               = EXCLUDED.name,
        route              = EXCLUDED.route,
        measure_unit       = EXCLUDED.measure_unit,
        min_interval_hours = EXCLUDED.min_interval_hours,
        max_per_24h        = EXCLUDED.max_per_24h,
        active             = EXCLUDED.active,
        logged_by          = EXCLUDED.logged_by,
        deleted_at         = NULL;

-- name: ListMedications :many
SELECT id, name, route, measure_unit, min_interval_hours, max_per_24h, active, logged_by, test
FROM medications
WHERE deleted_at IS NULL
ORDER BY name;

-- name: SoftDeleteMedication :exec
UPDATE medications SET deleted_at = NOW() WHERE id = @id AND deleted_at IS NULL;

-- name: SoftDeleteRoutinesForMedication :exec
UPDATE medication_routines SET deleted_at = NOW() WHERE medication_id = @medication_id AND deleted_at IS NULL;

-- name: SoftDeleteSeriesForMedication :exec
UPDATE medication_temp_series SET deleted_at = NOW() WHERE medication_id = @medication_id AND deleted_at IS NULL;

-- name: UpsertMedicationRoutine :exec
INSERT INTO medication_routines (id, medication_id, dose_amount, schedule_type, fixed_times, interval_hours, end_type, end_value, status, logged_by, test)
VALUES (@id, @medication_id, @dose_amount, @schedule_type, @fixed_times, @interval_hours, @end_type, @end_value, @status, @logged_by, @test)
ON CONFLICT (id) DO UPDATE
    SET medication_id  = EXCLUDED.medication_id,
        dose_amount    = EXCLUDED.dose_amount,
        schedule_type  = EXCLUDED.schedule_type,
        fixed_times    = EXCLUDED.fixed_times,
        interval_hours = EXCLUDED.interval_hours,
        end_type       = EXCLUDED.end_type,
        end_value      = EXCLUDED.end_value,
        status         = EXCLUDED.status,
        logged_by      = EXCLUDED.logged_by,
        deleted_at     = NULL;

-- name: ListMedicationRoutines :many
SELECT id, medication_id, dose_amount, schedule_type, fixed_times, interval_hours, end_type, end_value, status, logged_by, test
FROM medication_routines
WHERE deleted_at IS NULL
ORDER BY created_at;

-- name: SoftDeleteRoutine :exec
UPDATE medication_routines SET deleted_at = NOW() WHERE id = @id AND deleted_at IS NULL;

-- ── Dose events + temporary series (ROADMAP-0011 effort 0011.2) ────────────────

-- name: InsertMedicationEvent :exec
INSERT INTO medication_events (id, medication_id, status, timestamp, routine_id, slot_time,
    dose_amount, dose_unit, source, within_window_override, series_id, started_watch, notes, logged_by, test)
VALUES (@id, @medication_id, @status, @timestamp, @routine_id, @slot_time,
    @dose_amount, @dose_unit, @source, @within_window_override, @series_id, @started_watch, @notes, @logged_by, @test)
ON CONFLICT (id) DO UPDATE
    SET status                 = EXCLUDED.status,
        timestamp              = EXCLUDED.timestamp,
        routine_id             = EXCLUDED.routine_id,
        slot_time              = EXCLUDED.slot_time,
        dose_amount            = EXCLUDED.dose_amount,
        dose_unit              = EXCLUDED.dose_unit,
        source                 = EXCLUDED.source,
        within_window_override = EXCLUDED.within_window_override,
        series_id              = EXCLUDED.series_id,
        started_watch          = EXCLUDED.started_watch,
        notes                  = EXCLUDED.notes,
        logged_by              = EXCLUDED.logged_by,
        deleted_at             = NULL;

-- name: UpdateMedicationEvent :exec
-- History dose correction: timestamp + dose only. The actor (logged_by) is the
-- immutable record of who administered the dose and is never rewritten by an edit.
UPDATE medication_events
SET timestamp = @timestamp, dose_amount = @dose_amount
WHERE id = @id AND deleted_at IS NULL;

-- name: SoftDeleteMedicationEvent :exec
UPDATE medication_events SET deleted_at = NOW() WHERE id = @id AND deleted_at IS NULL;

-- name: ListRecentMedicationEvents :many
SELECT id, medication_id, status, timestamp, routine_id, slot_time, dose_amount, dose_unit,
    source, within_window_override, series_id, started_watch, notes, logged_by
FROM medication_events
WHERE deleted_at IS NULL AND timestamp >= @since
ORDER BY timestamp DESC;

-- name: InsertMedicationSeries :exec
INSERT INTO medication_temp_series (id, medication_id, interval_hours, anchor_dose_id, status, logged_by, test)
VALUES (@id, @medication_id, @interval_hours, @anchor_dose_id, 'active', @logged_by, @test)
ON CONFLICT (id) DO UPDATE
    SET interval_hours = EXCLUDED.interval_hours,
        anchor_dose_id = EXCLUDED.anchor_dose_id,
        status         = 'active',
        logged_by      = EXCLUDED.logged_by,
        deleted_at     = NULL;

-- name: EndMedicationSeries :exec
UPDATE medication_temp_series
SET status = @status, ended_reason = @ended_reason
WHERE id = @id AND deleted_at IS NULL;

-- name: ListActiveMedicationSeries :many
SELECT id, medication_id, interval_hours, anchor_dose_id, status, ended_reason
FROM medication_temp_series
WHERE deleted_at IS NULL AND status = 'active'
ORDER BY created_at;

-- ── Server-owned computations (ROADMAP-0011 effort 0011.3) ────────────────────

-- name: CountGivenForRoutine :one
-- Total given doses for a routine (drives max_doses auto-complete; not windowed).
SELECT count(*)::int FROM medication_events
WHERE routine_id = @routine_id AND status = 'given' AND deleted_at IS NULL;

-- name: SetRoutineStatus :exec
UPDATE medication_routines SET status = @status WHERE id = @id AND deleted_at IS NULL;

-- name: GetLastGivenForMedication :one
SELECT timestamp FROM medication_events
WHERE medication_id = @medication_id AND status = 'given' AND deleted_at IS NULL
ORDER BY timestamp DESC LIMIT 1;
