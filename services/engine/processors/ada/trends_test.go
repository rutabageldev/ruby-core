//go:build fast

package ada

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/store"
)

func TestAggregateTrend(t *testing.T) {
	loc := nyLoc(t)
	now := time.Date(2026, 7, 8, 15, 0, 0, 0, loc) // Wednesday of week [7/5, 7/12)
	winStart, winEnd, _ := calendarWindow("week", 0, now)
	buckets := calendarBuckets("week", winStart, winEnd)
	segKeys := []string{"wet", "dirty", "mixed"}

	// Truncated comparison: 4 days elapsed → prev window is the first 4 days of last week.
	prevStart, _, _ := calendarWindow("week", -1, now)
	prevCmpEnd := prevStart.AddDate(0, 0, 4)

	events := []trendEvent{
		{when: now.Add(-1 * time.Hour), segs: map[string]float64{"wet": 1}},                   // Wed bucket
		{when: now.Add(-1 * time.Hour), segs: map[string]float64{"dirty": 1}},                 // Wed bucket
		{when: time.Date(2026, 7, 5, 8, 0, 0, 0, loc), segs: map[string]float64{"wet": 1}},    // Sun bucket
		{when: time.Date(2026, 6, 29, 8, 0, 0, 0, loc), segs: map[string]float64{"mixed": 1}}, // prev cmp window
		{when: time.Date(2026, 7, 3, 8, 0, 0, 0, loc), segs: map[string]float64{"mixed": 1}},  // [prevCmpEnd, winStart): dropped
		{when: prevCmpEnd, segs: map[string]float64{"mixed": 1}},                              // exactly at prevCmpEnd: dropped
		{when: prevStart.Add(-1 * time.Hour), segs: map[string]float64{"wet": 5}},             // before prevStart: dropped
		{when: time.Date(2026, 7, 20, 8, 0, 0, 0, loc), segs: map[string]float64{"wet": 5}},   // after window: dropped
	}

	out, totals, grand, prevGrand := aggregateTrend(events, buckets, segKeys, prevStart, prevCmpEnd)

	if grand != 3 {
		t.Errorf("grand = %v, want 3", grand)
	}
	if prevGrand != 1 {
		t.Errorf("prevGrand = %v, want 1 (truncated window only)", prevGrand)
	}
	if out[3].Total != 2 { // Wednesday
		t.Errorf("Wed bucket total = %v, want 2", out[3].Total)
	}
	if out[0].Total != 1 { // Sunday
		t.Errorf("Sun bucket total = %v, want 1", out[0].Total)
	}
	if totals["wet"] != 2 || totals["dirty"] != 1 || totals["mixed"] != 0 {
		t.Errorf("totals = %v, want wet=2 dirty=1 mixed=0", totals)
	}
	if len(out) != 7 {
		t.Errorf("buckets = %d, want full 7-day shape with zeroed future days", len(out))
	}
}

// TestAggregateTrend_PrevMonthClamp: 30 days into March vs. 28-day February — the
// comparison clamps to all of February instead of overshooting into March.
func TestAggregateTrend_PrevMonthClamp(t *testing.T) {
	loc := nyLoc(t)
	now := time.Date(2026, 3, 30, 12, 0, 0, 0, loc)
	winStart, winEnd, _ := calendarWindow("month", 0, now)
	prevStart, prevEnd, _ := calendarWindow("month", -1, now)

	daysElapsed := daysElapsedIn(winStart, winEnd, now, 0)
	if daysElapsed != 30 {
		t.Fatalf("days_elapsed = %d, want 30", daysElapsed)
	}
	prevCmpEnd := prevEnd
	if t2 := prevStart.AddDate(0, 0, daysElapsed); t2.Before(prevEnd) {
		prevCmpEnd = t2
	}
	if !prevCmpEnd.Equal(prevEnd) {
		t.Errorf("prevCmpEnd = %v, want clamped to full February end %v", prevCmpEnd, prevEnd)
	}
}

func TestSleepWakeupEvents_NightDayAttribution(t *testing.T) {
	loc := nyLoc(t)
	dayD := time.Date(2026, 7, 6, 0, 0, 0, 0, loc)

	row := func(start time.Time, sleepType string) *store.GetTodaySleepSessionsRow {
		return &store.GetTodaySleepSessionsRow{
			StartTime: pgtype.Timestamptz{Time: start, Valid: true},
			EndTime:   pgtype.Timestamptz{Time: start.Add(2 * time.Hour), Valid: true},
			SleepType: sleepType,
		}
	}

	// (a) 21:00 night + 02:00 next-day night = one night with one wakeup, stamped on day D.
	// Rows are UTC instants to prove the .In(loc) conversion (pgx location is driver-dependent).
	evs := sleepWakeupEvents([]*store.GetTodaySleepSessionsRow{
		row(time.Date(2026, 7, 6, 21, 0, 0, 0, loc).UTC(), "night"),
		row(time.Date(2026, 7, 7, 2, 0, 0, 0, loc).UTC(), "night"),
	}, loc)
	if len(evs) != 1 {
		t.Fatalf("wakeups = %d, want 1", len(evs))
	}
	if !evs[0].when.Equal(dayD) {
		t.Errorf("wakeup stamped %v, want day D midnight %v", evs[0].when, dayD)
	}

	// (b) A lone early-morning session keys to the previous day and is the night itself.
	evs = sleepWakeupEvents([]*store.GetTodaySleepSessionsRow{
		row(time.Date(2026, 7, 7, 2, 0, 0, 0, loc), "night"),
	}, loc)
	if len(evs) != 0 {
		t.Errorf("lone 02:00 session wakeups = %d, want 0", len(evs))
	}

	// (c) Nights on consecutive evenings are separate nights — no wakeups.
	evs = sleepWakeupEvents([]*store.GetTodaySleepSessionsRow{
		row(time.Date(2026, 7, 6, 21, 0, 0, 0, loc), "night"),
		row(time.Date(2026, 7, 7, 21, 30, 0, 0, loc), "night"),
	}, loc)
	if len(evs) != 0 {
		t.Errorf("consecutive-evening wakeups = %d, want 0", len(evs))
	}

	// (d) Naps never count.
	evs = sleepWakeupEvents([]*store.GetTodaySleepSessionsRow{
		row(time.Date(2026, 7, 6, 13, 0, 0, 0, loc), "nap"),
		row(time.Date(2026, 7, 6, 15, 0, 0, 0, loc), "nap"),
	}, loc)
	if len(evs) != 0 {
		t.Errorf("nap wakeups = %d, want 0", len(evs))
	}
}

// TestTrendResponseJSON_Additive pins the wire contract: legacy keys unchanged
// (including camelCase prevGrand), new #161 keys snake_case, min_offset omitted when nil.
func TestTrendResponseJSON_Additive(t *testing.T) {
	resp := trendResponse{
		RequestID: "r1",
		Buckets:   []trendBucket{},
		Totals:    map[string]float64{},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{
		"request_id", "metric", "view", "period", "generated_at",
		"buckets", "totals", "grand", "prevGrand",
		"window_start", "window_end", "days_elapsed", "offset",
	} {
		if _, ok := m[k]; !ok {
			t.Errorf("marshaled response missing key %q", k)
		}
	}
	if _, ok := m["min_offset"]; ok {
		t.Error("min_offset should be omitted when nil")
	}

	mo := -3
	resp.MinOffset = &mo
	b, _ = json.Marshal(resp)
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["min_offset"]; !ok || v != float64(-3) {
		t.Errorf("min_offset = %v ok=%v, want -3", v, ok)
	}
}

func TestTrendSegKeys(t *testing.T) {
	ok := map[[2]string][]string{
		{"diapers", "count"}:  {"wet", "dirty", "mixed"},
		{"feeding", "breast"}: {"left", "right"},
		{"feeding", "bottle"}: {"milk", "formula"},
		{"feeding", "feeds"}:  {"bf", "bo"},
		{"sleep", "hours"}:    {"night", "nap"},
		{"sleep", "wakeups"}:  {"wakeups"},
		{"tummy", "min"}:      {"min"},
		{"tummy", "sessions"}: {"sessions"},
	}
	for k, want := range ok {
		got, valid := trendSegKeys(k[0], k[1])
		if !valid || len(got) != len(want) {
			t.Errorf("trendSegKeys(%q,%q) = %v,%v; want %v", k[0], k[1], got, valid, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("trendSegKeys(%q,%q)[%d] = %q, want %q", k[0], k[1], i, got[i], want[i])
			}
		}
	}
	if _, valid := trendSegKeys("bogus", "x"); valid {
		t.Error("unknown metric should be invalid")
	}
	if _, valid := trendSegKeys("sleep", "bogus"); valid {
		t.Error("unknown sleep view should be invalid")
	}
}

// ── Bottle segment reconciliation (PLAN-0040) ─────────────────────────────────

func TestBottleSegOz(t *testing.T) {
	cases := []struct {
		name               string
		source             string
		amount, milk, form float64
		wantMilk, wantForm float64
		wantOK             bool
	}{
		{
			name:   "single-source formula bottle carries amount only",
			source: "bottle_formula", amount: 3, milk: 0, form: 0,
			wantMilk: 0, wantForm: 3, wantOK: true,
		},
		{
			name:   "single-source breast-milk bottle carries amount only",
			source: "bottle_breast", amount: 2.5, milk: 0, form: 0,
			wantMilk: 2.5, wantForm: 0, wantOK: true,
		},
		{
			name:   "legacy dashboard source names normalize",
			source: "formula", amount: 4, milk: 0, form: 0,
			wantMilk: 0, wantForm: 4, wantOK: true,
		},
		{
			name:   "explicit split with matching amount is not double-counted",
			source: "bottle_formula", amount: 3, milk: 0, form: 3,
			wantMilk: 0, wantForm: 3, wantOK: true,
		},
		{
			name:   "mixed bottle with amount_oz 0 keeps its full split",
			source: "mixed", amount: 0, milk: 0, form: 1,
			wantMilk: 0, wantForm: 1, wantOK: true,
		},
		{
			name:   "partial residual is attributed on top of an existing split",
			source: "bottle_formula", amount: 5, milk: 0, form: 3,
			wantMilk: 0, wantForm: 5, wantOK: true,
		},
		{
			name:   "mixed residual prorates across the logged ratio",
			source: "mixed", amount: 8, milk: 1, form: 3,
			wantMilk: 2, wantForm: 6, wantOK: true,
		},
		{
			name:   "mixed residual with no split to prorate is unattributable",
			source: "mixed", amount: 3, milk: 0, form: 0,
			wantMilk: 0, wantForm: 0, wantOK: false,
		},
		{
			name:   "supplement residual on a breast feed is unattributable",
			source: "breast_left", amount: 2, milk: 0, form: 0,
			wantMilk: 0, wantForm: 0, wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			milk, form, ok := bottleSegOz(c.source, c.amount, c.milk, c.form)
			if milk != c.wantMilk || form != c.wantForm || ok != c.wantOK {
				t.Errorf("bottleSegOz(%q, %v, %v, %v) = %v, %v, %v; want %v, %v, %v",
					c.source, c.amount, c.milk, c.form, milk, form, ok, c.wantMilk, c.wantForm, c.wantOK)
			}
		})
	}
}

func TestFeedingEvents_BottleUsesAmountFallback(t *testing.T) {
	rows := []*store.GetLast24hFeedingsRow{
		{
			ID:        mustUUID("aaaaaaaa-0000-0000-0000-000000000001"),
			Timestamp: mustTimestamptz("2026-07-22T08:41:30Z"),
			Source:    "bottle_formula",
			AmountOz:  3, // amount only — the row shape that was silently dropped
		},
		{
			ID:        mustUUID("aaaaaaaa-0000-0000-0000-000000000002"),
			Timestamp: mustTimestamptz("2026-07-22T12:15:00Z"),
			Source:    "bottle_formula",
			AmountOz:  3,
			FormulaOz: 3,
		},
	}
	events := feedingEvents(rows, "bottle", nil)
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	var total float64
	for _, e := range events {
		if e.segs["milk"] != 0 {
			t.Errorf("milk = %v, want 0 for a formula-only household", e.segs["milk"])
		}
		total += e.segs["formula"]
	}
	if total != 6 {
		t.Errorf("formula total = %v, want 6 (amount-only feed must contribute)", total)
	}
}

// TestFeedingEvents_BottleWeekReproduction replays the prod week of 2026-07-19 that
// surfaced the undercount (#82/#161). Before PLAN-0040 these buckets read 14/10/14/9
// with a grand of 47, because every amount-only row contributed zero.
func TestFeedingEvents_BottleWeekReproduction(t *testing.T) {
	loc := nyLoc(t)
	now := time.Date(2026, 7, 22, 15, 0, 0, 0, loc) // Wednesday of week [7/19, 7/26)

	// (day, hour, minute, amount_oz, formula_oz) exactly as persisted in prod.
	feeds := []struct {
		day, hour, min  int
		amount, formula float64
	}{
		{19, 0, 0, 1, 1}, {19, 3, 45, 3, 3}, {19, 7, 15, 2.5, 2.5}, {19, 10, 5, 2.5, 2.5},
		{19, 12, 53, 2.5, 0}, {19, 16, 5, 2.5, 2.5}, {19, 19, 28, 2.5, 0}, {19, 21, 55, 2.5, 2.5},
		{20, 2, 15, 2.5, 2.5}, {20, 6, 0, 2.5, 2.5}, {20, 8, 27, 2, 2}, {20, 12, 35, 3, 3},
		{20, 14, 58, 2.5, 0}, {20, 18, 35, 2.5, 0}, {20, 21, 37, 3, 0},
		{21, 1, 55, 3, 3}, {21, 5, 55, 3, 3}, {21, 8, 45, 2.5, 2.5}, {21, 12, 20, 2.5, 2.5},
		{21, 14, 53, 2.5, 0}, {21, 18, 4, 2.5, 0}, {21, 20, 55, 3, 3},
		{22, 0, 50, 3, 3}, {22, 4, 41, 3, 0}, {22, 8, 15, 3, 3}, {22, 11, 15, 3, 3},
	}
	rows := make([]*store.GetLast24hFeedingsRow, 0, len(feeds))
	for _, f := range feeds {
		ts := time.Date(2026, 7, f.day, f.hour, f.min, 0, 0, loc)
		rows = append(rows, &store.GetLast24hFeedingsRow{
			Timestamp: pgtype.Timestamptz{Time: ts, Valid: true},
			Source:    "bottle_formula",
			AmountOz:  f.amount,
			FormulaOz: f.formula,
		})
	}

	winStart, winEnd, _ := calendarWindow("week", 0, now)
	buckets := calendarBuckets("week", winStart, winEnd)
	prevStart, _, _ := calendarWindow("week", -1, now)
	out, totals, grand, _ := aggregateTrend(
		feedingEvents(rows, "bottle", nil), buckets, []string{"milk", "formula"}, prevStart, prevStart)

	want := []float64{19, 18, 19, 12, 0, 0, 0} // Sun…Sat
	for i, w := range want {
		if out[i].Total != w {
			t.Errorf("%s bucket = %v, want %v", out[i].Label, out[i].Total, w)
		}
	}
	if grand != 68 {
		t.Errorf("grand = %v, want 68", grand)
	}
	if totals["formula"] != 68 || totals["milk"] != 0 {
		t.Errorf("totals = %v, want formula=68 milk=0", totals)
	}
}
