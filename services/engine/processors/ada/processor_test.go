//go:build fast

package ada

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/store"
)

// ── normalizeSource ───────────────────────────────────────────────────────────

func TestNormalizeSource(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"formula", "bottle_formula"},
		{"breast_milk", "bottle_breast"},
		// canonical values pass through unchanged
		{"bottle_formula", "bottle_formula"},
		{"bottle_breast", "bottle_breast"},
		{"mixed", "mixed"},
		{"breast_left", "breast_left"},
		{"breast_right", "breast_right"},
		{"breast", "breast"},
	}
	for _, c := range cases {
		got := normalizeSource(c.in)
		if got != c.want {
			t.Errorf("normalizeSource(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── isBottleSource ────────────────────────────────────────────────────────────

func TestIsBottleSource(t *testing.T) {
	bottles := []string{
		"bottle_formula", "bottle_breast", "mixed",
		"formula", "breast_milk", // legacy dashboard values
	}
	for _, s := range bottles {
		if !isBottleSource(s) {
			t.Errorf("isBottleSource(%q) = false, want true", s)
		}
	}

	notBottles := []string{"breast", "breast_left", "breast_right", "unknown", ""}
	for _, s := range notBottles {
		if isBottleSource(s) {
			t.Errorf("isBottleSource(%q) = true, want false", s)
		}
	}
}

// ── feedingDisplaySource ──────────────────────────────────────────────────────

func TestFeedingDisplaySource(t *testing.T) {
	cases := []struct {
		source          string
		hasBottleDetail bool
		want            string
	}{
		// breast feeds — no supplement
		{"breast", false, "breast"},
		{"breast_left", false, "breast"},
		{"breast_right", false, "breast"},
		// breast feeds — with supplement
		{"breast", true, "supplemented"},
		{"breast_left", true, "supplemented"},
		{"breast_right", true, "supplemented"},
		// canonical bottle values
		{"bottle_formula", false, "formula"},
		{"bottle_formula", true, "formula"},
		{"bottle_breast", false, "breast milk"},
		{"bottle_breast", true, "breast milk"},
		// legacy dashboard values
		{"formula", false, "formula"},
		{"formula", true, "formula"},
		{"breast_milk", false, "breast milk"},  // was incorrectly returning "breast" before fix
		{"breast_milk", true, "breast milk"},
		// mixed
		{"mixed", false, "mixed"},
		{"mixed", true, "mixed"},
		// unknown passthrough
		{"unknown", false, "unknown"},
	}
	for _, c := range cases {
		got := feedingDisplaySource(c.source, c.hasBottleDetail)
		if got != c.want {
			t.Errorf("feedingDisplaySource(%q, %v) = %q, want %q",
				c.source, c.hasBottleDetail, got, c.want)
		}
	}
}

// ── buildFeedingHistory ───────────────────────────────────────────────────────

func mustUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		panic(err)
	}
	return u
}

func mustTimestamptz(s string) pgtype.Timestamptz {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func TestBuildFeedingHistory_BreastOnly(t *testing.T) {
	rows := []*store.GetLast24hFeedingsRow{
		{
			ID:             mustUUID("aaaaaaaa-0000-0000-0000-000000000001"),
			Timestamp:      mustTimestamptz("2026-04-01T12:00:00Z"),
			Source:         "breast_left",
			AmountOz:       0,
			BreastMilkOz:   0,
			FormulaOz:      0,
			LeftDurationS:  300,
			RightDurationS: 0,
		},
	}
	entries := buildFeedingHistory(rows)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Source != "breast" {
		t.Errorf("Source = %q, want %q", e.Source, "breast")
	}
	if e.LeftDurationS != 300 {
		t.Errorf("LeftDurationS = %d, want 300", e.LeftDurationS)
	}
	if e.AmountOz != nil || e.BreastMilkOz != nil || e.FormulaOz != nil {
		t.Error("oz fields should be nil for breast-only feed")
	}
}

func TestBuildFeedingHistory_FormulaBottle(t *testing.T) {
	rows := []*store.GetLast24hFeedingsRow{
		{
			ID:             mustUUID("aaaaaaaa-0000-0000-0000-000000000002"),
			Timestamp:      mustTimestamptz("2026-04-01T13:00:00Z"),
			Source:         "bottle_formula",
			AmountOz:       3.0,
			BreastMilkOz:   0,
			FormulaOz:      0,
			LeftDurationS:  0,
			RightDurationS: 0,
		},
	}
	entries := buildFeedingHistory(rows)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Source != "formula" {
		t.Errorf("Source = %q, want %q", e.Source, "formula")
	}
	if e.AmountOz == nil || *e.AmountOz != 3.0 {
		t.Errorf("AmountOz = %v, want 3.0", e.AmountOz)
	}
	if e.BreastMilkOz != nil || e.FormulaOz != nil {
		t.Error("BreastMilkOz and FormulaOz should be nil for single-source bottle")
	}
}

func TestBuildFeedingHistory_LegacyFormulaSource(t *testing.T) {
	// Legacy rows in DB have source="formula" (dashboard display value).
	// buildFeedingHistory must still display correctly.
	rows := []*store.GetLast24hFeedingsRow{
		{
			ID:        mustUUID("aaaaaaaa-0000-0000-0000-000000000003"),
			Timestamp: mustTimestamptz("2026-04-01T14:00:00Z"),
			Source:    "formula", // legacy DB value
			AmountOz:  0,         // oz data was lost for these rows
		},
	}
	entries := buildFeedingHistory(rows)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Source != "formula" {
		t.Errorf("Source = %q, want %q", entries[0].Source, "formula")
	}
}

func TestBuildFeedingHistory_LegacyBreastMilkSource(t *testing.T) {
	// Legacy rows with source="breast_milk" must not be misclassified as breast feeds.
	rows := []*store.GetLast24hFeedingsRow{
		{
			ID:        mustUUID("aaaaaaaa-0000-0000-0000-000000000004"),
			Timestamp: mustTimestamptz("2026-04-01T15:00:00Z"),
			Source:    "breast_milk", // legacy DB value
			AmountOz:  0,
		},
	}
	entries := buildFeedingHistory(rows)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Source != "breast milk" {
		t.Errorf("Source = %q, want %q", entries[0].Source, "breast milk")
	}
}

func TestBuildFeedingHistory_Supplemented(t *testing.T) {
	// Breast feed with supplement: has breast source and non-zero oz from bottle detail.
	rows := []*store.GetLast24hFeedingsRow{
		{
			ID:             mustUUID("aaaaaaaa-0000-0000-0000-000000000005"),
			Timestamp:      mustTimestamptz("2026-04-01T16:00:00Z"),
			Source:         "breast_right",
			AmountOz:       2.0, // supplement oz
			LeftDurationS:  0,
			RightDurationS: 480,
		},
	}
	entries := buildFeedingHistory(rows)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Source != "supplemented" {
		t.Errorf("Source = %q, want %q", e.Source, "supplemented")
	}
	if e.AmountOz == nil || *e.AmountOz != 2.0 {
		t.Errorf("AmountOz = %v, want 2.0", e.AmountOz)
	}
}

func TestBuildFeedingHistory_MixedBottle(t *testing.T) {
	rows := []*store.GetLast24hFeedingsRow{
		{
			ID:           mustUUID("aaaaaaaa-0000-0000-0000-000000000006"),
			Timestamp:    mustTimestamptz("2026-04-01T17:00:00Z"),
			Source:       "mixed",
			AmountOz:     0,
			BreastMilkOz: 2.0,
			FormulaOz:    1.0,
		},
	}
	entries := buildFeedingHistory(rows)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Source != "mixed" {
		t.Errorf("Source = %q, want %q", e.Source, "mixed")
	}
	if e.AmountOz != nil {
		t.Error("AmountOz should be nil for mixed bottle (use BreastMilkOz/FormulaOz)")
	}
	if e.BreastMilkOz == nil || *e.BreastMilkOz != 2.0 {
		t.Errorf("BreastMilkOz = %v, want 2.0", e.BreastMilkOz)
	}
	if e.FormulaOz == nil || *e.FormulaOz != 1.0 {
		t.Errorf("FormulaOz = %v, want 1.0", e.FormulaOz)
	}
}
