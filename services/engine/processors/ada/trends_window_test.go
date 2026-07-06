//go:build fast

package ada

import (
	"testing"
	"time"
)

// nyLoc loads America/New_York — the calendar location trends run in (container TZ).
func nyLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load America/New_York: %v", err)
	}
	return loc
}

func TestCalendarWindow_Week(t *testing.T) {
	loc := nyLoc(t)
	now := time.Date(2026, 7, 8, 15, 0, 0, 0, loc) // Wednesday

	start, end, ok := calendarWindow("week", 0, now)
	if !ok || !start.Equal(time.Date(2026, 7, 5, 0, 0, 0, 0, loc)) || !end.Equal(time.Date(2026, 7, 12, 0, 0, 0, 0, loc)) {
		t.Errorf("offset 0 = [%v, %v) ok=%v, want [Sun 7/5, Sun 7/12)", start, end, ok)
	}

	start, end, _ = calendarWindow("week", -1, now)
	if !start.Equal(time.Date(2026, 6, 28, 0, 0, 0, 0, loc)) || !end.Equal(time.Date(2026, 7, 5, 0, 0, 0, 0, loc)) {
		t.Errorf("offset -1 = [%v, %v), want [6/28, 7/5)", start, end)
	}

	// The week rolls the instant Sunday midnight arrives.
	sunday := time.Date(2026, 7, 5, 0, 0, 0, 0, loc)
	start, _, _ = calendarWindow("week", 0, sunday)
	if !start.Equal(sunday) {
		t.Errorf("at Sunday 00:00 window start = %v, want that instant", start)
	}

	if _, _, ok := calendarWindow("bogus", 0, now); ok {
		t.Error("unknown period should not be ok")
	}
}

func TestCalendarWindow_MonthYearOffsets(t *testing.T) {
	loc := nyLoc(t)
	now := time.Date(2026, 7, 5, 20, 0, 0, 0, loc)

	// Month offset crossing a year boundary relies on time.Date normalization.
	start, end, _ := calendarWindow("month", -7, now)
	if !start.Equal(time.Date(2025, 12, 1, 0, 0, 0, 0, loc)) || !end.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, loc)) {
		t.Errorf("month offset -7 = [%v, %v), want [2025-12-01, 2026-01-01)", start, end)
	}

	start, end, _ = calendarWindow("year", -1, now)
	if !start.Equal(time.Date(2025, 1, 1, 0, 0, 0, 0, loc)) || !end.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, loc)) {
		t.Errorf("year offset -1 = [%v, %v), want [2025-01-01, 2026-01-01)", start, end)
	}
}

func TestCalendarBuckets_Week_DST(t *testing.T) {
	loc := nyLoc(t)

	// Fall-back week: Sun 2026-11-01 has 25 wall-clock hours.
	start, end, _ := calendarWindow("week", 0, time.Date(2026, 11, 4, 12, 0, 0, 0, loc))
	bs := calendarBuckets("week", start, end)
	if len(bs) != 7 {
		t.Fatalf("week buckets = %d, want 7", len(bs))
	}
	if got := bs[0].end.Sub(bs[0].start); got != 25*time.Hour {
		t.Errorf("fall-back Sunday bucket = %v, want 25h", got)
	}
	if !bs[0].start.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, loc)) || !bs[0].end.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, loc)) {
		t.Errorf("fall-back Sunday bucket = [%v, %v), want local midnights", bs[0].start, bs[0].end)
	}
	for i := 1; i < len(bs); i++ {
		if !bs[i].start.Equal(bs[i-1].end) {
			t.Errorf("bucket %d not contiguous with %d", i, i-1)
		}
	}

	// Spring-forward week: Sun 2026-03-08 has 23 wall-clock hours.
	start, end, _ = calendarWindow("week", 0, time.Date(2026, 3, 10, 12, 0, 0, 0, loc))
	bs = calendarBuckets("week", start, end)
	if got := bs[0].end.Sub(bs[0].start); got != 23*time.Hour {
		t.Errorf("spring-forward Sunday bucket = %v, want 23h", got)
	}

	// Labels are weekdays starting Sunday.
	want := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	for i, b := range bs {
		if b.label != want[i] {
			t.Errorf("label[%d] = %q, want %q", i, b.label, want[i])
		}
	}
}

func TestCalendarBuckets_Month_SixWeeks(t *testing.T) {
	loc := nyLoc(t)
	// August 2026 starts on a Saturday → 1 + 4×7 + 2 = 6 Sun–Sat buckets clipped to the month.
	start, end, _ := calendarWindow("month", 0, time.Date(2026, 8, 15, 12, 0, 0, 0, loc))
	bs := calendarBuckets("month", start, end)
	if len(bs) != 6 {
		t.Fatalf("Aug 2026 buckets = %d, want 6", len(bs))
	}
	wantLabels := []string{"8/1", "8/2", "8/9", "8/16", "8/23", "8/30"}
	for i, b := range bs {
		if b.label != wantLabels[i] {
			t.Errorf("label[%d] = %q, want %q", i, b.label, wantLabels[i])
		}
	}
	if !bs[0].end.Equal(time.Date(2026, 8, 2, 0, 0, 0, 0, loc)) {
		t.Errorf("first bucket end = %v, want 8/2 (clipped 1-day edge week)", bs[0].end)
	}
	if !bs[5].end.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, loc)) {
		t.Errorf("last bucket end = %v, want 9/1 (clipped to month end)", bs[5].end)
	}
}

func TestCalendarBuckets_Month_FourWeeks(t *testing.T) {
	loc := nyLoc(t)
	// February 2026 starts on a Sunday and has 28 days → exactly 4 full Sun–Sat buckets.
	start, end, _ := calendarWindow("month", 0, time.Date(2026, 2, 10, 12, 0, 0, 0, loc))
	bs := calendarBuckets("month", start, end)
	if len(bs) != 4 {
		t.Fatalf("Feb 2026 buckets = %d, want 4", len(bs))
	}
	for i, b := range bs {
		if got := daysBetween(b.start, b.end); got != 7 {
			t.Errorf("bucket %d = %d days, want 7", i, got)
		}
	}
}

func TestCalendarBuckets_Year(t *testing.T) {
	loc := nyLoc(t)
	start, end, _ := calendarWindow("year", 0, time.Date(2028, 7, 5, 12, 0, 0, 0, loc)) // leap year
	bs := calendarBuckets("year", start, end)
	if len(bs) != 12 {
		t.Fatalf("year buckets = %d, want 12", len(bs))
	}
	if bs[0].label != "Jan" || bs[11].label != "Dec" {
		t.Errorf("labels = %q..%q, want Jan..Dec", bs[0].label, bs[11].label)
	}
	if got := daysBetween(bs[1].start, bs[1].end); got != 29 {
		t.Errorf("Feb 2028 bucket = %d days, want 29 (leap)", got)
	}
}

// TestCalendarBuckets_FullShape: the bucket shape depends only on the window, never on
// now — an offset-0 query mid-period still returns the full period shape (R3).
func TestCalendarBuckets_FullShape(t *testing.T) {
	loc := nyLoc(t)
	for _, now := range []time.Time{
		time.Date(2026, 7, 5, 0, 0, 1, 0, loc),    // first second of the week
		time.Date(2026, 7, 11, 23, 59, 0, 0, loc), // last minute
	} {
		start, end, _ := calendarWindow("week", 0, now)
		if got := len(calendarBuckets("week", start, end)); got != 7 {
			t.Errorf("week shape at %v = %d buckets, want 7", now, got)
		}
	}
	start, end, _ := calendarWindow("year", 0, time.Date(2026, 1, 2, 8, 0, 0, 0, loc))
	if got := len(calendarBuckets("year", start, end)); got != 12 {
		t.Errorf("year shape on Jan 2 = %d buckets, want 12", got)
	}
}

func TestDaysElapsed(t *testing.T) {
	loc := nyLoc(t)

	cases := []struct {
		name   string
		period string
		offset int
		now    time.Time
		want   int
	}{
		{"week day1 (Sunday)", "week", 0, time.Date(2026, 7, 5, 9, 0, 0, 0, loc), 1},
		{"week day4 (Wednesday)", "week", 0, time.Date(2026, 7, 8, 9, 0, 0, 0, loc), 4},
		{"year mid (Jul 5 = day 186)", "year", 0, time.Date(2026, 7, 5, 9, 0, 0, 0, loc), 186},
		{"month day1", "month", 0, time.Date(2026, 7, 1, 0, 30, 0, 0, loc), 1},
		{"past week full", "week", -1, time.Date(2026, 7, 8, 9, 0, 0, 0, loc), 7},
		{"past Feb 2026 full", "month", -5, time.Date(2026, 7, 8, 9, 0, 0, 0, loc), 28},
		{"past leap year full", "year", -2, time.Date(2026, 7, 8, 9, 0, 0, 0, loc), 366},
	}
	for _, tc := range cases {
		start, end, ok := calendarWindow(tc.period, tc.offset, tc.now)
		if !ok {
			t.Fatalf("%s: window not ok", tc.name)
		}
		if got := daysElapsedIn(start, end, tc.now, tc.offset); got != tc.want {
			t.Errorf("%s: days_elapsed = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestMinOffsetFor(t *testing.T) {
	loc := nyLoc(t)
	birth := time.Date(2026, 4, 30, 14, 25, 0, 0, loc)
	now := time.Date(2026, 7, 8, 9, 0, 0, 0, loc)

	if got := minOffsetFor("week", birth, now); got != -10 {
		t.Errorf("week min_offset = %d, want -10 (weekStart 4/26 → 7/5)", got)
	}
	if got := minOffsetFor("month", birth, now); got != -3 {
		t.Errorf("month min_offset = %d, want -3", got)
	}
	if got := minOffsetFor("year", birth, now); got != 0 {
		t.Errorf("year min_offset = %d, want 0", got)
	}
	// Birth in the current period, and future/placeholder births, clamp to 0.
	if got := minOffsetFor("week", now.AddDate(0, 0, -2), now); got != 0 {
		t.Errorf("same-week birth min_offset = %d, want 0", got)
	}
	if got := minOffsetFor("month", now.AddDate(0, 2, 0), now); got != 0 {
		t.Errorf("future birth min_offset = %d, want 0", got)
	}
	// UTC-instant birth converts into the calendar location before week math.
	if got := minOffsetFor("week", birth.UTC(), now); got != -10 {
		t.Errorf("UTC birth min_offset = %d, want -10", got)
	}
}

func TestNormalizeOffset(t *testing.T) {
	for in, want := range map[int]int{3: 0, 0: 0, -5: -5} {
		if got := normalizeOffset(in); got != want {
			t.Errorf("normalizeOffset(%d) = %d, want %d", in, got, want)
		}
	}
}
