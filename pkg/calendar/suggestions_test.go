//go:build fast

package calendar

import (
	"testing"
	"time"

	"github.com/primaryrutabaga/ruby-core/pkg/calendar/expand"
)

// TestRankProviderUsage_WeeklyOutranksOneOff proves a provider on a weekly series
// scores higher than one with a single past event (per-occurrence counting).
func TestRankProviderUsage_WeeklyOutranksOneOff(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, loc)

	weeklyStart := now.AddDate(0, 0, -56) // 8 weeks ago, recurs weekly
	weekly := expand.Event{
		GoogleEventID: "wk", Start: weeklyStart, End: weeklyStart.Add(time.Hour),
		Location: loc, Recurrence: []string{"RRULE:FREQ=WEEKLY"},
	}
	oneOffStart := now.AddDate(0, 0, -7) // a single event a week ago
	oneOff := expand.Event{
		GoogleEventID: "one", Start: oneOffStart, End: oneOffStart.Add(time.Hour), Location: loc,
	}

	scores := RankProviderUsage(map[string][]expand.Event{
		"weekly":  {weekly},
		"oneoff":  {oneOff},
		"unused":  {},
	}, now, DefaultSuggestionWindow)

	if scores["weekly"] <= scores["oneoff"] {
		t.Errorf("weekly (%.2f) should outrank one-off (%.2f)", scores["weekly"], scores["oneoff"])
	}
	if scores["oneoff"] <= scores["unused"] {
		t.Errorf("one-off (%.2f) should outrank unused (%.2f)", scores["oneoff"], scores["unused"])
	}
	if scores["unused"] != 0 {
		t.Errorf("unused provider score = %.2f, want 0", scores["unused"])
	}
}
