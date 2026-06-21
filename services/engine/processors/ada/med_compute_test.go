//go:build fast

package ada

import (
	"testing"
	"time"
)

func tt(h, m int) time.Time { return time.Date(2026, 6, 20, h, m, 0, 0, time.UTC) }

// earliest_safe = lastGiven + min_interval; unsafe ONLY when now < earliest_safe,
// so a dose exactly at the boundary is safe. No limit is always safe.
func TestEarliestSafeBoundary(t *testing.T) {
	last := tt(10, 0)
	if got := earliestSafe(last, 4); !got.Equal(tt(14, 0)) {
		t.Fatalf("earliestSafe = %v, want 14:00", got)
	}
	// unsafe is derived as now < earliest_safe — safe exactly at the boundary.
	cases := []struct {
		now    time.Time
		unsafe bool
	}{
		{tt(13, 59), true},
		{tt(14, 0), false}, // safe exactly at the boundary
		{tt(14, 1), false},
	}
	es := earliestSafe(last, 4)
	for _, c := range cases {
		if got := c.now.Before(es); got != c.unsafe {
			t.Errorf("now=%s before earliestSafe = %v, want unsafe=%v", c.now.Format("15:04"), got, c.unsafe)
		}
	}
}

// doses_in_24h counts given over the half-open window (now-24h, now].
func TestDosesInRolling24h(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	lower := now.Add(-24 * time.Hour)
	given := []time.Time{
		lower,                  // exactly now-24h -> excluded (strict >)
		lower.Add(time.Second), // included
		now.Add(-time.Hour),    // included
		now,                    // included (<= now)
		now.Add(time.Second),   // future -> excluded
	}
	if got := dosesInRolling24h(given, now); got != 3 {
		t.Errorf("dosesInRolling24h = %d, want 3", got)
	}
}

func TestRoutineCompleteMaxDoses(t *testing.T) {
	now := tt(12, 0)
	if routineComplete("max_doses", "10", 9, now) {
		t.Error("9 of 10 should be incomplete")
	}
	if !routineComplete("max_doses", "10", 10, now) {
		t.Error("10 of 10 should be complete")
	}
	if !routineComplete("max_doses", "10", 11, now) {
		t.Error("11 of 10 should be complete")
	}
}

func TestRoutineCompleteEndDate(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	if routineComplete("end_date", "2026-06-21", 0, now) {
		t.Error("end tomorrow -> incomplete")
	}
	if routineComplete("end_date", "2026-06-20", 0, now) {
		t.Error("end today -> incomplete (today > end is false when equal)")
	}
	if !routineComplete("end_date", "2026-06-19", 0, now) {
		t.Error("end yesterday -> complete")
	}
	if routineComplete("none", "", 100, now) {
		t.Error("'none' never completes by rule")
	}
}

// Each slot resolves independently; a slot is missed only when a later slot has
// come due (supersession). A later due slot is never inflated by earlier missed
// ones — it stays a single `due`, so missed never stacks/carries forward.
func TestSlotStatusSupersessionNeverStacks(t *testing.T) {
	slots := []string{"08:00", "13:00", "18:00"}

	now := tt(13, 30) // 08:00 superseded by 13:00; 13:00 due; 18:00 upcoming
	for s, want := range map[string]slotState{"08:00": slotMissed, "13:00": slotDue, "18:00": slotUpcoming} {
		if got := slotStatus("", s, slots, now); got != want {
			t.Errorf("@13:30 slot %s = %v, want %v", s, got, want)
		}
	}

	now = tt(18, 30) // 08:00 + 13:00 each missed; 18:00 is a single `due`, not inflated
	for s, want := range map[string]slotState{"08:00": slotMissed, "13:00": slotMissed, "18:00": slotDue} {
		if got := slotStatus("", s, slots, now); got != want {
			t.Errorf("@18:30 slot %s = %v, want %v", s, got, want)
		}
	}

	if got := slotStatus("given", "08:00", slots, tt(18, 30)); got != slotGiven {
		t.Errorf("an acted (given) slot stays given, got %v", got)
	}
	if got := slotStatus("skipped", "08:00", slots, tt(18, 30)); got != slotSkipped {
		t.Errorf("an acted (skipped) slot stays skipped, got %v", got)
	}
}

func TestSeriesNextDue(t *testing.T) {
	if got := seriesNextDue(tt(10, 0), 6); !got.Equal(tt(16, 0)) {
		t.Errorf("seriesNextDue = %v, want 16:00", got)
	}
}
