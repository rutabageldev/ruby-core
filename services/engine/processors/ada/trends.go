package ada

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/store"
)

// Trends aggregation (#82/#161, ADR-0032). The dashboard fires ada.trends.query and reads
// the bucketed result back from sensor.ada_trends, matched by request_id. Windows are
// calendar-anchored at local midnight (week = Sun–Sat, month = Sun–Sat weeks clipped to
// the calendar month, year = 12 calendar months) and navigable via offset ≤ 0; window
// math lives in trends_window.go. Note the deliberate divergence from the Today view's
// bedtime rollover (ADR-0043) — see ADR-0032 §5.

const sensorTrends = "sensor.ada_trends"

// bucketWindow is one [start, end) bucket with its display label.
type bucketWindow struct {
	start time.Time
	end   time.Time
	label string
}

// trendEvent is one row's additive contribution to one or more segments at a point in time.
type trendEvent struct {
	when time.Time
	segs map[string]float64
}

// trendBucket is the response shape per bucket (mirrors TrendData).
type trendBucket struct {
	Segs  map[string]float64 `json:"segs"`
	Total float64            `json:"total"`
	Label string             `json:"label"`
}

// trendResponse is the full payload published to sensor.ada_trends (mirrors TrendData).
type trendResponse struct {
	RequestID   string             `json:"request_id"`
	Metric      string             `json:"metric"`
	View        string             `json:"view"`
	Period      string             `json:"period"`
	GeneratedAt string             `json:"generated_at"`
	Buckets     []trendBucket      `json:"buckets"`
	Totals      map[string]float64 `json:"totals"`
	Grand       float64            `json:"grand"`
	PrevGrand   float64            `json:"prevGrand"`
	// Window metadata (#161, ADR-0032 §7). WindowEnd is the inclusive last calendar day.
	WindowStart string `json:"window_start"`
	WindowEnd   string `json:"window_end"`
	DaysElapsed int    `json:"days_elapsed"`
	Offset      int    `json:"offset"`
	MinOffset   *int   `json:"min_offset,omitempty"`
}

// bucketLabel renders a bucket's display label from its start. The dashboard may relabel;
// these are reasonable defaults to confirm against the buildTrend() mock.
func bucketLabel(period string, start time.Time) string {
	switch period {
	case "week":
		return start.Format("Mon")
	case "month":
		return start.Format("1/2")
	case "year":
		return start.Format("Jan")
	}
	return start.Format("1/2")
}

// aggregateTrend assigns events to buckets and computes per-segment totals, the grand
// total (requested window), and prevGrand (events in the explicit [prevStart, prevEnd)
// comparison window, which the caller may truncate for like-for-like partial-period
// deltas — ADR-0032 §8). Events matching neither the prev window nor a bucket are
// dropped: for the current period that is post-truncation comparison spill; for
// navigated (offset < 0) windows it is everything fetched after windowEnd. Values are
// rounded for display.
func aggregateTrend(events []trendEvent, buckets []bucketWindow, segKeys []string, prevStart, prevEnd time.Time) (out []trendBucket, totals map[string]float64, grand, prevGrand float64) {
	out = make([]trendBucket, len(buckets))
	totals = make(map[string]float64, len(segKeys))
	for _, k := range segKeys {
		totals[k] = 0
	}
	for i, b := range buckets {
		segs := make(map[string]float64, len(segKeys))
		for _, k := range segKeys {
			segs[k] = 0
		}
		out[i] = trendBucket{Segs: segs, Label: b.label}
	}
	if len(buckets) == 0 {
		return out, totals, 0, 0
	}

	for _, e := range events {
		if !e.when.Before(prevStart) && e.when.Before(prevEnd) {
			for _, v := range e.segs {
				prevGrand += v
			}
			continue
		}
		for i := range buckets {
			if !e.when.Before(buckets[i].start) && e.when.Before(buckets[i].end) {
				for k, v := range e.segs {
					out[i].Segs[k] += v
					out[i].Total += v
					totals[k] += v
					grand += v
				}
				break
			}
		}
	}

	for i := range out {
		for k := range out[i].Segs {
			out[i].Segs[k] = round1(out[i].Segs[k])
		}
		out[i].Total = round1(out[i].Total)
	}
	for k := range totals {
		totals[k] = round1(totals[k])
	}
	return out, totals, round1(grand), round1(prevGrand)
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

// trendSegKeys returns the stacked-segment keys for a (metric, view), or ok=false if the
// pair is not supported. Keys are the consumer's verbatim identifiers (#82).
func trendSegKeys(metric, view string) ([]string, bool) {
	switch metric {
	case "diapers":
		if view == "count" {
			return []string{"wet", "dirty", "mixed"}, true
		}
	case "feeding":
		switch view {
		case "breast":
			return []string{"left", "right"}, true
		case "bottle":
			return []string{"milk", "formula"}, true
		case "feeds":
			return []string{"bf", "bo"}, true
		}
	case "sleep":
		switch view {
		case "hours":
			return []string{"night", "nap"}, true
		case "wakeups":
			return []string{"wakeups"}, true
		}
	case "tummy":
		switch view {
		case "min":
			return []string{"min"}, true
		case "sessions":
			return []string{"sessions"}, true
		}
	}
	return nil, false
}

// ── Handler ─────────────────────────────────────────────────────────────────────

func (p *Processor) handleTrendsQuery(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaTrendsQueryData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode trends_query: %w", err)
	}

	offset := normalizeOffset(d.Offset)
	if d.Offset > 0 {
		p.log.Warn("ada: trends query offset > 0 clamped to current period",
			slog.Int("offset", d.Offset), slog.String("request_id", d.RequestID))
	}

	now := time.Now()
	resp := trendResponse{
		RequestID:   d.RequestID,
		Metric:      d.Metric,
		View:        d.View,
		Period:      d.Period,
		GeneratedAt: now.UTC().Format(time.RFC3339),
		Buckets:     []trendBucket{},
		Totals:      map[string]float64{},
		Offset:      offset,
	}

	segKeys, okSeg := trendSegKeys(d.Metric, d.View)
	winStart, winEnd, okPeriod := calendarWindow(d.Period, offset, now)
	if !okSeg || !okPeriod {
		p.log.Warn("ada: trends query unsupported — returning empty result",
			slog.String("metric", d.Metric), slog.String("view", d.View), slog.String("period", d.Period))
		p.pushTrends(ctx, resp) // echo an empty response so the dashboard does not hang
		return nil
	}

	buckets := calendarBuckets(d.Period, winStart, winEnd)
	daysElapsed := daysElapsedIn(winStart, winEnd, now, offset)

	// Previous period for the delta; truncated to days_elapsed for the current partial
	// period so the comparison is like-for-like (ADR-0032 §8). The clamp keeps the
	// comparison at the full previous period when it is shorter than days_elapsed
	// (e.g. 30 days into March vs. February).
	prevStart, prevEnd, _ := calendarWindow(d.Period, offset-1, now)
	prevCmpEnd := prevEnd
	if offset == 0 {
		if t := prevStart.AddDate(0, 0, daysElapsed); t.Before(prevEnd) {
			prevCmpEnd = t
		}
	}

	events, err := p.trendEvents(ctx, d.Metric, d.View, prevStart, now.Location())
	if err != nil {
		return fmt.Errorf("ada: load trends events: %w", err)
	}

	resp.Buckets, resp.Totals, resp.Grand, resp.PrevGrand = aggregateTrend(events, buckets, segKeys, prevStart, prevCmpEnd)
	resp.WindowStart = winStart.Format("2006-01-02")
	resp.WindowEnd = winEnd.AddDate(0, 0, -1).Format("2006-01-02")
	resp.DaysElapsed = daysElapsed

	// min_offset (ADR-0032 §9): omitted when no profile exists — never an error.
	if prof, err := p.q.GetProfile(ctx); err == nil && prof.BirthAt.Valid {
		mo := minOffsetFor(d.Period, prof.BirthAt.Time, now)
		resp.MinOffset = &mo
	}

	p.pushTrends(ctx, resp)
	return nil
}

// trendEvents fetches the metric's rows since prevStart (reusing the rolling-window
// queries, which have no upper bound and therefore reach to now — wasteful for
// navigated windows but harmless at this data volume; aggregateTrend drops the spill)
// and maps them to additive trend events. loc is the calendar location for wakeup
// night grouping.
func (p *Processor) trendEvents(ctx context.Context, metric, view string, prevStart time.Time, loc *time.Location) ([]trendEvent, error) {
	since := toTimestamptz(prevStart)
	switch metric {
	case "diapers":
		rows, err := p.q.GetTodayDiapers(ctx, since)
		if err != nil {
			return nil, err
		}
		return diaperEvents(rows), nil
	case "feeding":
		rows, err := p.q.GetLast24hFeedings(ctx, since)
		if err != nil {
			return nil, err
		}
		return feedingEvents(rows, view, p.log), nil
	case "sleep":
		rows, err := p.q.GetTodaySleepSessions(ctx, since)
		if err != nil {
			return nil, err
		}
		return sleepEvents(rows, view, loc), nil
	case "tummy":
		rows, err := p.q.GetLast24hTummy(ctx, since)
		if err != nil {
			return nil, err
		}
		return tummyEvents(rows, view), nil
	}
	return nil, nil
}

func diaperEvents(rows []*store.GetTodayDiapersRow) []trendEvent {
	out := make([]trendEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, trendEvent{when: r.Timestamp.Time, segs: map[string]float64{r.Type: 1}})
	}
	return out
}

// bottleOzEpsilon guards the residual comparison against float noise from the
// numeric→float8 conversion. Amounts are recorded to 0.25 oz at the finest, so any
// real residual is orders of magnitude above this.
const bottleOzEpsilon = 1e-6

// bottleSegOz resolves a bottle feed's (milk, formula) segment contribution,
// reconciling the split columns against amount_oz. A single-source bottle is persisted
// with amount_oz alone (schemas.AdaFeedingLoggedData: "AmountOz is set for single-source
// bottles; BreastMilkOz and FormulaOz for mixed"), so a reader that trusts only the split
// columns drops it entirely — the #82 undercount. Any amount in excess of the recorded
// split is therefore attributed by feed source, mirroring the write-side resolution in
// supplementAmounts.
//
// The residual is clamped at zero so a row whose split exceeds its amount — a mixed
// bottle logged via ada.feeding.log, which carries no amount_oz — keeps its full split
// instead of being reduced. ok is false when a residual exists but the source gives no
// basis to attribute it; the caller reports that rather than absorbing it silently.
func bottleSegOz(source string, amountOz, milkOz, formulaOz float64) (milk, formula float64, ok bool) {
	milk, formula = milkOz, formulaOz
	residual := amountOz - (milk + formula)
	if residual <= bottleOzEpsilon {
		return milk, formula, true
	}
	switch normalizeSource(source) {
	case "bottle_breast":
		milk += residual
	case "bottle_formula":
		formula += residual
	case "mixed":
		// The logged split is the caregiver's stated ratio; prorate the excess across it
		// so that ratio survives. With no split to prorate against there is no basis.
		split := milk + formula
		if split <= 0 {
			return milk, formula, false
		}
		milk += residual * (milk / split)
		formula += residual * (formula / split)
	default:
		// A breast feed carrying bottle detail — a supplement merged by
		// AddFeedingBottleDetailAmounts whose own source was itself unresolved.
		return milk, formula, false
	}
	return milk, formula, true
}

func feedingEvents(rows []*store.GetLast24hFeedingsRow, view string, log *slog.Logger) []trendEvent {
	out := make([]trendEvent, 0, len(rows))
	for _, r := range rows {
		var segs map[string]float64
		switch view {
		case "breast":
			segs = map[string]float64{"left": float64(r.LeftDurationS) / 60, "right": float64(r.RightDurationS) / 60}
		case "bottle":
			milk, formula, ok := bottleSegOz(r.Source, r.AmountOz, r.BreastMilkOz, r.FormulaOz)
			if !ok && log != nil {
				log.Warn("ada: bottle trend residual not attributable to a segment",
					slog.String("feeding_id", uuid.UUID(r.ID.Bytes).String()),
					slog.String("source", r.Source),
					slog.Float64("amount_oz", r.AmountOz),
					slog.Float64("attributed_oz", milk+formula))
			}
			segs = map[string]float64{"milk": milk, "formula": formula}
		case "feeds":
			if isBottleSource(r.Source) {
				segs = map[string]float64{"bo": 1}
			} else {
				segs = map[string]float64{"bf": 1}
			}
		}
		out = append(out, trendEvent{when: r.Timestamp.Time, segs: segs})
	}
	return out
}

func sleepEvents(rows []*store.GetTodaySleepSessionsRow, view string, loc *time.Location) []trendEvent {
	if view == "wakeups" {
		return sleepWakeupEvents(rows, loc)
	}
	out := make([]trendEvent, 0, len(rows))
	for _, r := range rows {
		hrs := r.EndTime.Time.Sub(r.StartTime.Time).Hours()
		if hrs < 0 {
			hrs = 0
		}
		seg := "nap"
		if r.SleepType == "night" {
			seg = "night"
		}
		out = append(out, trendEvent{when: r.StartTime.Time, segs: map[string]float64{seg: hrs}})
	}
	return out
}

// sleepWakeupEvents counts night-sleep sessions beyond the first within each night as
// wakeups (ADR-0032 §10). Nights are keyed by a noon cut in loc: a session starting
// before 12:00 local belongs to the previous calendar day's night. Wakeups are emitted
// stamped at the night-day's local midnight, so an early-morning wakeup lands in the
// bucket of the night it belongs to, not the next day's.
func sleepWakeupEvents(rows []*store.GetTodaySleepSessionsRow, loc *time.Location) []trendEvent {
	type night struct {
		start  time.Time
		dayKey time.Time
	}
	var nights []night
	for _, r := range rows {
		if r.SleepType != "night" {
			continue
		}
		st := r.StartTime.Time
		key := midnight(st.In(loc))
		if st.In(loc).Hour() < 12 {
			key = key.AddDate(0, 0, -1)
		}
		nights = append(nights, night{start: st, dayKey: key})
	}
	sort.Slice(nights, func(i, j int) bool { return nights[i].start.Before(nights[j].start) })
	seen := make(map[time.Time]bool)
	out := make([]trendEvent, 0)
	for _, nt := range nights {
		if seen[nt.dayKey] {
			out = append(out, trendEvent{when: nt.dayKey, segs: map[string]float64{"wakeups": 1}})
		} else {
			seen[nt.dayKey] = true
		}
	}
	return out
}

func tummyEvents(rows []*store.GetLast24hTummyRow, view string) []trendEvent {
	out := make([]trendEvent, 0, len(rows))
	for _, r := range rows {
		var segs map[string]float64
		if view == "sessions" {
			segs = map[string]float64{"sessions": 1}
		} else {
			segs = map[string]float64{"min": float64(r.DurationS) / 60}
		}
		out = append(out, trendEvent{when: r.StartTime.Time, segs: segs})
	}
	return out
}

func (p *Processor) pushTrends(ctx context.Context, resp trendResponse) {
	b, err := json.Marshal(resp)
	if err != nil {
		p.log.Warn("ada: marshal trends response", slog.String("error", err.Error()))
		return
	}
	var attrs map[string]any
	if err := json.Unmarshal(b, &attrs); err != nil {
		p.log.Warn("ada: trends response to attrs", slog.String("error", err.Error()))
		return
	}
	state := resp.RequestID
	if state == "" {
		state = "ok"
	}
	if err := p.ha.PushState(ctx, sensorTrends, state, attrs); err != nil {
		p.log.Warn("ada: push trends", slog.String("error", err.Error()))
	}
}
