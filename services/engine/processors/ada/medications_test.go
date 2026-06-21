//go:build fast

package ada

import (
	"testing"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

// The routine `end` rule arrives nested ({type, value?}) with value as a number
// (max_doses) or a date string (end_date); it is stored as end_type + end_value
// TEXT and reconstructed on projection. These round-trips must be lossless.
func TestEndValueRoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		endType  string
		value    any
		wantStr  string
		wantBack any
	}{
		{"none", "none", nil, "", nil},
		{"max_doses int", "max_doses", float64(10), "10", float64(10)},
		{"max_doses fractional", "max_doses", float64(2.5), "2.5", float64(2.5)},
		{"end_date", "end_date", "2026-07-01", "2026-07-01", "2026-07-01"},
		{"string number stays string for end_date", "end_date", "3", "3", "3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotStr := endValueString(c.value)
			if gotStr != c.wantStr {
				t.Fatalf("endValueString(%v) = %q, want %q", c.value, gotStr, c.wantStr)
			}
			gotBack := endValueParse(c.endType, gotStr)
			if gotBack != c.wantBack {
				t.Errorf("endValueParse(%q, %q) = %#v, want %#v", c.endType, gotStr, gotBack, c.wantBack)
			}
		})
	}
}

// The routine payload decodes the nested end object and nullable interval; a fixed
// schedule carries fixed_times and a null interval, an interval schedule the inverse.
func TestRoutineUpsertDecode(t *testing.T) {
	data := map[string]any{
		"id":            "33333333-3333-3333-3333-333333333333",
		"medication_id": "11111111-1111-1111-1111-111111111111",
		"dose_amount":   1.25,
		"schedule_type": "interval",
		"fixed_times":   []any{},
		"interval_hours": 6,
		"end":           map[string]any{"type": "max_doses", "value": 10},
		"status":        "active",
		"logged_by":     "katie",
	}
	var d schemas.AdaMedicationRoutineUpsertData
	if err := remarshal(data, &d); err != nil {
		t.Fatalf("remarshal: %v", err)
	}
	if d.DoseAmount != 1.25 {
		t.Errorf("dose_amount = %v, want 1.25", d.DoseAmount)
	}
	if d.ScheduleType != "interval" {
		t.Errorf("schedule_type = %q, want interval", d.ScheduleType)
	}
	if d.IntervalHours == nil || *d.IntervalHours != 6 {
		t.Errorf("interval_hours = %v, want 6", d.IntervalHours)
	}
	if d.End.Type != "max_doses" || endValueString(d.End.Value) != "10" {
		t.Errorf("end = %+v, want {max_doses 10}", d.End)
	}
	if len(d.FixedTimes) != 0 {
		t.Errorf("fixed_times = %v, want empty", d.FixedTimes)
	}
}

// A `given` dose carries the full MedEvent incl. the dose snapshot; a `skipped`
// one carries no dose. Optional fields must round-trip, dose_amount as nullable.
func TestMedicationEventDecode(t *testing.T) {
	var given schemas.AdaMedicationEventData
	if err := remarshal(map[string]any{
		"id": "ev-1718900000000", "medication_id": "m-1", "status": "given",
		"timestamp": "2026-06-20T16:00:00Z", "actor": "Katie",
		"dose_amount": 1.25, "dose_unit": "mL", "source": "prn",
		"series_id": "s-1", "started_watch": true,
	}, &given); err != nil {
		t.Fatalf("remarshal given: %v", err)
	}
	if given.Status != "given" || given.DoseAmount == nil || *given.DoseAmount != 1.25 {
		t.Errorf("given decode wrong: status=%q dose=%v", given.Status, given.DoseAmount)
	}
	if given.Source != "prn" || given.SeriesID != "s-1" || !given.StartedWatch {
		t.Errorf("given optional fields wrong: %+v", given)
	}

	var skipped schemas.AdaMedicationEventData
	if err := remarshal(map[string]any{
		"id": "ev-2", "medication_id": "m-1", "status": "skipped",
		"timestamp": "2026-06-20T18:00:00Z", "actor": "Michael",
		"routine_id": "rt-1", "slot_time": "18:00",
	}, &skipped); err != nil {
		t.Fatalf("remarshal skipped: %v", err)
	}
	if skipped.Status != "skipped" || skipped.DoseAmount != nil {
		t.Errorf("skipped should carry no dose: %+v", skipped)
	}
	if skipped.RoutineID != "rt-1" || skipped.SlotTime != "18:00" {
		t.Errorf("skipped slot fields wrong: %+v", skipped)
	}
}

// A medication's nullable safety limits must decode as nil (no limit), never zero.
func TestMedicationUpsertNullableLimits(t *testing.T) {
	var d schemas.AdaMedicationUpsertData
	if err := remarshal(map[string]any{
		"id": "x", "name": "Vitamin D", "route": "drops", "measure_unit": "drops",
		"min_interval_hours": nil, "max_per_24h": nil, "active": true,
	}, &d); err != nil {
		t.Fatalf("remarshal: %v", err)
	}
	if d.MinIntervalHours != nil {
		t.Errorf("min_interval_hours = %v, want nil", *d.MinIntervalHours)
	}
	if d.MaxPer24h != nil {
		t.Errorf("max_per_24h = %v, want nil", *d.MaxPer24h)
	}
	if !d.Active {
		t.Error("active = false, want true")
	}
}
