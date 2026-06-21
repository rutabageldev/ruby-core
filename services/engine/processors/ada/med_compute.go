package ada

import (
	"strconv"
	"strings"
	"time"
)

// Pure medication safety computations (ROADMAP-0011 effort 0011.3, ADR-0038).
// These mirror the client formulas in adaMeds.ts EXACTLY, to the boundary — the
// engine is authoritative when the app is closed, and the two must never disagree.
// now must be in the family's local zone (the engine container runs
// TZ=America/New_York), matching the dashboard's browser-local Date math.

const medHour = time.Hour

type slotState int

const (
	slotGiven slotState = iota
	slotSkipped
	slotMissed
	slotDue
	slotUpcoming
)

// parseHHMM splits a "HH:MM" slot, mirroring `hhmm.split(':').map(Number)`.
func parseHHMM(s string) (h, m int, ok bool) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	hh, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	mm, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return hh, mm, true
}

// medSlotTime returns today's local "HH:MM", mirroring new Date(nowMs).setHours(h,m,0,0).
func medSlotTime(now time.Time, hhmm string) (time.Time, bool) {
	h, m, ok := parseHHMM(hhmm)
	if !ok {
		return time.Time{}, false
	}
	return time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location()), true
}

// earliestSafe = lastGiven + minIntervalHours (computeGuard). The engine projects
// this absolute time; the consumer derives the live "unsafe" flag as now <
// earliest_safe (so a dose exactly at the boundary is safe).
func earliestSafe(lastGiven time.Time, minIntervalHours float64) time.Time {
	return lastGiven.Add(time.Duration(minIntervalHours * float64(medHour)))
}

// dosesInRolling24h counts given doses in the half-open window (now-24h, now].
func dosesInRolling24h(givenTimes []time.Time, now time.Time) int {
	lower := now.Add(-24 * medHour)
	count := 0
	for _, t := range givenTimes {
		if t.After(lower) && !t.After(now) { // t > now-24h && t <= now
			count++
		}
	}
	return count
}

// routineComplete mirrors isRoutineComplete (minus the status=='completed'
// short-circuit, which the caller checks). givenCount is the routine's given doses.
func routineComplete(endType, endValue string, givenCount int, now time.Time) bool {
	switch endType {
	case "max_doses":
		maxDoses := 0
		if v, err := strconv.Atoi(strings.TrimSpace(endValue)); err == nil {
			maxDoses = v
		} else if f, err := strconv.ParseFloat(strings.TrimSpace(endValue), 64); err == nil {
			maxDoses = int(f)
		}
		return givenCount >= maxDoses
	case "end_date":
		if endValue == "" {
			return false
		}
		end, err := time.ParseInLocation("2006-01-02", endValue, now.Location())
		if err != nil {
			return false
		}
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return today.After(end) // today > end, both at local midnight
	default:
		return false
	}
}

// slotStatus mirrors the per-slot resolution in scheduledInstancesToday. eventStatus
// is the given/skipped event for this slot ("" if none). A slot is `missed` only by
// supersession — a later slot of the same routine has already come due — so missed
// never stacks and never carries a dose forward.
func slotStatus(eventStatus, slot string, fixedTimes []string, now time.Time) slotState {
	switch eventStatus {
	case "given":
		return slotGiven
	case "skipped":
		return slotSkipped
	}
	thisSlot, ok := medSlotTime(now, slot)
	if !ok {
		return slotUpcoming
	}
	for _, t := range fixedTimes {
		st, ok := medSlotTime(now, t)
		if !ok {
			continue
		}
		if st.After(thisSlot) && !st.After(now) { // slotMs(t) > slotMs(slot) && slotMs(t) <= now
			return slotMissed
		}
	}
	if !now.Before(thisSlot) { // now >= slotMs(slot)
		return slotDue
	}
	return slotUpcoming
}

// seriesNextDue = anchor dose time + interval (seriesNextDueMs).
func seriesNextDue(anchorTime time.Time, intervalHours float64) time.Time {
	return anchorTime.Add(time.Duration(intervalHours * float64(medHour)))
}
