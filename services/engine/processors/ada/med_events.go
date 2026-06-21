package ada

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/store"
)

// Medication dose events + temporary series (ROADMAP-0011 effort 0011.2, ADR-0037).
// Dose events are TRACKING: they use eventTestOrPreBirth(evt), so all pre-birth
// practice doses are test=true and wiped at birth. given/skipped are
// caregiver-logged; system-emitted `missed` arrives in effort 0011.3.

const (
	sensorMedEvents = "sensor.ada_med_events"
	// medEventsWindow bounds the projected event history. 7 days comfortably
	// covers today's timeline plus any guard-relevant last dose (min-interval is
	// hours, never days) without projecting unbounded history.
	medEventsWindow = 7 * 24 * time.Hour
)

// handleMedicationEvent persists a given/skipped dose. The dose snapshot
// (dose_amount + dose_unit) records what was actually administered.
func (p *Processor) handleMedicationEvent(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaMedicationEventData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode medication event: %w", err)
	}
	if d.ID == "" || d.MedicationID == "" {
		return fmt.Errorf("ada: medication event missing id/medication_id")
	}
	ts, err := parseRFC3339(d.Timestamp)
	if err != nil {
		return fmt.Errorf("ada: parse medication event timestamp: %w", err)
	}
	status := d.Status
	if status == "" { // fall back to the event type if the payload omitted it
		if evt.Type == schemas.AdaEventMedicationSkipped {
			status = "skipped"
		} else {
			status = "given"
		}
	}
	actor := d.Actor
	if actor == "" {
		actor = d.LoggedBy
	}
	if err := p.q.InsertMedicationEvent(ctx, &store.InsertMedicationEventParams{
		ID:                   d.ID,
		MedicationID:         d.MedicationID,
		Status:               status,
		Timestamp:            toTimestamptz(ts),
		RoutineID:            textFromString(d.RoutineID),
		SlotTime:             textFromString(d.SlotTime),
		DoseAmount:           numericFromFloatPtr(d.DoseAmount),
		DoseUnit:             textFromString(d.DoseUnit),
		Source:               textFromString(d.Source),
		WithinWindowOverride: d.WithinWindowOverride,
		SeriesID:             textFromString(d.SeriesID),
		StartedWatch:         d.StartedWatch,
		Notes:                textFromString(d.Notes),
		LoggedBy:             actor,
		Test:                 p.eventTestOrPreBirth(evt),
	}); err != nil {
		return fmt.Errorf("ada: insert medication event: %w", err)
	}
	p.pushMedEventsSensor(ctx)
	return nil
}

// handleMedicationSeriesStart opens an as-needed watch anchored to a given dose.
func (p *Processor) handleMedicationSeriesStart(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaMedicationSeriesStartData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode medication_series_start: %w", err)
	}
	if d.ID == "" || d.MedicationID == "" {
		return fmt.Errorf("ada: medication_series_start missing id/medication_id")
	}
	if err := p.q.InsertMedicationSeries(ctx, &store.InsertMedicationSeriesParams{
		ID:            d.ID,
		MedicationID:  d.MedicationID,
		IntervalHours: numericFromFloat(d.IntervalHours),
		AnchorDoseID:  textFromString(d.AnchorDoseID),
		LoggedBy:      d.LoggedBy,
		Test:          p.eventTestOrPreBirth(evt),
	}); err != nil {
		return fmt.Errorf("ada: insert medication series: %w", err)
	}
	p.pushMedEventsSensor(ctx)
	return nil
}

// handleMedicationSeriesEnd closes a watch: planned → resolved, dismissed → disregarded.
func (p *Processor) handleMedicationSeriesEnd(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaMedicationSeriesEndData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode medication_series_end: %w", err)
	}
	status := "resolved"
	if d.EndedReason == "dismissed" {
		status = "disregarded"
	}
	if err := p.q.EndMedicationSeries(ctx, &store.EndMedicationSeriesParams{
		ID:          d.ID,
		Status:      status,
		EndedReason: textFromString(d.EndedReason),
	}); err != nil {
		return fmt.Errorf("ada: end medication series: %w", err)
	}
	p.pushMedEventsSensor(ctx)
	return nil
}

// handleMedEventUpdate corrects a logged dose (timestamp + amount); identity and
// the rest of the snapshot are immutable.
func (p *Processor) handleMedEventUpdate(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaMedicationEventUpdateData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode medication_event_update: %w", err)
	}
	ts, err := parseRFC3339(d.Timestamp)
	if err != nil {
		return fmt.Errorf("ada: parse medication_event_update timestamp: %w", err)
	}
	if err := p.q.UpdateMedicationEvent(ctx, &store.UpdateMedicationEventParams{
		ID:         d.ID,
		Timestamp:  toTimestamptz(ts),
		DoseAmount: numericFromFloatPtr(d.DoseAmount),
	}); err != nil {
		return fmt.Errorf("ada: update medication event: %w", err)
	}
	p.pushMedEventsSensor(ctx)
	return nil
}

// handleMedEventDelete soft-deletes a logged dose.
func (p *Processor) handleMedEventDelete(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaDeleteData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode medication_event_delete: %w", err)
	}
	if err := p.q.SoftDeleteMedicationEvent(ctx, d.ID); err != nil {
		return fmt.Errorf("ada: soft-delete medication event: %w", err)
	}
	p.pushMedEventsSensor(ctx)
	return nil
}

// ── Projection ────────────────────────────────────────────────────────────────

// medEventItem mirrors the adaMeds.ts MedEvent type (logged_by surfaces as `actor`).
type medEventItem struct {
	ID                   string   `json:"id"`
	MedicationID         string   `json:"medication_id"`
	Status               string   `json:"status"`
	Timestamp            string   `json:"timestamp"`
	RoutineID            string   `json:"routine_id,omitempty"`
	SlotTime             string   `json:"slot_time,omitempty"`
	Actor                string   `json:"actor,omitempty"`
	DoseAmount           *float64 `json:"dose_amount,omitempty"`
	DoseUnit             string   `json:"dose_unit,omitempty"`
	Source               string   `json:"source,omitempty"`
	WithinWindowOverride bool     `json:"within_window_override,omitempty"`
	SeriesID             string   `json:"series_id,omitempty"`
	StartedWatch         bool     `json:"started_watch,omitempty"`
}

// seriesItem mirrors the adaMeds.ts TemporarySeries type. Carried as the `series`
// attribute on sensor.ada_med_events (no dedicated series sensor in the contract).
type seriesItem struct {
	ID            string  `json:"id"`
	MedicationID  string  `json:"medication_id"`
	IntervalHours float64 `json:"interval_hours"`
	AnchorDoseID  string  `json:"anchor_dose_id"`
	Status        string  `json:"status"`
}

// pushMedEventsSensor projects the recent dose events + active watches to
// sensor.ada_med_events. No-op when HA push is disabled (ADR-0033).
func (p *Processor) pushMedEventsSensor(ctx context.Context) {
	since := pgtype.Timestamptz{Time: time.Now().UTC().Add(-medEventsWindow), Valid: true}
	rows, err := p.q.ListRecentMedicationEvents(ctx, since)
	if err != nil {
		p.log.Warn("ada: list medication events", slog.String("error", err.Error()))
		return
	}
	items := make([]medEventItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, medEventItem{
			ID:                   r.ID,
			MedicationID:         r.MedicationID,
			Status:               r.Status,
			Timestamp:            r.Timestamp.Time.UTC().Format(time.RFC3339),
			RoutineID:            r.RoutineID.String,
			SlotTime:             r.SlotTime.String,
			Actor:                r.LoggedBy,
			DoseAmount:           numericToFloatPtr(r.DoseAmount),
			DoseUnit:             r.DoseUnit.String,
			Source:               r.Source.String,
			WithinWindowOverride: r.WithinWindowOverride,
			SeriesID:             r.SeriesID.String,
			StartedWatch:         r.StartedWatch,
		})
	}

	seriesRows, err := p.q.ListActiveMedicationSeries(ctx)
	if err != nil {
		p.log.Warn("ada: list active medication series", slog.String("error", err.Error()))
		seriesRows = nil
	}
	series := make([]seriesItem, 0, len(seriesRows))
	for _, s := range seriesRows {
		series = append(series, seriesItem{
			ID:            s.ID,
			MedicationID:  s.MedicationID,
			IntervalHours: numericToFloat(s.IntervalHours),
			AnchorDoseID:  s.AnchorDoseID.String,
			Status:        s.Status,
		})
	}

	attrs := map[string]any{
		"items":        items,
		"series":       series,
		"last_updated": time.Now().UTC().Format(time.RFC3339),
	}
	if err := p.ha.PushState(ctx, sensorMedEvents, strconv.Itoa(len(items)), attrs); err != nil {
		p.log.Warn("ada: push medication events", slog.String("error", err.Error()))
	}
}
