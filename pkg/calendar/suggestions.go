package calendar

import (
	"time"

	"github.com/primaryrutabaga/ruby-core/pkg/calendar/expand"
)

// DefaultSuggestionWindow is the lookback over which provider usage is scored.
const DefaultSuggestionWindow = 90 * 24 * time.Hour

// RankProviderUsage scores each provider by recency-weighted per-occurrence usage:
// it expands each associated event's PAST occurrences within [now-window, now] and
// sums a linear recency weight (1.0 at now, →0 at the window edge). A weekly series
// therefore scores much higher than a single past event — exactly the
// "computed from associations + expansion, nothing stored" property (ROADMAP-0012).
func RankProviderUsage(providerEvents map[string][]expand.Event, now time.Time, window time.Duration) map[string]float64 {
	scores := make(map[string]float64, len(providerEvents))
	if window <= 0 {
		return scores
	}
	from := now.Add(-window)
	windowSecs := window.Seconds()

	for pid, events := range providerEvents {
		var total float64
		for _, ev := range events {
			insts, err := expand.Expand(ev, nil, from, now)
			if err != nil {
				continue
			}
			for _, in := range insts {
				if in.Start.Before(from) || in.Start.After(now) {
					continue
				}
				w := 1 - now.Sub(in.Start).Seconds()/windowSecs
				if w > 0 {
					total += w
				}
			}
		}
		scores[pid] = total
	}
	return scores
}
