//go:build fast

package handlers

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/primaryrutabaga/ruby-core/pkg/calendar/expand"
	"github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
)

func mkUUID(b byte) pgtype.UUID { return pgtype.UUID{Bytes: [16]byte{b}, Valid: true} }
func txt(s string) pgtype.Text  { return pgtype.Text{String: s, Valid: s != ""} }

// TestBuildEmailIndex covers the #133 multi-email reconciliation: aliases resolve to their
// person (case-insensitively), inactive people's aliases are ignored, and a primary email
// wins over a different person's alias on collision.
func TestBuildEmailIndex(t *testing.T) {
	p1, p2, inactive := mkUUID(1), mkUUID(2), mkUUID(9)
	people := []*store.DirectoryPerson{
		{ID: p1, Email: txt("Mom@example.com")},
		{ID: p2, Email: txt("dad@example.com")},
	}
	aliases := []*store.ListAllPersonEmailsRow{
		{PersonID: p1, Email: "Mom.Alias@Gmail.com"},  // alias → p1 (case-folded)
		{PersonID: inactive, Email: "ghost@example.com"}, // inactive (not in people) → ignored
		{PersonID: p2, Email: "MOM@example.com"},       // collides with p1's primary → must not win
	}

	idx := buildEmailIndex(people, aliases)

	if got := idx["mom@example.com"]; got != uuidStr(p1) {
		t.Errorf("primary collision: mom@example.com = %q, want p1 %q", got, uuidStr(p1))
	}
	if got := idx["mom.alias@gmail.com"]; got != uuidStr(p1) {
		t.Errorf("alias mom.alias@gmail.com = %q, want p1 %q", got, uuidStr(p1))
	}
	if _, ok := idx["ghost@example.com"]; ok {
		t.Error("an inactive person's alias must not be indexed")
	}
	if len(idx) != 3 { // mom primary, dad primary, mom alias
		t.Errorf("index size = %d, want 3 (%v)", len(idx), idx)
	}
}

// TestToAPIInstance_RecurrenceFields covers the #155 §1 read-API exposure: recurring
// occurrences carry recurrence + recurring_event_id + original_start; override instances
// carry the master id and pinned original slot; single events carry none of them.
func TestToAPIInstance_RecurrenceFields(t *testing.T) {
	occ := time.Date(2026, 6, 22, 13, 0, 0, 0, time.UTC)

	t.Run("recurring master occurrence", func(t *testing.T) {
		row := &store.CalendarEvent{
			GoogleEventID: "master1", Status: "confirmed", Etag: "etag-m1",
			Recurrence: []string{"RRULE:FREQ=WEEKLY;BYDAY=MO"},
		}
		ci := toAPIInstance(expand.Instance{GoogleEventID: "master1", Start: occ, End: occ.Add(time.Hour)}, row)
		if ci.Etag.Value != "etag-m1" {
			t.Errorf("etag = %q", ci.Etag.Value)
		}
		if len(ci.Recurrence) != 1 || ci.Recurrence[0] != "RRULE:FREQ=WEEKLY;BYDAY=MO" {
			t.Errorf("recurrence = %v", ci.Recurrence)
		}
		if ci.RecurringEventID.Value != "master1" {
			t.Errorf("recurring_event_id = %q, want master1", ci.RecurringEventID.Value)
		}
		if !ci.OriginalStart.Set || !ci.OriginalStart.Value.Equal(occ) {
			t.Errorf("original_start = %v (set=%v), want %v", ci.OriginalStart.Value, ci.OriginalStart.Set, occ)
		}
	})

	t.Run("override instance", func(t *testing.T) {
		moved := occ.Add(2 * time.Hour)
		row := &store.CalendarEvent{
			GoogleEventID: "ovr1", Status: "confirmed", Etag: "etag-o1",
			RecurringEventID:      txt("master1"),
			OriginalStartDatetime: pgtype.Timestamptz{Time: occ, Valid: true},
		}
		ci := toAPIInstance(expand.Instance{GoogleEventID: "ovr1", Start: moved, End: moved.Add(time.Hour), IsOverride: true}, row)
		if ci.RecurringEventID.Value != "master1" {
			t.Errorf("recurring_event_id = %q, want master1", ci.RecurringEventID.Value)
		}
		if !ci.OriginalStart.Set || !ci.OriginalStart.Value.Equal(occ) {
			t.Errorf("original_start = %v, want pinned slot %v (not the moved time)", ci.OriginalStart.Value, occ)
		}
		if len(ci.Recurrence) != 0 {
			t.Errorf("override must not carry recurrence, got %v", ci.Recurrence)
		}
	})

	t.Run("single event", func(t *testing.T) {
		row := &store.CalendarEvent{GoogleEventID: "s1", Status: "confirmed", Etag: "etag-s1"}
		ci := toAPIInstance(expand.Instance{GoogleEventID: "s1", Start: occ, End: occ.Add(time.Hour)}, row)
		if len(ci.Recurrence) != 0 || ci.RecurringEventID.Set || ci.OriginalStart.Set {
			t.Errorf("single event should carry no recurrence fields: rec=%v rid=%v os=%v",
				ci.Recurrence, ci.RecurringEventID.Set, ci.OriginalStart.Set)
		}
	})
}
