//go:build fast

package ada

import "testing"

func TestDeriveFeedingSource(t *testing.T) {
	cases := []struct {
		name                string
		left, right         int
		breastMilk, formula float64
		want                string
	}{
		{"left only", 300, 0, 0, 0, "breast_left"},
		{"right only", 0, 300, 0, 0, "breast_right"},
		{"both sides", 300, 300, 0, 0, "breast"},
		{"mixed bottle", 0, 0, 2, 3, "mixed"},
		{"breast-milk bottle", 0, 0, 2, 0, "bottle_breast"},
		{"formula bottle", 0, 0, 0, 3, "bottle_formula"},
		{"empty falls back to formula", 0, 0, 0, 0, "bottle_formula"},
		{"breast takes precedence over bottle", 300, 0, 2, 1, "breast_left"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := deriveFeedingSource(c.left, c.right, c.breastMilk, c.formula)
			if got != c.want {
				t.Errorf("deriveFeedingSource(%d,%d,%v,%v) = %q, want %q",
					c.left, c.right, c.breastMilk, c.formula, got, c.want)
			}
		})
	}
}

func TestParseUUID(t *testing.T) {
	if _, err := parseUUID("not-a-uuid"); err == nil {
		t.Error("expected error for invalid uuid")
	}
	u, err := parseUUID("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !u.Valid {
		t.Error("expected valid pgtype.UUID")
	}
}
