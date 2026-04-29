package ada

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/store"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/who"
)

// ── Growth measurement handler ────────────────────────────────────────────────

func (p *Processor) handleGrowthLogged(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaGrowthLoggedData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode growth_logged: %w", err)
	}

	if d.WeightOz == nil && d.LengthIn == nil && d.HeadCircumferenceIn == nil {
		p.log.Warn("ada: growth_logged event has no measurements — ignoring")
		return nil
	}

	measuredAt, err := parseRFC3339(d.Timestamp)
	if err != nil {
		return fmt.Errorf("ada: parse growth timestamp: %w", err)
	}

	// Advisory range checks — log and continue, never reject.
	if d.WeightOz != nil && (*d.WeightOz < 4 || *d.WeightOz > 480) {
		p.log.Warn("ada: growth weight outside advisory range",
			slog.Float64("weight_oz", *d.WeightOz),
			slog.String("range", "4–480 oz"))
	}
	if d.LengthIn != nil && (*d.LengthIn < 14 || *d.LengthIn > 40) {
		p.log.Warn("ada: growth length outside advisory range",
			slog.Float64("length_in", *d.LengthIn),
			slog.String("range", "14–40 in"))
	}
	if d.HeadCircumferenceIn != nil && (*d.HeadCircumferenceIn < 10 || *d.HeadCircumferenceIn > 20) {
		p.log.Warn("ada: growth head circumference outside advisory range",
			slog.Float64("head_in", *d.HeadCircumferenceIn),
			slog.String("range", "10–20 in"))
	}

	// Compute percentiles from birth date. Any error or missing profile is a soft-fail —
	// the measurement is still useful without a percentile.
	var weightPct, lengthPct, headPct *float64
	profile, err := p.q.GetProfile(ctx)
	if err != nil {
		p.log.Warn("ada: get profile for growth percentile — storing without percentile",
			slog.String("error", err.Error()))
	} else {
		ageDays := measuredAt.Sub(profile.BirthAt.Time).Hours() / 24
		switch {
		case ageDays < 0:
			p.log.Warn("ada: growth measured_at is before birth date — skipping percentile",
				slog.Float64("age_days", ageDays))
		case ageDays > 730:
			p.log.Warn("ada: age exceeds WHO table range (730 days) — skipping percentile",
				slog.Float64("age_days", ageDays))
		default:
			weightPct = computePct(p.log, who.WeightTable, ageDays, d.WeightOz, "weight")
			lengthPct = computePct(p.log, who.LengthTable, ageDays, d.LengthIn, "length")
			headPct = computePct(p.log, who.HeadTable, ageDays, d.HeadCircumferenceIn, "head")
		}
	}

	if _, err := p.q.InsertGrowthMeasurement(ctx, &store.InsertGrowthMeasurementParams{
		MeasuredAt:          toTimestamptz(measuredAt),
		WeightOz:            numericFromFloatPtr(d.WeightOz),
		LengthIn:            numericFromFloatPtr(d.LengthIn),
		HeadCircumferenceIn: numericFromFloatPtr(d.HeadCircumferenceIn),
		Source:              d.Source,
		WeightPct:           numericFromFloatPtr(weightPct),
		LengthPct:           numericFromFloatPtr(lengthPct),
		HeadPct:             numericFromFloatPtr(headPct),
		LoggedBy:            d.LoggedBy,
	}); err != nil {
		return fmt.Errorf("ada: insert growth measurement: %w", err)
	}

	p.pushGrowthSensors(ctx)
	return nil
}

// computePct computes the WHO percentile for a single measurement value.
// Returns nil if the value is nil or if computation fails (logs warning).
func computePct(log *slog.Logger, table who.Table, ageDays float64, value *float64, name string) *float64 {
	if value == nil {
		return nil
	}
	pct, err := table.Percentile(ageDays, *value)
	if err != nil {
		log.Warn("ada: growth percentile computation failed",
			slog.String("measurement", name),
			slog.String("error", err.Error()))
		return nil
	}
	rounded := math.Round(pct*10) / 10
	return &rounded
}

// ── Growth sensor push ────────────────────────────────────────────────────────

// pushGrowthSensors pushes the three latest-measurement sensors and the full history.
// Called on every new growth event and from refreshAllSensors.
func (p *Processor) pushGrowthSensors(ctx context.Context) {
	p.pushLatestWeight(ctx)
	p.pushLatestLength(ctx)
	p.pushLatestHeadCircumference(ctx)
	p.pushGrowthHistory(ctx)
}

func (p *Processor) pushLatestWeight(ctx context.Context) {
	row, err := p.q.GetLatestWeight(ctx)
	if err != nil {
		if !isNoRows(err) {
			p.log.Warn("ada: get latest weight", slog.String("error", err.Error()))
		}
		return
	}
	oz := numericToFloat(row.WeightOz)
	attrs := map[string]any{
		"weight_oz":   oz,
		"measured_at": row.MeasuredAt.Time.UTC().Format(time.RFC3339),
		"source":      row.Source,
	}
	if pct, ok := numericToFloatOk(row.WeightPct); ok {
		attrs["percentile"] = pct
	}
	if err := p.ha.PushState(ctx, sensorLatestWeight,
		strconv.FormatFloat(oz, 'f', 2, 64), attrs); err != nil {
		p.log.Warn("ada: push latest weight", slog.String("error", err.Error()))
	}
}

func (p *Processor) pushLatestLength(ctx context.Context) {
	row, err := p.q.GetLatestLength(ctx)
	if err != nil {
		if !isNoRows(err) {
			p.log.Warn("ada: get latest length", slog.String("error", err.Error()))
		}
		return
	}
	in := numericToFloat(row.LengthIn)
	attrs := map[string]any{
		"length_in":   in,
		"measured_at": row.MeasuredAt.Time.UTC().Format(time.RFC3339),
		"source":      row.Source,
	}
	if pct, ok := numericToFloatOk(row.LengthPct); ok {
		attrs["percentile"] = pct
	}
	if err := p.ha.PushState(ctx, sensorLatestLength,
		strconv.FormatFloat(in, 'f', 2, 64), attrs); err != nil {
		p.log.Warn("ada: push latest length", slog.String("error", err.Error()))
	}
}

func (p *Processor) pushLatestHeadCircumference(ctx context.Context) {
	row, err := p.q.GetLatestHeadCircumference(ctx)
	if err != nil {
		if !isNoRows(err) {
			p.log.Warn("ada: get latest head circumference", slog.String("error", err.Error()))
		}
		return
	}
	in := numericToFloat(row.HeadCircumferenceIn)
	attrs := map[string]any{
		"head_circumference_in": in,
		"measured_at":           row.MeasuredAt.Time.UTC().Format(time.RFC3339),
		"source":                row.Source,
	}
	if pct, ok := numericToFloatOk(row.HeadPct); ok {
		attrs["percentile"] = pct
	}
	if err := p.ha.PushState(ctx, sensorLatestHeadCircumference,
		strconv.FormatFloat(in, 'f', 2, 64), attrs); err != nil {
		p.log.Warn("ada: push latest head circumference", slog.String("error", err.Error()))
	}
}

// ── Growth history sensor ─────────────────────────────────────────────────────

type growthWeightEntry struct {
	ID         string   `json:"id"`
	MeasuredAt string   `json:"measured_at"`
	WeightOz   float64  `json:"weight_oz"`
	Percentile *float64 `json:"percentile,omitempty"`
	Source     string   `json:"source"`
}

type growthLengthEntry struct {
	ID         string   `json:"id"`
	MeasuredAt string   `json:"measured_at"`
	LengthIn   float64  `json:"length_in"`
	Percentile *float64 `json:"percentile,omitempty"`
	Source     string   `json:"source"`
}

type growthHeadEntry struct {
	ID                  string   `json:"id"`
	MeasuredAt          string   `json:"measured_at"`
	HeadCircumferenceIn float64  `json:"head_circumference_in"`
	Percentile          *float64 `json:"percentile,omitempty"`
	Source              string   `json:"source"`
}

// pushGrowthHistory queries all growth measurements and pushes them as three separate
// descending arrays (weight, length, head) on sensor.ada_growth_history.
func (p *Processor) pushGrowthHistory(ctx context.Context) {
	rows, err := p.q.GetAllGrowthMeasurements(ctx)
	if err != nil {
		p.log.Warn("ada: query growth history", slog.String("error", err.Error()))
		return
	}

	weightEntries := make([]growthWeightEntry, 0)
	lengthEntries := make([]growthLengthEntry, 0)
	headEntries := make([]growthHeadEntry, 0)

	for _, r := range rows {
		id := uuid.UUID(r.ID.Bytes).String()
		ts := r.MeasuredAt.Time.UTC().Format(time.RFC3339)

		if r.WeightOz.Valid {
			e := growthWeightEntry{
				ID:         id,
				MeasuredAt: ts,
				WeightOz:   numericToFloat(r.WeightOz),
				Source:     r.Source,
			}
			if pct, ok := numericToFloatOk(r.WeightPct); ok {
				e.Percentile = &pct
			}
			weightEntries = append(weightEntries, e)
		}
		if r.LengthIn.Valid {
			e := growthLengthEntry{
				ID:         id,
				MeasuredAt: ts,
				LengthIn:   numericToFloat(r.LengthIn),
				Source:     r.Source,
			}
			if pct, ok := numericToFloatOk(r.LengthPct); ok {
				e.Percentile = &pct
			}
			lengthEntries = append(lengthEntries, e)
		}
		if r.HeadCircumferenceIn.Valid {
			e := growthHeadEntry{
				ID:                  id,
				MeasuredAt:          ts,
				HeadCircumferenceIn: numericToFloat(r.HeadCircumferenceIn),
				Source:              r.Source,
			}
			if pct, ok := numericToFloatOk(r.HeadPct); ok {
				e.Percentile = &pct
			}
			headEntries = append(headEntries, e)
		}
	}

	attrs := map[string]any{
		"weight":       weightEntries,
		"length":       lengthEntries,
		"head":         headEntries,
		"last_updated": time.Now().UTC().Format(time.RFC3339),
	}
	if err := p.ha.PushState(ctx, sensorGrowthHistory,
		strconv.Itoa(len(rows)), attrs); err != nil {
		p.log.Warn("ada: push growth history", slog.String("error", err.Error()))
	}
}

// ── WHO curve data sensor ─────────────────────────────────────────────────────

type growthCurveMeasurement struct {
	LMS   []lmsEntry              `json:"lms"`
	Bands map[string][][2]float64 `json:"bands"`
}

type lmsEntry struct {
	AgeDays float64 `json:"age_days"`
	L       float64 `json:"L"`
	M       float64 `json:"M"`
	S       float64 `json:"S"`
}

// pushGrowthCurves precomputes WHO percentile band values and pushes both the raw LMS
// tables and the precomputed bands to sensor.ada_growth_curves. Called from
// refreshAllSensors on startup, HA reconnect, and the 4-hour safety net.
func (p *Processor) pushGrowthCurves(ctx context.Context) {
	samplePoints := buildSamplePoints()
	percentiles := []struct {
		key string
		pct float64
	}{
		{"p3", 3}, {"p15", 15}, {"p50", 50}, {"p85", 85}, {"p97", 97},
	}

	buildMeasurement := func(table who.Table) growthCurveMeasurement {
		// Raw LMS rows from the table.
		lms := make([]lmsEntry, len(table))
		for i, row := range table {
			lms[i] = lmsEntry{
				AgeDays: row.AgeDays,
				L:       row.L,
				M:       row.M,
				S:       row.S,
			}
		}

		// Precomputed band values at each sample point.
		bands := make(map[string][][2]float64, len(percentiles))
		for _, p := range percentiles {
			points := make([][2]float64, 0, len(samplePoints))
			for _, ageDays := range samplePoints {
				val, err := table.InversePercentile(ageDays, p.pct)
				if err != nil {
					continue
				}
				points = append(points, [2]float64{
					ageDays,
					math.Round(val*100) / 100,
				})
			}
			bands[p.key] = points
		}
		return growthCurveMeasurement{LMS: lms, Bands: bands}
	}

	payload := map[string]growthCurveMeasurement{
		"weight": buildMeasurement(who.WeightTable),
		"length": buildMeasurement(who.LengthTable),
		"head":   buildMeasurement(who.HeadTable),
	}

	// Marshal to map[string]any for PushState attributes.
	b, err := json.Marshal(payload)
	if err != nil {
		p.log.Warn("ada: marshal growth curves", slog.String("error", err.Error()))
		return
	}
	var attrs map[string]any
	if err := json.Unmarshal(b, &attrs); err != nil {
		p.log.Warn("ada: unmarshal growth curves to attrs", slog.String("error", err.Error()))
		return
	}

	if err := p.ha.PushState(ctx, sensorGrowthCurves, "ok", attrs); err != nil {
		p.log.Warn("ada: push growth curves", slog.String("error", err.Error()))
	}
}

// buildSamplePoints returns the age-in-days sample points for band curves:
// 7-day intervals for days 0–91 (weeks 0–13), then 30-day intervals from 91–730.
func buildSamplePoints() []float64 {
	var pts []float64
	for d := 0; d <= 91; d += 7 {
		pts = append(pts, float64(d))
	}
	for d := 91 + 30; d <= 730; d += 30 {
		pts = append(pts, float64(d))
	}
	return pts
}

// ── pgtype helpers ────────────────────────────────────────────────────────────

// numericToFloat converts a pgtype.Numeric to float64, returning 0 for invalid/NULL.
// Uses Float64Value() — n.Scan(&f) fills n FROM f, not the reverse.
func numericToFloat(n pgtype.Numeric) float64 {
	if !n.Valid {
		return 0
	}
	f8, err := n.Float64Value()
	if err != nil || !f8.Valid {
		return 0
	}
	return f8.Float64
}

// numericToFloatOk converts a pgtype.Numeric to float64, reporting whether the value
// was non-NULL. Used for optional sensor attributes (percentile, oz amounts).
func numericToFloatOk(n pgtype.Numeric) (float64, bool) {
	if !n.Valid {
		return 0, false
	}
	f8, err := n.Float64Value()
	if err != nil || !f8.Valid {
		return 0, false
	}
	return math.Round(f8.Float64*10) / 10, true
}

// isNoRows reports whether an error is a pgx "no rows" sentinel.
func isNoRows(err error) bool {
	return err != nil && err.Error() == pgx.ErrNoRows.Error()
}
