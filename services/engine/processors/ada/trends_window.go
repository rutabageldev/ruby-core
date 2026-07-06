package ada

import "time"

// Calendar window math for trends (#161, ADR-0032 §5-§9). All boundaries are built with
// time.Date/AddDate in the reference time's location — never by adding fixed durations,
// because weeks and months containing DST transitions are not multiples of 24h. Bucket
// membership stays [start, end) on absolute instants.

// midnight returns local midnight of t's calendar day, in t's location.
func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// weekStart returns local midnight of the Sunday on or before t.
func weekStart(t time.Time) time.Time {
	d := midnight(t)
	return d.AddDate(0, 0, -int(d.Weekday()))
}

// normalizeOffset clamps a period offset to ≤ 0 (0 = current period, -n = n periods back).
func normalizeOffset(o int) int {
	if o > 0 {
		return 0
	}
	return o
}

// calendarWindow returns the [start, end) calendar window for a period at the given
// offset, anchored in now's location: week = Sun–Sat, month = calendar month, year =
// calendar year. offset must already be normalized.
func calendarWindow(period string, offset int, now time.Time) (start, end time.Time, ok bool) {
	loc := now.Location()
	switch period {
	case "week":
		start = weekStart(now).AddDate(0, 0, 7*offset)
		return start, start.AddDate(0, 0, 7), true
	case "month":
		// time.Date normalizes month overflow/underflow (e.g. July + -7 → December prev year).
		start = time.Date(now.Year(), now.Month()+time.Month(offset), 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 1, 0), true
	case "year":
		start = time.Date(now.Year()+offset, time.January, 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(1, 0, 0), true
	}
	return time.Time{}, time.Time{}, false
}

// calendarBuckets splits a calendar window into display buckets: week = 7 one-day
// buckets, month = Sun–Sat weeks clipped to the month (4–6 buckets), year = 12 one-month
// buckets. The shape depends only on the window, so offset-0 windows always carry their
// full shape and future buckets aggregate to zero (ADR-0032 §6).
func calendarBuckets(period string, winStart, winEnd time.Time) []bucketWindow {
	var out []bucketWindow
	switch period {
	case "week":
		for i := range 7 {
			start := winStart.AddDate(0, 0, i)
			out = append(out, bucketWindow{start: start, end: winStart.AddDate(0, 0, i+1), label: bucketLabel(period, start)})
		}
	case "month":
		for cur := winStart; cur.Before(winEnd); {
			step := (7 - int(cur.Weekday())) % 7
			if step == 0 {
				step = 7
			}
			next := cur.AddDate(0, 0, step)
			if next.After(winEnd) {
				next = winEnd
			}
			out = append(out, bucketWindow{start: cur, end: next, label: bucketLabel(period, cur)})
			cur = next
		}
	case "year":
		for i := range 12 {
			start := winStart.AddDate(0, i, 0)
			out = append(out, bucketWindow{start: start, end: winStart.AddDate(0, i+1, 0), label: bucketLabel(period, start)})
		}
	}
	return out
}

// daysBetween counts calendar days from a's day to b's day (b exclusive) by stepping
// AddDate, so DST hour drift cannot skew the count.
func daysBetween(a, b time.Time) int {
	n := 0
	for d := midnight(a); d.Before(b); d = d.AddDate(0, 0, 1) {
		n++
	}
	return n
}

// daysElapsedIn returns the response days_elapsed (ADR-0032 §7): the full day count for
// past windows, days into the period including today for the current one.
func daysElapsedIn(winStart, winEnd, now time.Time, offset int) int {
	if offset < 0 {
		return daysBetween(winStart, winEnd)
	}
	return daysBetween(winStart, midnight(now).AddDate(0, 0, 1))
}

// minOffsetFor returns the offset of the period containing birth (ADR-0032 §9), ≤ 0.
func minOffsetFor(period string, birth, now time.Time) int {
	b := birth.In(now.Location())
	var mo int
	switch period {
	case "week":
		mo = -daysBetween(weekStart(b), weekStart(now)) / 7
	case "month":
		mo = -((now.Year()*12 + int(now.Month())) - (b.Year()*12 + int(b.Month())))
	case "year":
		mo = -(now.Year() - b.Year())
	}
	return normalizeOffset(mo)
}
