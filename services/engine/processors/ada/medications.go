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

// Medication registry + dosing routines (ROADMAP-0011 effort 0011.1, ADR-0037).
// Registry and routines are standing config: they use eventTest(evt) only (no
// pre-birth forcing), so a real entry made before birth survives the clean-slate.
//
// ids are dashboard-provided strings (m-/rt-/... prefixed), not UUIDs — the
// dashboard is the id authority, so the engine stores them verbatim.

const (
	sensorMedications = "sensor.ada_medications"
	sensorMedRoutines = "sensor.ada_med_routines"
)

// handleMedicationUpsert persists (insert-or-update) a registry medication.
func (p *Processor) handleMedicationUpsert(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaMedicationUpsertData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode medication_upsert: %w", err)
	}
	if d.ID == "" {
		return fmt.Errorf("ada: medication_upsert missing id")
	}
	if err := p.q.UpsertMedication(ctx, &store.UpsertMedicationParams{
		ID:               d.ID,
		Name:             d.Name,
		Route:            d.Route,
		MeasureUnit:      d.MeasureUnit,
		MinIntervalHours: numericFromFloatPtr(d.MinIntervalHours),
		MaxPer24h:        int4FromIntPtr(d.MaxPer24h),
		Active:           d.Active,
		LoggedBy:         d.LoggedBy,
		Test:             eventTest(evt),
	}); err != nil {
		return fmt.Errorf("ada: upsert medication: %w", err)
	}
	p.pushMedicationSensors(ctx)
	return nil
}

// handleMedicationDelete soft-deletes a medication and cascades to its routines
// and any active series in one transaction (app-level cascade, ADR-0037 #2).
func (p *Processor) handleMedicationDelete(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaDeleteData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode medication_delete: %w", err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ada: begin medication_delete tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := p.q.WithTx(tx)
	if err := qtx.SoftDeleteMedication(ctx, d.ID); err != nil {
		return fmt.Errorf("ada: soft-delete medication: %w", err)
	}
	if err := qtx.SoftDeleteRoutinesForMedication(ctx, d.ID); err != nil {
		return fmt.Errorf("ada: soft-delete medication routines: %w", err)
	}
	if err := qtx.SoftDeleteSeriesForMedication(ctx, d.ID); err != nil {
		return fmt.Errorf("ada: soft-delete medication series: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ada: commit medication_delete: %w", err)
	}
	p.pushMedicationSensors(ctx)
	return nil
}

// handleMedicationRoutineUpsert persists (insert-or-update) a dosing routine.
func (p *Processor) handleMedicationRoutineUpsert(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaMedicationRoutineUpsertData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode medication_routine_upsert: %w", err)
	}
	if d.ID == "" || d.MedicationID == "" {
		return fmt.Errorf("ada: medication_routine_upsert missing id/medication_id")
	}
	fixedTimes := d.FixedTimes
	if fixedTimes == nil {
		fixedTimes = []string{} // NOT NULL column; never store NULL
	}
	endType := d.End.Type
	if endType == "" {
		endType = "none"
	}
	status := d.Status
	if status == "" {
		status = "active"
	}
	if err := p.q.UpsertMedicationRoutine(ctx, &store.UpsertMedicationRoutineParams{
		ID:            d.ID,
		MedicationID:  d.MedicationID,
		DoseAmount:    numericFromFloat(d.DoseAmount),
		ScheduleType:  d.ScheduleType,
		FixedTimes:    fixedTimes,
		IntervalHours: numericFromFloatPtr(d.IntervalHours),
		EndType:       endType,
		EndValue:      textFromString(endValueString(d.End.Value)),
		Status:        status,
		LoggedBy:      d.LoggedBy,
		Test:          eventTest(evt),
	}); err != nil {
		return fmt.Errorf("ada: upsert medication routine: %w", err)
	}
	p.pushMedicationSensors(ctx)
	return nil
}

// handleMedicationRoutineDelete soft-deletes a single routine.
func (p *Processor) handleMedicationRoutineDelete(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaDeleteData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode medication_routine_delete: %w", err)
	}
	if err := p.q.SoftDeleteRoutine(ctx, d.ID); err != nil {
		return fmt.Errorf("ada: soft-delete medication routine: %w", err)
	}
	p.pushMedicationSensors(ctx)
	return nil
}

// ── Projection ────────────────────────────────────────────────────────────────

// medicationItem mirrors the adaMeds.ts Medication type so the dashboard read-seam
// repoint is a pure binding swap. Nullable safety limits stay null, never zero.
type medicationItem struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Route            string   `json:"route"`
	MeasureUnit      string   `json:"measure_unit"`
	MinIntervalHours *float64 `json:"min_interval_hours"`
	MaxPer24h        *int     `json:"max_per_24h"`
	Active           bool     `json:"active"`
}

// routineEnd mirrors the nested {type, value?} end rule.
type routineEnd struct {
	Type  string `json:"type"`
	Value any    `json:"value,omitempty"`
}

// routineItem mirrors the adaMeds.ts Routine type.
type routineItem struct {
	ID            string     `json:"id"`
	MedicationID  string     `json:"medication_id"`
	DoseAmount    float64    `json:"dose_amount"`
	ScheduleType  string     `json:"schedule_type"`
	FixedTimes    []string   `json:"fixed_times"`
	IntervalHours *float64   `json:"interval_hours"`
	End           routineEnd `json:"end"`
	Status        string     `json:"status"`
}

// pushMedicationSensors re-projects the registry and routine sensors. Called after
// every mutation and on the periodic full sensor refresh. No-op when HA push is
// disabled (HA_INGEST_ENABLED=false, ADR-0033).
func (p *Processor) pushMedicationSensors(ctx context.Context) {
	p.pushMedications(ctx)
	p.pushMedRoutines(ctx)
}

func (p *Processor) pushMedications(ctx context.Context) {
	rows, err := p.q.ListMedications(ctx)
	if err != nil {
		p.log.Warn("ada: list medications", slog.String("error", err.Error()))
		return
	}
	items := make([]medicationItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, medicationItem{
			ID:               r.ID,
			Name:             r.Name,
			Route:            r.Route,
			MeasureUnit:      r.MeasureUnit,
			MinIntervalHours: numericToFloatPtr(r.MinIntervalHours),
			MaxPer24h:        int4ToIntPtr(r.MaxPer24h),
			Active:           r.Active,
		})
	}
	attrs := map[string]any{"items": items, "last_updated": time.Now().UTC().Format(time.RFC3339)}
	if err := p.ha.PushState(ctx, sensorMedications, strconv.Itoa(len(items)), attrs); err != nil {
		p.log.Warn("ada: push medications", slog.String("error", err.Error()))
	}
}

func (p *Processor) pushMedRoutines(ctx context.Context) {
	rows, err := p.q.ListMedicationRoutines(ctx)
	if err != nil {
		p.log.Warn("ada: list medication routines", slog.String("error", err.Error()))
		return
	}
	items := make([]routineItem, 0, len(rows))
	for _, r := range rows {
		ft := r.FixedTimes
		if ft == nil {
			ft = []string{}
		}
		items = append(items, routineItem{
			ID:            r.ID,
			MedicationID:  r.MedicationID,
			DoseAmount:    numericToFloat(r.DoseAmount),
			ScheduleType:  r.ScheduleType,
			FixedTimes:    ft,
			IntervalHours: numericToFloatPtr(r.IntervalHours),
			End:           routineEnd{Type: r.EndType, Value: endValueParse(r.EndType, r.EndValue.String)},
			Status:        r.Status,
		})
	}
	attrs := map[string]any{"items": items, "last_updated": time.Now().UTC().Format(time.RFC3339)}
	if err := p.ha.PushState(ctx, sensorMedRoutines, strconv.Itoa(len(items)), attrs); err != nil {
		p.log.Warn("ada: push medication routines", slog.String("error", err.Error()))
	}
}

// ── Conversion helpers ──────────────────────────────────────────────────────────

// numericToFloatPtr converts a nullable NUMERIC to *float64 at full precision
// (numericToFloatOk rounds to 0.1, which would corrupt doses like 1.25).
func numericToFloatPtr(n pgtype.Numeric) *float64 {
	if !n.Valid {
		return nil
	}
	f8, err := n.Float64Value()
	if err != nil || !f8.Valid {
		return nil
	}
	f := f8.Float64
	return &f
}

func int4FromIntPtr(v *int) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(*v), Valid: true} //nolint:gosec // G115: a per-24h dose ceiling is a tiny count, never near int32 max
}

func int4ToIntPtr(v pgtype.Int4) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int32)
	return &i
}

func textFromString(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

// endValueString stringifies a routine end value (number for max_doses, date
// string for end_date) for storage in the end_value TEXT column.
func endValueString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// endValueParse reconstructs the typed end value for projection: a number for
// max_doses, the raw string otherwise.
func endValueParse(endType, s string) any {
	if s == "" {
		return nil
	}
	if endType == "max_doses" {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	return s
}
