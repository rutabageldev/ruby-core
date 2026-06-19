package ada

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/store"
)

// Trends aggregation (#82, ADR-0032). The dashboard fires ada.trends.query and reads
// the bucketed result back from sensor.ada_trends, matched by request_id. Buckets are
// aligned to the bedtime boundary (sensor.ada_today_boundary): week = 7×1-day, month =
// 4×7-day, year = 12×~30-day, with the most recent bucket covering the current day-window.

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
}

// periodSpec returns the bucket width and count for a period.
func periodSpec(period string) (width time.Duration, count int, ok bool) {
	switch period {
	case "week":
		return 24 * time.Hour, 7, true
	case "month":
		return 7 * 24 * time.Hour, 4, true
	case "year":
		return 30 * 24 * time.Hour, 12, true
	}
	return 0, 0, false
}

// trendBuckets returns the boundary-aligned buckets for a period, the most recent ending
// at windowEnd (the next bedtime boundary, so the current partial day-window is included).
func trendBuckets(period string, windowEnd time.Time) []bucketWindow {
	w, n, ok := periodSpec(period)
	if !ok {
		return nil
	}
	out := make([]bucketWindow, n)
	for i := 0; i < n; i++ {
		start := windowEnd.Add(-time.Duration(n-i) * w)
		end := windowEnd.Add(-time.Duration(n-1-i) * w)
		out[i] = bucketWindow{start: start, end: end, label: bucketLabel(period, start)}
	}
	return out
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
// total (current window), and prevGrand (events before the window, i.e. the prior equal
// window — callers fetch events back to prevWindowStart). Values are rounded for display.
func aggregateTrend(events []trendEvent, buckets []bucketWindow, segKeys []string) (out []trendBucket, totals map[string]float64, grand, prevGrand float64) {
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
	windowStart := buckets[0].start

	for _, e := range events {
		if e.when.Before(windowStart) {
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

	resp := trendResponse{
		RequestID:   d.RequestID,
		Metric:      d.Metric,
		View:        d.View,
		Period:      d.Period,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Buckets:     []trendBucket{},
		Totals:      map[string]float64{},
	}

	segKeys, okSeg := trendSegKeys(d.Metric, d.View)
	_, n, okPeriod := periodSpec(d.Period)
	if !okSeg || !okPeriod {
		p.log.Warn("ada: trends query unsupported — returning empty result",
			slog.String("metric", d.Metric), slog.String("view", d.View), slog.String("period", d.Period))
		p.pushTrends(ctx, resp) // echo an empty response so the dashboard does not hang
		return nil
	}

	w, _, _ := periodSpec(d.Period)
	cfg := p.loadSleepConfig(ctx)
	b0 := computeTodayBoundary(cfg.BedtimeHHMM)
	windowEnd := b0.Add(24 * time.Hour)
	buckets := trendBuckets(d.Period, windowEnd)
	windowStart := buckets[0].start
	prevStart := windowStart.Add(-time.Duration(n) * w)

	events, err := p.trendEvents(ctx, d.Metric, d.View, prevStart, b0)
	if err != nil {
		return fmt.Errorf("ada: load trends events: %w", err)
	}

	resp.Buckets, resp.Totals, resp.Grand, resp.PrevGrand = aggregateTrend(events, buckets, segKeys)
	p.pushTrends(ctx, resp)
	return nil
}

// trendEvents fetches the metric's rows since prevStart (reusing the rolling-window
// queries, which have no upper bound and therefore reach to now) and maps them to
// additive trend events. b0 is the current bedtime boundary, used for wakeup grouping.
func (p *Processor) trendEvents(ctx context.Context, metric, view string, prevStart, b0 time.Time) ([]trendEvent, error) {
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
		return feedingEvents(rows, view), nil
	case "sleep":
		rows, err := p.q.GetTodaySleepSessions(ctx, since)
		if err != nil {
			return nil, err
		}
		return sleepEvents(rows, view, b0), nil
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

func feedingEvents(rows []*store.GetLast24hFeedingsRow, view string) []trendEvent {
	out := make([]trendEvent, 0, len(rows))
	for _, r := range rows {
		var segs map[string]float64
		switch view {
		case "breast":
			segs = map[string]float64{"left": float64(r.LeftDurationS) / 60, "right": float64(r.RightDurationS) / 60}
		case "bottle":
			segs = map[string]float64{"milk": r.BreastMilkOz, "formula": r.FormulaOz}
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

func sleepEvents(rows []*store.GetTodaySleepSessionsRow, view string, b0 time.Time) []trendEvent {
	if view == "wakeups" {
		return sleepWakeupEvents(rows, b0)
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

// sleepWakeupEvents counts night-sleep sessions beyond the first within each night-window
// (a bedtime-to-bedtime day) as wakeups. Definition to confirm against the buildTrend() mock.
func sleepWakeupEvents(rows []*store.GetTodaySleepSessionsRow, b0 time.Time) []trendEvent {
	type night struct {
		start  time.Time
		dayIdx int
	}
	var nights []night
	for _, r := range rows {
		if r.SleepType != "night" {
			continue
		}
		st := r.StartTime.Time
		nights = append(nights, night{start: st, dayIdx: int(math.Floor(st.Sub(b0).Hours() / 24))})
	}
	sort.Slice(nights, func(i, j int) bool { return nights[i].start.Before(nights[j].start) })
	seen := make(map[int]bool)
	out := make([]trendEvent, 0)
	for _, nt := range nights {
		if seen[nt.dayIdx] {
			out = append(out, trendEvent{when: nt.start, segs: map[string]float64{"wakeups": 1}})
		} else {
			seen[nt.dayIdx] = true
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
