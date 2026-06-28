// Package expand performs on-demand, timezone-aware expansion of calendar events
// into concrete instances over a window (ROADMAP-0012, ADR-0042). Recurring series
// are expanded in their IANA timezone so a weekly 9am slot stays 9am local across
// DST boundaries; EXDATEs (carried in the recurrence lines) and cancelled override
// children are subtracted, and modified override children replace the base instance.
//
// It is intentionally decoupled from the storage layer: callers map their rows into
// Event/Override values. Used by both the engine (reminders) and the read API.
package expand

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// MaxWindowDays bounds how wide a single expansion request may be. On-demand
// expansion is otherwise unbounded; the read endpoint enforces this (ADR-0042).
const MaxWindowDays = 366

// ErrWindowTooLarge is returned by CheckWindow when [from, to) exceeds MaxWindowDays.
var ErrWindowTooLarge = errors.New("calendar: requested range exceeds the maximum window")

// Event is the expansion input: a single or recurring master event. Start/End are
// absolute instants of the first occurrence; Location is the IANA zone used for
// DST-correct recurrence math (nil means UTC).
type Event struct {
	GoogleEventID string
	Start         time.Time
	End           time.Time
	Location      *time.Location
	AllDay        bool
	Recurrence    []string // RRULE/EXDATE/RDATE lines; empty => single event
}

// Override is a modified or cancelled single occurrence (a Google child event with
// recurringEventId + originalStartTime), keyed by the original occurrence start.
type Override struct {
	OriginalStart time.Time
	Cancelled     bool
	Instance      Instance // replacement; ignored when Cancelled
}

// Instance is one concrete occurrence in the result. Start/End are UTC instants.
type Instance struct {
	GoogleEventID string
	Start         time.Time
	End           time.Time
	AllDay        bool
	IsOverride    bool
}

// CheckWindow returns ErrWindowTooLarge if [from, to) is invalid or exceeds the
// maximum window. Call it before Expand at request boundaries.
func CheckWindow(from, to time.Time) error {
	if !to.After(from) {
		return fmt.Errorf("calendar: range end must be after start")
	}
	if to.Sub(from) > MaxWindowDays*24*time.Hour {
		return ErrWindowTooLarge
	}
	return nil
}

// Expand returns all instances of ev whose [start, end) overlaps [from, to),
// timezone-aware, with EXDATEs and cancelled overrides subtracted and modified
// overrides applied. Results are sorted by start.
func Expand(ev Event, overrides []Override, from, to time.Time) ([]Instance, error) {
	loc := ev.Location
	if loc == nil {
		loc = time.UTC
	}
	duration := max(ev.End.Sub(ev.Start), 0)

	cancelled := make(map[int64]bool, len(overrides))
	replaced := make(map[int64]Instance, len(overrides))
	for _, o := range overrides {
		key := o.OriginalStart.UTC().Unix()
		if o.Cancelled {
			cancelled[key] = true
		} else {
			replaced[key] = o.Instance
		}
	}

	var starts []time.Time
	if len(ev.Recurrence) == 0 {
		starts = []time.Time{ev.Start}
	} else {
		set, err := buildSet(ev.Start, loc, ev.Recurrence)
		if err != nil {
			return nil, err
		}
		// Widen the lower bound by the event duration so an occurrence that starts
		// before `from` but still overlaps the window is included.
		starts = set.Between(from.Add(-duration), to, true)
	}

	out := make([]Instance, 0, len(starts))
	for _, s := range starts {
		st := s.UTC()
		en := st.Add(duration)
		// Keep instances overlapping [from, to): end > from && start < to.
		if !en.After(from) || !st.Before(to) {
			continue
		}
		key := st.Unix()
		if cancelled[key] {
			continue
		}
		if inst, ok := replaced[key]; ok {
			out = append(out, inst)
			continue
		}
		out = append(out, Instance{
			GoogleEventID: ev.GoogleEventID,
			Start:         st,
			End:           en,
			AllDay:        ev.AllDay,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

// buildSet composes a DTSTART line (in the event's timezone) with the recurrence
// lines and parses them into an rrule.Set. rrule-go returns occurrences in the
// DTSTART timezone, which Expand converts to UTC.
func buildSet(start time.Time, loc *time.Location, recurrence []string) (*rrule.Set, error) {
	var sb strings.Builder
	sb.WriteString("DTSTART;TZID=")
	sb.WriteString(loc.String())
	sb.WriteString(":")
	sb.WriteString(start.In(loc).Format("20060102T150405"))
	for _, line := range recurrence {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sb.WriteString("\n")
		sb.WriteString(line)
	}

	set, err := rrule.StrToRRuleSet(sb.String())
	if err != nil {
		return nil, fmt.Errorf("calendar: parse recurrence: %w", err)
	}
	return set, nil
}
