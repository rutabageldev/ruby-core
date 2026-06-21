//go:build fast

package ada

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/store"
)

func medRow(id, routineID, seriesID, status string, ts time.Time) *store.ListRecentMedicationEventsRow {
	return &store.ListRecentMedicationEventsRow{
		ID:        id,
		Status:    status,
		Timestamp: pgtype.Timestamptz{Time: ts, Valid: true},
		RoutineID: pgtype.Text{String: routineID, Valid: routineID != ""},
		SeriesID:  pgtype.Text{String: seriesID, Valid: seriesID != ""},
	}
}

// A watch re-anchors to its most recent given dose (max timestamp), so a later or
// back-dated watched dose moves next_due — never the original anchor or a skip.
func TestLatestWatchedDose(t *testing.T) {
	events := []*store.ListRecentMedicationEventsRow{
		medRow("d1", "", "s1", "given", tt(10, 0)),
		medRow("d3", "", "s1", "given", tt(12, 0)), // latest for s1
		medRow("d2", "", "s1", "given", tt(11, 0)),
		medRow("other", "", "s2", "given", tt(13, 0)),
		medRow("skip", "", "s1", "skipped", tt(14, 0)), // skipped never anchors
	}
	if got := latestWatchedDose(events, "s1"); got == nil || got.ID != "d3" {
		t.Errorf("latestWatchedDose(s1) = %v, want d3", got)
	}
	if got := latestWatchedDose(events, "missing"); got != nil {
		t.Errorf("latestWatchedDose(missing) = %v, want nil", got)
	}
	if got := latestWatchedDose(events, ""); got != nil {
		t.Errorf("latestWatchedDose(\"\") = %v, want nil", got)
	}
}

// An interval routine's next_due is driven by the latest given dose for the routine
// (max timestamp), so logging a later/back-dated dose recomputes it.
func TestLastGivenForRoutine(t *testing.T) {
	events := []*store.ListRecentMedicationEventsRow{
		medRow("a", "rt1", "", "given", tt(10, 0)),
		medRow("c", "rt1", "", "given", tt(12, 0)), // latest for rt1
		medRow("b", "rt1", "", "given", tt(11, 0)),
		medRow("x", "rt2", "", "given", tt(13, 0)),
	}
	if got := lastGivenForRoutine(events, "rt1"); got == nil || got.ID != "c" {
		t.Errorf("lastGivenForRoutine(rt1) = %v, want c", got)
	}
}
