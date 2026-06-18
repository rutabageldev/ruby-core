//go:build fast

package ada

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/store"
)

// ── #74 supplement amount routing ─────────────────────────────────────────────

func TestSupplementAmounts(t *testing.T) {
	cases := []struct {
		name                       string
		source                     string
		amountOz, bmOz, foOz       float64
		wantAmount, wantBM, wantFO float64
	}{
		{"mixed", "mixed", 0, 2, 3, 5, 2, 3},
		{"single breast_milk (dashboard)", "breast_milk", 4, 0, 0, 4, 4, 0},
		{"single formula (dashboard)", "formula", 4, 0, 0, 4, 0, 4},
		{"single canonical bottle_breast", "bottle_breast", 1.5, 0, 0, 1.5, 1.5, 0},
		{"single canonical bottle_formula", "bottle_formula", 1.5, 0, 0, 1.5, 0, 1.5},
		{"legacy/unknown falls back to amount", "weird", 2, 0, 0, 2, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			amount, bm, fo := supplementAmounts(c.source, c.amountOz, c.bmOz, c.foOz)
			if amount != c.wantAmount || bm != c.wantBM || fo != c.wantFO {
				t.Errorf("supplementAmounts(%q, %v, %v, %v) = (%v, %v, %v); want (%v, %v, %v)",
					c.source, c.amountOz, c.bmOz, c.foOz, amount, bm, fo, c.wantAmount, c.wantBM, c.wantFO)
			}
		})
	}
}

// ── #75 tummy history ──────────────────────────────────────────────────────────

func TestBuildTummyHistory(t *testing.T) {
	start := time.Date(2026, 6, 18, 14, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Minute)
	rows := []*store.GetLast24hTummyRow{
		{
			ID:        pgUUID(7),
			StartTime: toTimestamptz(start),
			EndTime:   toTimestamptz(end),
			DurationS: 300,
			LoggedBy:  "katie",
		},
	}
	entries := buildTummyHistory(rows)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.ID == "" {
		t.Error("expected non-empty id")
	}
	if e.StartTime != start.Format(time.RFC3339) {
		t.Errorf("start_time = %q, want %q", e.StartTime, start.Format(time.RFC3339))
	}
	if e.EndTime != end.Format(time.RFC3339) {
		t.Errorf("end_time = %q, want %q", e.EndTime, end.Format(time.RFC3339))
	}
	if e.DurationS != 300 {
		t.Errorf("duration_s = %d, want 300", e.DurationS)
	}
	if e.LoggedBy != "katie" {
		t.Errorf("logged_by = %q, want %q", e.LoggedBy, "katie")
	}
}

func TestBuildTummyHistory_Empty(t *testing.T) {
	entries := buildTummyHistory(nil)
	if entries == nil {
		t.Fatal("expected non-nil empty slice (so JSON marshals to [])")
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// ── #80 growth history logged_by ───────────────────────────────────────────────

func TestBuildGrowthHistory_LoggedBy(t *testing.T) {
	measured := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	rows := []*store.GetAllGrowthMeasurementsRow{
		{
			ID:         pgUUID(1),
			MeasuredAt: toTimestamptz(measured),
			WeightOz:   numericFromFloat(160),
			Source:     "home",
			LoggedBy:   "michael",
		},
		{
			ID:         pgUUID(2),
			MeasuredAt: toTimestamptz(measured),
			LengthIn:   numericFromFloat(22),
			Source:     "pediatrician",
			LoggedBy:   "katie",
		},
	}
	weight, length, head := buildGrowthHistory(rows)
	if len(weight) != 1 || weight[0].LoggedBy != "michael" {
		t.Errorf("weight logged_by = %+v, want one entry by michael", weight)
	}
	if len(length) != 1 || length[0].LoggedBy != "katie" {
		t.Errorf("length logged_by = %+v, want one entry by katie", length)
	}
	if len(head) != 0 {
		t.Errorf("expected no head entries, got %d", len(head))
	}
}

// pgUUID builds a deterministic valid pgtype.UUID from a seed byte for tests.
func pgUUID(seed byte) pgtype.UUID {
	var b [16]byte
	b[0] = seed
	return pgtype.UUID{Bytes: b, Valid: true}
}
