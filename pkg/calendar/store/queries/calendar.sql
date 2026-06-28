-- name: UpsertEvent :exec
INSERT INTO calendar_event (
    google_event_id, ical_uid, summary,
    start_date, start_datetime, start_timezone,
    end_date, end_datetime, end_timezone, all_day,
    start_utc, end_utc,
    recurrence, recurring_event_id,
    original_start_date, original_start_datetime, original_start_timezone,
    location, description, calendar_id, status, etag, sequence, raw, synced_at
) VALUES (
    @google_event_id, @ical_uid, @summary,
    @start_date, @start_datetime, @start_timezone,
    @end_date, @end_datetime, @end_timezone, @all_day,
    @start_utc, @end_utc,
    @recurrence, @recurring_event_id,
    @original_start_date, @original_start_datetime, @original_start_timezone,
    @location, @description, @calendar_id, @status, @etag, @sequence, @raw, now()
)
ON CONFLICT (google_event_id) DO UPDATE SET
    ical_uid = EXCLUDED.ical_uid,
    summary = EXCLUDED.summary,
    start_date = EXCLUDED.start_date,
    start_datetime = EXCLUDED.start_datetime,
    start_timezone = EXCLUDED.start_timezone,
    end_date = EXCLUDED.end_date,
    end_datetime = EXCLUDED.end_datetime,
    end_timezone = EXCLUDED.end_timezone,
    all_day = EXCLUDED.all_day,
    start_utc = EXCLUDED.start_utc,
    end_utc = EXCLUDED.end_utc,
    recurrence = EXCLUDED.recurrence,
    recurring_event_id = EXCLUDED.recurring_event_id,
    original_start_date = EXCLUDED.original_start_date,
    original_start_datetime = EXCLUDED.original_start_datetime,
    original_start_timezone = EXCLUDED.original_start_timezone,
    location = EXCLUDED.location,
    description = EXCLUDED.description,
    calendar_id = EXCLUDED.calendar_id,
    status = EXCLUDED.status,
    etag = EXCLUDED.etag,
    sequence = EXCLUDED.sequence,
    raw = EXCLUDED.raw,
    synced_at = now();

-- name: GetEvent :one
SELECT * FROM calendar_event WHERE google_event_id = @google_event_id;

-- name: DeleteEvent :exec
DELETE FROM calendar_event WHERE google_event_id = @google_event_id;

-- ListSingleEventsInRange returns non-recurring, non-override, non-cancelled events
-- whose [start_utc, end_utc) overlaps the requested window.
-- name: ListSingleEventsInRange :many
SELECT * FROM calendar_event
WHERE recurrence IS NULL
  AND recurring_event_id IS NULL
  AND status <> 'cancelled'
  AND end_utc > @range_start
  AND start_utc < @range_end
ORDER BY start_utc;

-- ListRecurringMasters returns every recurring series master (no range filter —
-- expansion decides which occurrences fall in the window).
-- name: ListRecurringMasters :many
SELECT * FROM calendar_event
WHERE recurrence IS NOT NULL
  AND status <> 'cancelled'
ORDER BY start_utc;

-- ListOverrides returns every override/cancelled child across all series; the
-- caller groups them by recurring_event_id and subtracts/applies during expansion.
-- name: ListOverrides :many
SELECT * FROM calendar_event
WHERE recurring_event_id IS NOT NULL
ORDER BY recurring_event_id, start_utc;
