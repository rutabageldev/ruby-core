//go:build fast

package who

import (
	"math"
	"testing"
)

// WHO reference values for spot-checks (girls, imperial units).
// Weight in oz, length/head in inches.
const (
	weightBirthMedianOz  = 114.0125 // 3.2322 kg
	lengthBirthMedianIn  = 19.3495  // 49.1477 cm
	headBirthMedianIn    = 13.3381  // 33.8787 cm
	weight12moMedianOz   = 315.6349 // ~8.952 kg at 12 months
)

func TestPercentile_MedianEquals50(t *testing.T) {
	// At any age, a measurement equal to M should produce exactly the 50th percentile.
	cases := []struct {
		name    string
		table   Table
		ageDays float64
	}{
		{"weight birth", WeightTable, 0},
		{"weight 12mo", WeightTable, 365.2},
		{"length birth", LengthTable, 0},
		{"head birth", HeadTable, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, M, _, err := tc.table.Interpolate(tc.ageDays)
			if err != nil {
				t.Fatalf("Interpolate: %v", err)
			}
			pct, err := tc.table.Percentile(tc.ageDays, M)
			if err != nil {
				t.Fatalf("Percentile: %v", err)
			}
			if math.Abs(pct-50) > 0.01 {
				t.Errorf("expected ~50th percentile for median value, got %.4f", pct)
			}
		})
	}
}

func TestPercentile_KnownValues(t *testing.T) {
	// Spot-check against WHO reference percentile values (±0.5% tolerance).
	// Reference: WHO girls wfa 0-5 years, SD columns (SD2neg ≈ 2.3rd pct, SD2 ≈ 97.7th).
	cases := []struct {
		name    string
		table   Table
		ageDays float64
		valueOz float64
		wantPct float64
		tol     float64
	}{
		// Weight at birth: SD0 (median) = 3.2322 kg = 114.0125 oz → 50th
		{"weight birth median", WeightTable, 0, weightBirthMedianOz, 50, 0.5},
		// Weight at birth: WHO SD2neg = 2.4 kg = 84.66 oz → ~2.3rd pct
		{"weight birth -2SD", WeightTable, 0, 2.4 * 35.27396, 2.3, 1.0},
		// Weight at birth: WHO SD2 = 4.2 kg = 148.15 oz → ~97.7th pct
		{"weight birth +2SD", WeightTable, 0, 4.2 * 35.27396, 97.7, 1.0},
		// Length at birth: median = 49.1477 cm = 19.3495 in → 50th
		{"length birth median", LengthTable, 0, lengthBirthMedianIn, 50, 0.5},
		// Head at birth: median → 50th
		{"head birth median", HeadTable, 0, headBirthMedianIn, 50, 0.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pct, err := tc.table.Percentile(tc.ageDays, tc.valueOz)
			if err != nil {
				t.Fatalf("Percentile: %v", err)
			}
			if math.Abs(pct-tc.wantPct) > tc.tol {
				t.Errorf("got %.3f%%, want %.3f%% (±%.1f%%)", pct, tc.wantPct, tc.tol)
			}
		})
	}
}

func TestPercentile_LNearZero(t *testing.T) {
	// Weight-for-age L crosses zero around month 4. Percentile must not panic or return NaN.
	// Month 4 ≈ 121.75 days; L ≈ -0.005.
	_, M, _, err := WeightTable.Interpolate(121.75)
	if err != nil {
		t.Fatalf("Interpolate: %v", err)
	}
	pct, err := WeightTable.Percentile(121.75, M)
	if err != nil {
		t.Fatalf("Percentile at L≈0: %v", err)
	}
	if math.IsNaN(pct) || math.IsInf(pct, 0) {
		t.Errorf("expected finite percentile near L=0, got %v", pct)
	}
	if math.Abs(pct-50) > 0.5 {
		t.Errorf("median at L≈0 should be ~50th, got %.4f", pct)
	}
}

func TestInterpolate_OutsideRange(t *testing.T) {
	// Below minimum age returns error.
	_, _, _, err := WeightTable.Interpolate(-1)
	if err == nil {
		t.Error("expected error for negative age")
	}

	// At or beyond the last entry returns last row (no extrapolation error).
	L, M, S, err := WeightTable.Interpolate(9999)
	if err != nil {
		t.Errorf("unexpected error beyond table range: %v", err)
	}
	if L == 0 && M == 0 && S == 0 {
		t.Error("expected non-zero L/M/S for beyond-range age")
	}
}

func TestInversePercentile_RoundTrip(t *testing.T) {
	// InversePercentile(Percentile(v)) should round-trip to the original value.
	tables := []struct {
		name  string
		table Table
	}{
		{"weight", WeightTable},
		{"length", LengthTable},
		{"head", HeadTable},
	}
	ageDays := 182.5 // ~6 months
	pcts := []float64{3, 15, 50, 85, 97}

	for _, tc := range tables {
		_, M, _, _ := tc.table.Interpolate(ageDays)
		for _, pct := range pcts {
			v, err := tc.table.InversePercentile(ageDays, pct)
			if err != nil {
				t.Fatalf("%s InversePercentile(%v): %v", tc.name, pct, err)
			}
			got, err := tc.table.Percentile(ageDays, v)
			if err != nil {
				t.Fatalf("%s Percentile round-trip: %v", tc.name, err)
			}
			if math.Abs(got-pct) > 0.1 {
				t.Errorf("%s: round-trip at %.0fth pct: got %.4f (value=%.4f, M=%.4f)",
					tc.name, pct, got, v, M)
			}
		}
	}
}

func TestProbit_Symmetry(t *testing.T) {
	// probit(p) == -probit(1-p)
	pairs := [][2]float64{{0.01, 0.99}, {0.05, 0.95}, {0.25, 0.75}, {0.5, 0.5}}
	for _, p := range pairs {
		a, b := probit(p[0]), probit(p[1])
		if math.Abs(a+b) > 1e-9 {
			t.Errorf("probit symmetry failed: probit(%.2f)=%.6f, probit(%.2f)=%.6f", p[0], a, p[1], b)
		}
	}
}

func TestProbit_KnownValues(t *testing.T) {
	cases := []struct{ p, want float64 }{
		{0.5, 0},
		{0.8413, 1.0},  // phi(1) ≈ 0.8413
		{0.9772, 2.0},  // phi(2) ≈ 0.9772
		{0.1587, -1.0}, // phi(-1) ≈ 0.1587
	}
	for _, tc := range cases {
		got := probit(tc.p)
		if math.Abs(got-tc.want) > 0.001 {
			t.Errorf("probit(%.4f): got %.6f, want %.6f", tc.p, got, tc.want)
		}
	}
}
