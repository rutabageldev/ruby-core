package ada

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/store"
)

// Server-owned medication reconcile (ROADMAP-0011 effort 0011.3, ADR-0038). Runs on
// the 60-second safety-net tick. The engine is authoritative for the time-edge
// transitions that must fire even with the app closed: emitting `missed`, completing
// routines, expiring watches, and reminding when a dose is due. These are the ONLY
// medication state the engine writes without a caregiver action; none ever fabricates
// a dose.

// seriesExpiryBackstop auto-expires an as-needed watch whose anchor dose is older
// than this (Exit 3; the dashboard owns the explicit user exits).
const seriesExpiryBackstop = 24 * time.Hour

// reconcileMedications is the periodic medication state machine. now is local
// (the engine container runs TZ=America/New_York), matching the dashboard clock.
func (p *Processor) reconcileMedications(ctx context.Context) {
	now := time.Now()

	meds, err := p.q.ListMedications(ctx)
	if err != nil {
		p.log.Warn("ada: reconcile list medications", slog.String("error", err.Error()))
		return
	}
	medActive := make(map[string]bool, len(meds))
	medName := make(map[string]string, len(meds))
	for _, m := range meds {
		medActive[m.ID] = m.Active
		medName[m.ID] = m.Name
	}

	routines, err := p.q.ListMedicationRoutines(ctx)
	if err != nil {
		p.log.Warn("ada: reconcile list routines", slog.String("error", err.Error()))
		return
	}

	// Recent events for slot/anchor/last-given lookups (max_doses uses a dedicated
	// count, not this window). 48h comfortably covers today's slots and any
	// not-yet-expired anchor (expiry is 24h).
	since := pgtype.Timestamptz{Time: now.Add(-48 * time.Hour).UTC(), Valid: true}
	events, err := p.q.ListRecentMedicationEvents(ctx, since)
	if err != nil {
		p.log.Warn("ada: reconcile list events", slog.String("error", err.Error()))
		return
	}
	eventByID := make(map[string]*store.ListRecentMedicationEventsRow, len(events))
	for _, e := range events {
		eventByID[e.ID] = e
	}

	changed := false

	for _, r := range routines {
		if r.Status == "completed" {
			continue
		}
		// Auto-complete (no phantom dose) takes precedence — a completed routine has
		// no live slots.
		if p.maybeCompleteRoutine(ctx, r, now) {
			changed = true
			continue
		}
		if r.ScheduleType != "fixed_times" || !medActive[r.MedicationID] {
			continue
		}
		// Keep stored `missed` rows in sync with the derived per-slot status.
		for _, slot := range r.FixedTimes {
			evStatus := slotEventStatus(events, r.ID, slot, now)
			st := slotStatus(evStatus, slot, r.FixedTimes, now)
			missedID := missedEventID(r.ID, slot, now)
			_, hasMissed := eventByID[missedID]
			switch {
			case st == slotMissed && !hasMissed:
				p.insertMissed(ctx, r, slot, now)
				changed = true
			case st != slotMissed && hasMissed:
				if err := p.q.SoftDeleteMedicationEvent(ctx, missedID); err != nil {
					p.log.Warn("ada: clear stale missed", slog.String("error", err.Error()))
				} else {
					changed = true
				}
			}
		}
	}

	series, err := p.q.ListActiveMedicationSeries(ctx)
	if err != nil {
		p.log.Warn("ada: reconcile list series", slog.String("error", err.Error()))
		series = nil
	}
	for _, s := range series {
		anchor, ok := eventByID[s.AnchorDoseID.String]
		// No anchor in the 48h window (>48h old, or the dose was deleted) → past the
		// 24h backstop, expire. Otherwise expire once the anchor crosses the backstop.
		if !ok || now.Sub(anchor.Timestamp.Time) > seriesExpiryBackstop {
			if err := p.q.EndMedicationSeries(ctx, &store.EndMedicationSeriesParams{
				ID: s.ID, Status: "expired", EndedReason: textFromString("auto_expire"),
			}); err != nil {
				p.log.Warn("ada: expire series", slog.String("error", err.Error()))
			} else {
				changed = true
			}
		}
	}

	p.remindDueMedications(ctx, now, routines, series, events, eventByID, medActive, medName)

	if changed {
		p.pushMedEventsSensor(ctx)
		p.pushMedicationSensors(ctx)
	}
}

// maybeCompleteRoutine sets status='completed' when the end rule is met, writing no
// dose event. Returns true if it completed the routine.
func (p *Processor) maybeCompleteRoutine(ctx context.Context, r *store.ListMedicationRoutinesRow, now time.Time) bool {
	complete := false
	switch r.EndType {
	case "max_doses":
		cnt, err := p.q.CountGivenForRoutine(ctx, textFromString(r.ID))
		if err != nil {
			p.log.Warn("ada: count given for routine", slog.String("error", err.Error()))
			return false
		}
		complete = routineComplete("max_doses", r.EndValue.String, int(cnt), now)
	case "end_date":
		complete = routineComplete("end_date", r.EndValue.String, 0, now)
	}
	if !complete {
		return false
	}
	if err := p.q.SetRoutineStatus(ctx, &store.SetRoutineStatusParams{ID: r.ID, Status: "completed"}); err != nil {
		p.log.Warn("ada: complete routine", slog.String("error", err.Error()))
		return false
	}
	return true
}

// insertMissed writes an actorless `missed` dose for a superseded slot. The id is
// deterministic per (routine, slot, local day) so the reconcile is idempotent.
func (p *Processor) insertMissed(ctx context.Context, r *store.ListMedicationRoutinesRow, slot string, now time.Time) {
	slotT, ok := medSlotTime(now, slot)
	if !ok {
		return
	}
	if err := p.q.InsertMedicationEvent(ctx, &store.InsertMedicationEventParams{
		ID:           missedEventID(r.ID, slot, now),
		MedicationID: r.MedicationID,
		Status:       "missed",
		Timestamp:    toTimestamptz(slotT.UTC()),
		RoutineID:    textFromString(r.ID),
		SlotTime:     textFromString(slot),
		LoggedBy:     "", // actorless
		Test:         r.Test || !p.born.Load(),
	}); err != nil {
		p.log.Warn("ada: insert missed", slog.String("error", err.Error()))
	}
}

func missedEventID(routineID, slot string, now time.Time) string {
	return fmt.Sprintf("missed-%s-%s-%s", routineID, slot, now.Format("20060102"))
}

// slotEventStatus returns the given/skipped event status for a routine's slot on the
// local calendar day, or "" if none. Unlike the dashboard's date-agnostic lookup, the
// server scopes to today so a dose given for the same slot on a prior day does not
// suppress today's missed detection.
func slotEventStatus(events []*store.ListRecentMedicationEventsRow, routineID, slot string, now time.Time) string {
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	dayEnd := dayStart.Add(24 * time.Hour)
	for _, e := range events {
		if e.RoutineID.String != routineID || e.SlotTime.String != slot {
			continue
		}
		if e.Status != "given" && e.Status != "skipped" {
			continue
		}
		t := e.Timestamp.Time.In(now.Location())
		if !t.Before(dayStart) && t.Before(dayEnd) {
			return e.Status
		}
	}
	return ""
}

// lastGivenForRoutine returns the most recent given dose for a routine, or nil.
func lastGivenForRoutine(events []*store.ListRecentMedicationEventsRow, routineID string) *store.ListRecentMedicationEventsRow {
	var last *store.ListRecentMedicationEventsRow
	for _, e := range events {
		if e.RoutineID.String != routineID || e.Status != "given" {
			continue
		}
		if last == nil || e.Timestamp.Time.After(last.Timestamp.Time) {
			last = e
		}
	}
	return last
}

// remindDueMedications pushes a "Medication due" notification to active caretakers
// (mirroring dispatchFeedingAlert) when a dose is due, de-duplicated via per-target
// markers in ada_config so a due is reminded once, not every tick.
func (p *Processor) remindDueMedications(
	ctx context.Context,
	now time.Time,
	routines []*store.ListMedicationRoutinesRow,
	series []*store.ListActiveMedicationSeriesRow,
	events []*store.ListRecentMedicationEventsRow,
	eventByID map[string]*store.ListRecentMedicationEventsRow,
	medActive map[string]bool,
	medName map[string]string,
) {
	type due struct{ medID, key, marker string }
	var dues []due

	for _, r := range routines {
		if r.Status == "completed" || !medActive[r.MedicationID] {
			continue
		}
		switch r.ScheduleType {
		case "fixed_times":
			for _, slot := range r.FixedTimes {
				if slotStatus(slotEventStatus(events, r.ID, slot, now), slot, r.FixedTimes, now) == slotDue {
					dues = append(dues, due{r.MedicationID, "medremind:" + r.ID + ":" + slot, now.Format("20060102")})
				}
			}
		case "interval":
			nd := now // computeDue: with no prior dose an interval routine is due now
			marker := "none"
			if last := lastGivenForRoutine(events, r.ID); last != nil {
				nd = last.Timestamp.Time.Add(time.Duration(numericToFloat(r.IntervalHours) * float64(medHour)))
				marker = last.ID
			}
			if !nd.After(now) {
				dues = append(dues, due{r.MedicationID, "medremind:" + r.ID + ":int", marker})
			}
		}
	}
	for _, s := range series {
		anchor, ok := eventByID[s.AnchorDoseID.String]
		if !ok {
			continue
		}
		if nd := seriesNextDue(anchor.Timestamp.Time, numericToFloat(s.IntervalHours)); !nd.After(now) {
			dues = append(dues, due{s.MedicationID, "medremind:series:" + s.ID, s.AnchorDoseID.String})
		}
	}
	if len(dues) == 0 {
		return
	}

	channels, err := p.q.GetActivePeopleWithChannels(ctx)
	if err != nil || len(channels) == 0 {
		return
	}
	for _, d := range dues {
		if cfg, err := p.q.GetConfig(ctx, d.key); err == nil && cfg.Value == d.marker {
			continue // already reminded for this due instance
		}
		name := medName[d.medID]
		body := fmt.Sprintf("%s is due.", name)
		for _, ch := range channels {
			if ch.Type != "ha_push" {
				continue
			}
			if err := p.ha.Notify(ctx, ch.Address, "Medication due 💊", body); err != nil {
				p.log.Warn("ada: medication due notify", slog.String("error", err.Error()))
			}
		}
		if err := p.q.UpsertConfig(ctx, &store.UpsertConfigParams{Key: d.key, Value: d.marker}); err != nil {
			p.log.Warn("ada: mark medication reminded", slog.String("error", err.Error()))
		}
	}
}
