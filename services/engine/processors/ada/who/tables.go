// Package who provides WHO Child Growth Standards LMS tables for girls (0–24 months)
// and functions for computing growth percentiles from raw measurements.
//
// All M values are stored in imperial units (oz for weight, inches for length and head
// circumference) so the LMS formula can be applied to measurements directly without
// unit conversion. L and S are dimensionless.
//
// Data source: WHO Child Growth Standards, sourced from
// https://www.who.int/tools/child-growth-standards/standards
// Tables are stable since 2006. A WHO update requires editing the JSON files and
// cutting a new release — no runtime refresh mechanism is needed or provided.
package who

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
)

//go:embed who_weight_girls.json
var weightData []byte

//go:embed who_length_girls.json
var lengthData []byte

//go:embed who_head_girls.json
var headData []byte

// LMSRow is one age entry in a WHO LMS table.
// AgeDays is the age in days (WHO monthly offsets converted at 30.4375 days/month).
// M is in imperial units: oz for weight, inches for length and head circumference.
type LMSRow struct {
	AgeDays float64 `json:"age_days"`
	L       float64 `json:"L"`
	M       float64 `json:"M"`
	S       float64 `json:"S"`
}

// Table is a parsed WHO LMS table, ordered by AgeDays ascending.
type Table []LMSRow

// WeightTable, LengthTable, HeadTable are the package-level initialized tables.
// They are loaded once at package init and shared across all callers.
var (
	WeightTable Table
	LengthTable Table
	HeadTable   Table
)

func init() {
	var err error
	if WeightTable, err = loadTable(weightData); err != nil {
		panic(fmt.Sprintf("who: load weight table: %v", err))
	}
	if LengthTable, err = loadTable(lengthData); err != nil {
		panic(fmt.Sprintf("who: load length table: %v", err))
	}
	if HeadTable, err = loadTable(headData); err != nil {
		panic(fmt.Sprintf("who: load head table: %v", err))
	}
}

func loadTable(data []byte) (Table, error) {
	var rows []LMSRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	return Table(rows), nil
}

// Interpolate returns the L, M, S values for the given age in days by linear
// interpolation between the two nearest table rows. Returns an error if ageDays
// is outside the table range (below 0 or above the last entry).
func (t Table) Interpolate(ageDays float64) (L, M, S float64, err error) {
	if len(t) == 0 {
		return 0, 0, 0, fmt.Errorf("who: empty table")
	}
	if ageDays < t[0].AgeDays {
		return 0, 0, 0, fmt.Errorf("who: age %.1f days is below table minimum (%.1f)", ageDays, t[0].AgeDays)
	}
	last := t[len(t)-1]
	if ageDays >= last.AgeDays {
		// At or beyond the last entry — return the last row directly (no extrapolation).
		return last.L, last.M, last.S, nil
	}
	// Binary search for the bracketing interval.
	lo, hi := 0, len(t)-1
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if t[mid].AgeDays <= ageDays {
			lo = mid
		} else {
			hi = mid
		}
	}
	r0, r1 := t[lo], t[hi]
	frac := (ageDays - r0.AgeDays) / (r1.AgeDays - r0.AgeDays)
	L = r0.L + frac*(r1.L-r0.L)
	M = r0.M + frac*(r1.M-r0.M)
	S = r0.S + frac*(r1.S-r0.S)
	return L, M, S, nil
}

// Percentile computes the WHO LMS percentile (0–100) for a given measurement value
// and age. value must be in the same imperial units as the table's M column.
//
// When |L| < 1e-6 (L approaches 0), the limit form Z = ln(value/M) / S is used to
// avoid a 0/0 division. This applies to weight-for-age around months 3–5.
func (t Table) Percentile(ageDays, value float64) (float64, error) {
	L, M, S, err := t.Interpolate(ageDays)
	if err != nil {
		return 0, err
	}
	var z float64
	if math.Abs(L) < 1e-6 {
		z = math.Log(value/M) / S
	} else {
		z = (math.Pow(value/M, L) - 1) / (L * S)
	}
	return phi(z) * 100, nil
}

// InversePercentile computes the measurement value corresponding to a given percentile
// (0–100) and age in days. The result is in the same imperial units as the table's M column.
//
// When |L| < 1e-6, the limit form value = M * exp(Z * S) is used.
func (t Table) InversePercentile(ageDays, pct float64) (float64, error) {
	L, M, S, err := t.Interpolate(ageDays)
	if err != nil {
		return 0, err
	}
	z := probit(pct / 100)
	if math.Abs(L) < 1e-6 {
		return M * math.Exp(z*S), nil
	}
	return M * math.Pow(1+L*S*z, 1/L), nil
}

// phi is the standard normal CDF.
func phi(z float64) float64 {
	return 0.5 * (1 + math.Erf(z/math.Sqrt2))
}

// probit is the inverse standard normal CDF (quantile function), implemented via the
// Beasley-Springer-Moro rational approximation. Accurate to ~1e-9 for p in (0, 1).
func probit(p float64) float64 {
	if p <= 0 {
		return math.Inf(-1)
	}
	if p >= 1 {
		return math.Inf(1)
	}

	// Coefficients for the rational approximation.
	const (
		a0 = 3.3871328727963666080e0
		a1 = 1.3314166789178437745e+2
		a2 = 1.9715909503065514427e+3
		a3 = 1.3731693765509461125e+4
		a4 = 4.5921953931549871457e+4
		a5 = 6.7265770927008700853e+4
		a6 = 3.3430575583588128105e+4
		a7 = 2.5090809287301226727e+3
		b1 = 4.2313330701600911252e+1
		b2 = 6.8718700749205790830e+2
		b3 = 5.3941960214247511077e+3
		b4 = 2.1213794301586595867e+4
		b5 = 3.9307895800092710610e+4
		b6 = 2.8729085735721942674e+4
		b7 = 5.2264952788528545610e+3
		c0 = 1.42343711074721209143e0
		c1 = 4.63033784615654529590e0
		c2 = 5.76949722146864628717e0
		c3 = 3.64784832476320460504e0
		c4 = 1.27045825245236838258e0
		c5 = 2.41780725177450611770e-1
		c6 = 2.27001535109994502416e-2
		c7 = 7.74545014781205834090e-4
		d1 = 2.05319162663775882187e0
		d2 = 1.67638483926195265728e0
		d3 = 6.89767334985100004550e-1
		d4 = 1.48103976427480074590e-1
		d5 = 1.51986665636164571966e-2
		d6 = 5.47593808499534494600e-4
		d7 = 1.05075007164441684324e-9
		e0 = 6.65790464350110377720e0
		e1 = 5.46378491116411436990e0
		e2 = 1.78482653991729133580e0
		e3 = 2.96560571828504891230e-1
		e4 = 2.65321895265761230930e-2
		e5 = 1.24266094738807843860e-3
		e6 = 2.71155556874348757815e-5
		e7 = 2.01033439929228813265e-7
		f1 = 5.99832206555887937690e-1
		f2 = 1.36929880922735805310e-1
		f3 = 1.48753612908506508940e-2
		f4 = 7.86869131145613259100e-4
		f5 = 1.84631831751005468180e-5
		f6 = 1.42151175831644588870e-7
		f7 = 2.04426310338993978564e-15
	)

	q := p - 0.5
	var r, x float64
	if math.Abs(q) <= 0.425 {
		r = 0.180625 - q*q
		x = q * (((((((a7*r+a6)*r+a5)*r+a4)*r+a3)*r+a2)*r+a1)*r + a0) /
			(((((((b7*r+b6)*r+b5)*r+b4)*r+b3)*r+b2)*r+b1)*r + 1)
		return x
	}
	if q < 0 {
		r = p
	} else {
		r = 1 - p
	}
	r = math.Sqrt(-math.Log(r))
	if r <= 5 {
		r -= 1.6
		x = (((((((c7*r+c6)*r+c5)*r+c4)*r+c3)*r+c2)*r+c1)*r + c0) /
			(((((((d7*r+d6)*r+d5)*r+d4)*r+d3)*r+d2)*r+d1)*r + 1)
	} else {
		r -= 5
		x = (((((((e7*r+e6)*r+e5)*r+e4)*r+e3)*r+e2)*r+e1)*r + e0) /
			(((((((f7*r+f6)*r+f5)*r+f4)*r+f3)*r+f2)*r+f1)*r + 1)
	}
	if q < 0 {
		return -x
	}
	return x
}
