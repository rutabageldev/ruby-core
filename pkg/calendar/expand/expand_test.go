//go:build fast

package expand

import (
	"testing"
	"time"
)

func nyc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load NYC tz: %v", err)
	}
	return loc
}

// weekly builds a weekly-on-Monday 9am ET event starting 2026-03-02 (the Monday
// before US DST begins on 2026-03-08), 1h long.
func weekly(t *testing.T) Event {
	loc := nyc(t)
	start := time.Date(2026, 3, 2, 9, 0, 0, 0, loc)
	return Event{
		GoogleEventID: "evt-weekly",
		Start:         start,
		End:           start.Add(time.Hour),
		Location:      loc,
		Recurrence:    []string{"RRULE:FREQ=WEEKLY;BYDAY=MO"},
	}
}

// instanceOn returns the instance whose start falls on the given calendar day in ET.
func instanceOn(t *testing.T, insts []Instance, loc *time.Location, y int, m time.Month, d int) (Instance, bool) {
	t.Helper()
	for _, in := range insts {
		ld := in.Start.In(loc)
		if ld.Year() == y && ld.Month() == m && ld.Day() == d {
			return in, true
		}
	}
	return Instance{}, false
}

func march(t *testing.T) (from, to time.Time) {
	return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
}

// TestExpand_DSTAware proves a weekly 9am-ET slot stays 9am local across the
// spring-forward boundary — i.e. its UTC offset changes (14:00Z before DST,
// 13:00Z after) while the local wall-clock time is constant.
func TestExpand_DSTAware(t *testing.T) {
	loc := nyc(t)
	from, to := march(t)
	insts, err := Expand(weekly(t), nil, from, to)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}

	mar2, ok := instanceOn(t, insts, loc, 2026, time.March, 2)
	if !ok {
		t.Fatal("missing Mar 2 occurrence")
	}
	if h := mar2.Start.UTC().Hour(); h != 14 {
		t.Errorf("Mar 2 (EST) start = %02d:00Z, want 14:00Z", h)
	}

	mar9, ok := instanceOn(t, insts, loc, 2026, time.March, 9)
	if !ok {
		t.Fatal("missing Mar 9 occurrence")
	}
	if h := mar9.Start.UTC().Hour(); h != 13 {
		t.Errorf("Mar 9 (EDT) start = %02d:00Z, want 13:00Z", h)
	}

	// Both must still be 9am local.
	for _, in := range []Instance{mar2, mar9} {
		if h := in.Start.In(loc).Hour(); h != 9 {
			t.Errorf("occurrence %s is %02d local, want 09", in.Start.In(loc).Format("Jan 2"), h)
		}
	}
}

// TestExpand_EXDATE removes a single occurrence via an EXDATE line.
func TestExpand_EXDATE(t *testing.T) {
	loc := nyc(t)
	from, to := march(t)
	ev := weekly(t)
	ev.Recurrence = append(ev.Recurrence, "EXDATE;TZID=America/New_York:20260309T090000")

	insts, err := Expand(ev, nil, from, to)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if _, ok := instanceOn(t, insts, loc, 2026, time.March, 9); ok {
		t.Error("Mar 9 should have been excluded by EXDATE")
	}
	if _, ok := instanceOn(t, insts, loc, 2026, time.March, 16); !ok {
		t.Error("Mar 16 should still be present")
	}
}

// TestExpand_CancelledOverride subtracts a cancelled child occurrence.
func TestExpand_CancelledOverride(t *testing.T) {
	loc := nyc(t)
	from, to := march(t)
	cancel := Override{OriginalStart: time.Date(2026, 3, 16, 9, 0, 0, 0, loc), Cancelled: true}

	insts, err := Expand(weekly(t), []Override{cancel}, from, to)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if _, ok := instanceOn(t, insts, loc, 2026, time.March, 16); ok {
		t.Error("Mar 16 should have been subtracted by the cancelled override")
	}
}

// TestExpand_ModifiedOverride replaces a base occurrence with its override.
func TestExpand_ModifiedOverride(t *testing.T) {
	loc := nyc(t)
	from, to := march(t)
	moved := time.Date(2026, 3, 23, 11, 0, 0, 0, loc) // moved 9am -> 11am
	ovr := Override{
		OriginalStart: time.Date(2026, 3, 23, 9, 0, 0, 0, loc),
		Instance: Instance{
			GoogleEventID: "evt-weekly_override",
			Start:         moved.UTC(),
			End:           moved.Add(time.Hour).UTC(),
			IsOverride:    true,
		},
	}

	insts, err := Expand(weekly(t), []Override{ovr}, from, to)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	got, ok := instanceOn(t, insts, loc, 2026, time.March, 23)
	if !ok {
		t.Fatal("Mar 23 occurrence missing")
	}
	if h := got.Start.In(loc).Hour(); h != 11 {
		t.Errorf("Mar 23 override start = %02d local, want 11", h)
	}
	if !got.IsOverride {
		t.Error("Mar 23 instance should be flagged IsOverride")
	}
}

// TestExpand_SingleEvent returns one instance for a non-recurring event overlapping
// the window, and none when it falls outside.
func TestExpand_SingleEvent(t *testing.T) {
	loc := nyc(t)
	start := time.Date(2026, 3, 10, 14, 0, 0, 0, loc)
	ev := Event{GoogleEventID: "one", Start: start, End: start.Add(90 * time.Minute), Location: loc}

	from, to := march(t)
	insts, err := Expand(ev, nil, from, to)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(insts) != 1 {
		t.Fatalf("got %d instances, want 1", len(insts))
	}

	// Outside the window => no instances.
	outFrom := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	outTo := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	insts, err = Expand(ev, nil, outFrom, outTo)
	if err != nil {
		t.Fatalf("Expand (outside): %v", err)
	}
	if len(insts) != 0 {
		t.Errorf("got %d instances outside window, want 0", len(insts))
	}
}

// TestCheckWindow enforces the max-window guard and rejects inverted ranges.
func TestCheckWindow(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := CheckWindow(from, from.AddDate(0, 0, 30)); err != nil {
		t.Errorf("30-day window should be allowed: %v", err)
	}
	if err := CheckWindow(from, from.AddDate(0, 0, 400)); err == nil {
		t.Error("400-day window should exceed the guard")
	}
	if err := CheckWindow(from, from); err == nil {
		t.Error("zero-length window should be rejected")
	}
}
