package calendar

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	calendarv3 "google.golang.org/api/calendar/v3"

	"github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

const dateLayout = "2006-01-02"

// payloadToGoogle maps a calendar.event.upsert payload to a Google Calendar event
// for write-through. Only Google-owned fields are set; overlay fields (subjects,
// childcare) are never written to Google (ADR-0042).
func payloadToGoogle(p *schemas.CalendarUpsertData) *calendarv3.Event {
	return &calendarv3.Event{
		Summary:     p.Summary,
		Location:    p.Location,
		Description: p.Description,
		Start:       payloadDateToGoogle(p.Start, p.AllDay),
		End:         payloadDateToGoogle(p.End, p.AllDay),
		Recurrence:  p.Recurrence,
	}
}

func payloadDateToGoogle(d schemas.CalendarEventDate, allDay bool) *calendarv3.EventDateTime {
	if allDay {
		return &calendarv3.EventDateTime{Date: d.Date}
	}
	return &calendarv3.EventDateTime{DateTime: d.DateTime, TimeZone: d.TimeZone}
}

// googleToParams maps a Google event to mirror upsert params, computing the derived
// UTC anchors used for range queries. Called by both the poller and write-through.
func googleToParams(ev *calendarv3.Event, calendarID string) (*store.UpsertEventParams, error) {
	startDate, startDT, startTZ, startUTC, err := splitDateTime(ev.Start)
	if err != nil {
		return nil, fmt.Errorf("calendar: map start: %w", err)
	}
	endDate, endDT, endTZ, endUTC, err := splitDateTime(ev.End)
	if err != nil {
		return nil, fmt.Errorf("calendar: map end: %w", err)
	}

	var origDate pgtype.Date
	var origDT pgtype.Timestamptz
	var origTZ pgtype.Text
	if ev.OriginalStartTime != nil {
		origDate, origDT, origTZ, _, err = splitDateTime(ev.OriginalStartTime)
		if err != nil {
			return nil, fmt.Errorf("calendar: map original_start: %w", err)
		}
	}

	raw, err := json.Marshal(ev)
	if err != nil {
		return nil, fmt.Errorf("calendar: marshal raw: %w", err)
	}

	status := ev.Status
	if status == "" {
		status = "confirmed"
	}

	return &store.UpsertEventParams{
		GoogleEventID:         ev.Id,
		IcalUid:               text(ev.ICalUID),
		Summary:               text(ev.Summary),
		StartDate:             startDate,
		StartDatetime:         startDT,
		StartTimezone:         startTZ,
		EndDate:               endDate,
		EndDatetime:           endDT,
		EndTimezone:           endTZ,
		AllDay:                ev.Start != nil && ev.Start.Date != "",
		StartUtc:              ts(startUTC),
		EndUtc:                ts(endUTC),
		Recurrence:            ev.Recurrence,
		RecurringEventID:      text(ev.RecurringEventId),
		OriginalStartDate:     origDate,
		OriginalStartDatetime: origDT,
		OriginalStartTimezone: origTZ,
		Location:              text(ev.Location),
		Description:           text(ev.Description),
		CalendarID:            calendarID,
		Status:                status,
		Etag:                  trimEtag(ev.Etag),
		Sequence:              int32(ev.Sequence), //nolint:gosec // G115: Google sequence is a small monotonic counter
		Raw:                   raw,
	}, nil
}

// splitDateTime converts a Google EventDateTime into the date-XOR-datetime trio plus
// a derived UTC anchor. All-day dates anchor at midnight UTC (internal range key).
func splitDateTime(d *calendarv3.EventDateTime) (pgtype.Date, pgtype.Timestamptz, pgtype.Text, time.Time, error) {
	if d == nil {
		return pgtype.Date{}, pgtype.Timestamptz{}, pgtype.Text{}, time.Time{}, fmt.Errorf("nil EventDateTime")
	}
	if d.DateTime != "" {
		t, err := time.Parse(time.RFC3339, d.DateTime)
		if err != nil {
			return pgtype.Date{}, pgtype.Timestamptz{}, pgtype.Text{}, time.Time{}, err
		}
		return pgtype.Date{}, ts(t.UTC()), text(d.TimeZone), t.UTC(), nil
	}
	// all-day date
	t, err := time.Parse(dateLayout, d.Date)
	if err != nil {
		return pgtype.Date{}, pgtype.Timestamptz{}, pgtype.Text{}, time.Time{}, err
	}
	return pgtype.Date{Time: t, Valid: true}, pgtype.Timestamptz{}, pgtype.Text{}, t.UTC(), nil
}

// --- small pgtype helpers ---

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

// trimEtag strips the surrounding quotes Google wraps around etags so the value
// round-trips cleanly as an If-Match header.
func trimEtag(e string) string {
	if len(e) >= 2 && e[0] == '"' && e[len(e)-1] == '"' {
		return e[1 : len(e)-1]
	}
	return e
}
