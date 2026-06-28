// Package calendar holds shared mapping between stored calendar rows and the
// expansion model, used by both the engine processor and the read API so neither
// owns the other's types.
package calendar

import (
	"time"

	"github.com/primaryrutabaga/ruby-core/pkg/calendar/expand"
	"github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
)

// RowToEvent maps a stored master/single event to an expansion input. The IANA zone
// comes from the stored start timezone (UTC for all-day / missing).
func RowToEvent(row *store.CalendarEvent) expand.Event {
	loc := time.UTC
	if row.StartTimezone.Valid && row.StartTimezone.String != "" {
		if l, err := time.LoadLocation(row.StartTimezone.String); err == nil {
			loc = l
		}
	}
	return expand.Event{
		GoogleEventID: row.GoogleEventID,
		Start:         row.StartUtc.Time,
		End:           row.EndUtc.Time,
		Location:      loc,
		AllDay:        row.AllDay,
		Recurrence:    row.Recurrence,
	}
}

// RowToOverride maps a stored override/cancelled child to an expansion override.
func RowToOverride(row *store.CalendarEvent) expand.Override {
	orig := row.StartUtc.Time
	switch {
	case row.OriginalStartDatetime.Valid:
		orig = row.OriginalStartDatetime.Time
	case row.OriginalStartDate.Valid:
		orig = row.OriginalStartDate.Time.UTC()
	}
	return expand.Override{
		OriginalStart: orig,
		Cancelled:     row.Status == "cancelled",
		Instance: expand.Instance{
			GoogleEventID: row.GoogleEventID,
			Start:         row.StartUtc.Time,
			End:           row.EndUtc.Time,
			AllDay:        row.AllDay,
			IsOverride:    true,
		},
	}
}
