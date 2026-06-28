//go:build fast

package calendar

import (
	"testing"

	calendarv3 "google.golang.org/api/calendar/v3"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

func TestPayloadToGoogle_Timed(t *testing.T) {
	p := &schemas.CalendarUpsertData{
		Summary: "Dentist",
		Start:   schemas.CalendarEventDate{DateTime: "2026-06-26T09:00:00-04:00", TimeZone: "America/New_York"},
		End:     schemas.CalendarEventDate{DateTime: "2026-06-26T10:00:00-04:00", TimeZone: "America/New_York"},
	}
	g := payloadToGoogle(p)
	if g.Start.DateTime != p.Start.DateTime || g.Start.TimeZone != "America/New_York" {
		t.Errorf("timed start mismapped: %+v", g.Start)
	}
	if g.Start.Date != "" {
		t.Error("timed event must not set Date")
	}
}

func TestPayloadToGoogle_AllDay(t *testing.T) {
	p := &schemas.CalendarUpsertData{
		Summary: "Trip",
		AllDay:  true,
		Start:   schemas.CalendarEventDate{Date: "2026-06-26"},
		End:     schemas.CalendarEventDate{Date: "2026-06-27"}, // exclusive end
	}
	g := payloadToGoogle(p)
	if g.Start.Date != "2026-06-26" || g.End.Date != "2026-06-27" {
		t.Errorf("all-day dates mismapped: start=%q end=%q", g.Start.Date, g.End.Date)
	}
	if g.Start.DateTime != "" {
		t.Error("all-day event must not set DateTime")
	}
}

func TestSplitDateTime(t *testing.T) {
	// Timed: derived UTC is the instant; tz preserved; date null.
	date, dt, tz, utc, err := splitDateTime(&calendarv3.EventDateTime{
		DateTime: "2026-06-26T09:00:00-04:00", TimeZone: "America/New_York",
	})
	if err != nil {
		t.Fatalf("splitDateTime timed: %v", err)
	}
	if date.Valid {
		t.Error("timed event should have null date")
	}
	if !dt.Valid || tz.String != "America/New_York" {
		t.Errorf("timed datetime/tz mismapped: dt=%v tz=%v", dt, tz)
	}
	if utc.Hour() != 13 { // 09:00 EDT == 13:00 UTC
		t.Errorf("derived UTC hour = %d, want 13", utc.Hour())
	}

	// All-day: date valid, datetime null, UTC midnight.
	date, dt, _, utc, err = splitDateTime(&calendarv3.EventDateTime{Date: "2026-06-26"})
	if err != nil {
		t.Fatalf("splitDateTime all-day: %v", err)
	}
	if !date.Valid || dt.Valid {
		t.Error("all-day should set date and null datetime")
	}
	if utc.Hour() != 0 {
		t.Errorf("all-day derived UTC should be midnight, got hour %d", utc.Hour())
	}
}

func TestTrimEtag(t *testing.T) {
	if got := trimEtag(`"abc123"`); got != "abc123" {
		t.Errorf("trimEtag = %q, want abc123", got)
	}
	if got := trimEtag("noquotes"); got != "noquotes" {
		t.Errorf("trimEtag = %q, want noquotes", got)
	}
}
