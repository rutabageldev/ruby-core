//go:build fast

package ada

import (
	"testing"
	"time"
)

func TestTrendBuckets_Week(t *testing.T) {
	windowEnd := time.Date(2026, 6, 18, 19, 0, 0, 0, time.UTC)
	bs := trendBuckets("week", windowEnd)
	if len(bs) != 7 {
		t.Fatalf("week buckets = %d, want 7", len(bs))
	}
	if !bs[6].end.Equal(windowEnd) {
		t.Errorf("last bucket end = %v, want %v", bs[6].end, windowEnd)
	}
	if !bs[6].start.Equal(windowEnd.Add(-24 * time.Hour)) {
		t.Errorf("last bucket start = %v, want windowEnd-24h", bs[6].start)
	}
	if !bs[0].start.Equal(windowEnd.Add(-7 * 24 * time.Hour)) {
		t.Errorf("first bucket start = %v, want windowEnd-7d", bs[0].start)
	}
	// contiguous
	for i := 1; i < len(bs); i++ {
		if !bs[i].start.Equal(bs[i-1].end) {
			t.Errorf("bucket %d not contiguous with %d", i, i-1)
		}
	}
}

func TestTrendBuckets_MonthYearCounts(t *testing.T) {
	windowEnd := time.Date(2026, 6, 18, 19, 0, 0, 0, time.UTC)
	if got := len(trendBuckets("month", windowEnd)); got != 4 {
		t.Errorf("month buckets = %d, want 4", got)
	}
	if got := len(trendBuckets("year", windowEnd)); got != 12 {
		t.Errorf("year buckets = %d, want 12", got)
	}
	if trendBuckets("bogus", windowEnd) != nil {
		t.Error("unknown period should yield nil buckets")
	}
}

func TestAggregateTrend(t *testing.T) {
	windowEnd := time.Date(2026, 6, 18, 19, 0, 0, 0, time.UTC)
	buckets := trendBuckets("week", windowEnd)
	segKeys := []string{"wet", "dirty", "mixed"}

	events := []trendEvent{
		{when: windowEnd.Add(-1 * time.Hour), segs: map[string]float64{"wet": 1}},        // bucket 6
		{when: windowEnd.Add(-1 * time.Hour), segs: map[string]float64{"dirty": 1}},      // bucket 6
		{when: windowEnd.Add(-25 * time.Hour), segs: map[string]float64{"wet": 1}},       // bucket 5
		{when: windowEnd.Add(-8 * 24 * time.Hour), segs: map[string]float64{"mixed": 1}}, // prev window
	}

	out, totals, grand, prevGrand := aggregateTrend(events, buckets, segKeys)

	if grand != 3 {
		t.Errorf("grand = %v, want 3", grand)
	}
	if prevGrand != 1 {
		t.Errorf("prevGrand = %v, want 1", prevGrand)
	}
	if out[6].Total != 2 {
		t.Errorf("bucket[6].total = %v, want 2", out[6].Total)
	}
	if out[5].Total != 1 {
		t.Errorf("bucket[5].total = %v, want 1", out[5].Total)
	}
	if totals["wet"] != 2 || totals["dirty"] != 1 || totals["mixed"] != 0 {
		t.Errorf("totals = %v, want wet=2 dirty=1 mixed=0", totals)
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
