package schemas

// Ada event subject constants — NATS subjects for ha.events.ada.> events.
const (
	AdaEventFeedingEnded        = "ha.events.ada.feeding_ended"
	AdaEventFeedingLogged       = "ha.events.ada.feeding_logged"
	AdaEventFeedingSupplemented = "ha.events.ada.feeding_supplement_logged"
	AdaEventDiaperLogged        = "ha.events.ada.diaper_logged"
	AdaEventSleepStarted        = "ha.events.ada.sleep_started"
	AdaEventSleepEnded          = "ha.events.ada.sleep_ended"
	AdaEventSleepLogged         = "ha.events.ada.sleep_logged"
	AdaEventTummyEnded          = "ha.events.ada.tummy_ended"
	AdaEventTummyLogged         = "ha.events.ada.tummy_logged"
	AdaEventFeedingLoggedPast   = "ha.events.ada.feeding_logged_past"
	AdaEventBorn                = "ha.events.ada.born"
	AdaEventSyncUsers           = "ha.events.ada.sync_users"
	AdaEventUsersSynced         = "ha.events.ada.users_synced"
	AdaEventCaretakerUpdate     = "ha.events.ada.caretaker_update"
	AdaEventTummyTarget         = "ha.events.ada.config_tummy_target"
	AdaEventAddChannel          = "ha.events.ada.add_channel"
	AdaEventRemoveChannel       = "ha.events.ada.remove_channel"
	AdaEventBedtimeConfig       = "ha.events.ada.config_bedtime"
	AdaEventGrowthLogged        = "ha.events.ada.growth_logged"
	AdaEventFeedingClaimed      = "ha.events.ada.feeding_claimed"
	AdaEventTrendsQuery         = "ha.events.ada.trends_query"

	// Edit/delete (#77, #78, #79). Updates are full-resolution replacements.
	AdaEventFeedingUpdate = "ha.events.ada.feeding_updated"
	AdaEventFeedingDelete = "ha.events.ada.feeding_deleted"
	AdaEventDiaperUpdate  = "ha.events.ada.diaper_updated"
	AdaEventDiaperDelete  = "ha.events.ada.diaper_deleted"
	AdaEventSleepUpdate   = "ha.events.ada.sleep_updated"
	AdaEventSleepDelete   = "ha.events.ada.sleep_deleted"
	AdaEventTummyUpdate   = "ha.events.ada.tummy_updated"
	AdaEventTummyDelete   = "ha.events.ada.tummy_deleted"
	AdaEventGrowthUpdate  = "ha.events.ada.growth_updated"
	AdaEventGrowthDelete  = "ha.events.ada.growth_deleted"

	// Medications & Emergency (ROADMAP-0011, ADR-0037). Registry + routines are
	// standing config; dose events + emergency rows land in later efforts.
	AdaEventMedicationUpsert        = "ha.events.ada.medication_upsert"
	AdaEventMedicationDelete        = "ha.events.ada.medication_delete"
	AdaEventMedicationRoutineUpsert = "ha.events.ada.medication_routine_upsert"
	AdaEventMedicationRoutineDelete = "ha.events.ada.medication_routine_delete"

	// Dose events + temporary series (effort 0011.2). given/skipped are
	// caregiver-logged doses; series start/end bracket an as-needed watch.
	AdaEventMedicationGiven       = "ha.events.ada.medication_given"
	AdaEventMedicationSkipped     = "ha.events.ada.medication_skipped"
	AdaEventMedicationSeriesStart = "ha.events.ada.medication_series_start"
	AdaEventMedicationSeriesEnd   = "ha.events.ada.medication_series_end"
	AdaEventMedicationEventUpdate = "ha.events.ada.medication_event_update"
	AdaEventMedicationEventDelete = "ha.events.ada.medication_event_delete"
)

// AdaDeleteData is the payload for every ada.<area>.delete event — a single id.
// Also used by ada.medication.delete and ada.medication.routine.delete.
type AdaDeleteData struct {
	ID       string `json:"id"`
	LoggedBy string `json:"logged_by,omitempty"`
}

// AdaMedicationUpsertData is the medication-registry payload (ada.medication.upsert).
// Identity + optional safety limits only — no dose, no schedule (those live on the
// routine). min_interval_hours and max_per_24h are nullable, so pointers distinguish
// "no limit" from zero.
type AdaMedicationUpsertData struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Route            string   `json:"route"`        // oral|drops|topical|suppository
	MeasureUnit      string   `json:"measure_unit"` // mL|mg|drops|supp
	MinIntervalHours *float64 `json:"min_interval_hours"`
	MaxPer24h        *int     `json:"max_per_24h"`
	Active           bool     `json:"active"`
	LoggedBy         string   `json:"logged_by,omitempty"`
}

// AdaRoutineEnd is the nested `end` rule on a routine ({type, value?}). Value is a
// number for max_doses or a date string for end_date; absent for none.
type AdaRoutineEnd struct {
	Type  string `json:"type"` // none|max_doses|end_date
	Value any    `json:"value,omitempty"`
}

// AdaMedicationRoutineUpsertData is the dosing-routine payload
// (ada.medication.routine.upsert) — the standing dose + cadence + end rule.
type AdaMedicationRoutineUpsertData struct {
	ID            string        `json:"id"`
	MedicationID  string        `json:"medication_id"`
	DoseAmount    float64       `json:"dose_amount"`
	ScheduleType  string        `json:"schedule_type"` // fixed_times|interval
	FixedTimes    []string      `json:"fixed_times"`
	IntervalHours *float64      `json:"interval_hours"`
	End           AdaRoutineEnd `json:"end"`
	Status        string        `json:"status"` // active|completed
	LoggedBy      string        `json:"logged_by,omitempty"`
}

// AdaMedicationEventData is the dose-event payload (ada.medication.given / skipped) —
// the full MedEvent. Only `given` carries the dose snapshot; `skipped` carries none.
// Actor is the caregiver (== logged_by); system-emitted `missed` rows are actorless.
type AdaMedicationEventData struct {
	ID                   string   `json:"id"`
	MedicationID         string   `json:"medication_id"`
	Status               string   `json:"status"` // given|skipped
	Timestamp            string   `json:"timestamp"`
	RoutineID            string   `json:"routine_id,omitempty"`
	SlotTime             string   `json:"slot_time,omitempty"`
	Actor                string   `json:"actor,omitempty"`
	DoseAmount           *float64 `json:"dose_amount,omitempty"`
	DoseUnit             string   `json:"dose_unit,omitempty"`
	Source               string   `json:"source,omitempty"` // scheduled|prn
	WithinWindowOverride bool     `json:"within_window_override,omitempty"`
	SeriesID             string   `json:"series_id,omitempty"`
	StartedWatch         bool     `json:"started_watch,omitempty"`
	Notes                string   `json:"notes,omitempty"`
	LoggedBy             string   `json:"logged_by,omitempty"`
}

// AdaMedicationSeriesStartData opens an as-needed watch anchored to a given dose.
type AdaMedicationSeriesStartData struct {
	ID            string  `json:"id"`
	MedicationID  string  `json:"medication_id"`
	IntervalHours float64 `json:"interval_hours"`
	AnchorDoseID  string  `json:"anchor_dose_id"`
	LoggedBy      string  `json:"logged_by,omitempty"`
}

// AdaMedicationSeriesEndData closes a watch. EndedReason ∈ planned | dismissed.
type AdaMedicationSeriesEndData struct {
	ID           string `json:"id"`
	MedicationID string `json:"medication_id"`
	EndedReason  string `json:"ended_reason"`
	LoggedBy     string `json:"logged_by,omitempty"`
}

// AdaMedicationEventUpdateData is a history dose correction (timestamp + amount).
type AdaMedicationEventUpdateData struct {
	ID         string   `json:"id"`
	Timestamp  string   `json:"timestamp"`
	DoseAmount *float64 `json:"dose_amount"`
	LoggedBy   string   `json:"logged_by,omitempty"`
}

// AdaFeedingUpdateData is the full-resolution replacement payload for a feeding (#79).
// Any combination of components is valid; a zero-valued field clears that component.
// Breast timing is in seconds; bottle amounts are in oz. The source is re-derived
// from which fields are non-zero, exactly as a fresh submission would be.
type AdaFeedingUpdateData struct {
	ID           string  `json:"id"`
	StartTime    string  `json:"start_time"`
	LeftBreastS  int     `json:"left_breast_s,omitempty"`
	RightBreastS int     `json:"right_breast_s,omitempty"`
	BreastMilkOz float64 `json:"breast_milk_oz,omitempty"`
	FormulaOz    float64 `json:"formula_oz,omitempty"`
	LoggedBy     string  `json:"logged_by,omitempty"`
}

// AdaDiaperUpdateData replaces a diaper event by id. Type ∈ wet | dirty | mixed.
type AdaDiaperUpdateData struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	LoggedBy  string `json:"logged_by,omitempty"`
}

// AdaSleepUpdateData replaces a sleep session by id. SleepType ∈ nap | night.
type AdaSleepUpdateData struct {
	ID        string `json:"id"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	DurationS int    `json:"duration_s"`
	SleepType string `json:"sleep_type"`
	LoggedBy  string `json:"logged_by,omitempty"`
}

// AdaTummyUpdateData replaces a tummy time session by id.
type AdaTummyUpdateData struct {
	ID        string `json:"id"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	DurationS int    `json:"duration_s"`
	LoggedBy  string `json:"logged_by,omitempty"`
}

// AdaGrowthUpdateData replaces a growth measurement by id. Optional metric fields
// use pointers — nil means "not present" (cleared). Percentiles are recomputed.
type AdaGrowthUpdateData struct {
	ID                  string   `json:"id"`
	WeightOz            *float64 `json:"weight_oz,omitempty"`
	LengthIn            *float64 `json:"length_in,omitempty"`
	HeadCircumferenceIn *float64 `json:"head_circumference_in,omitempty"`
	Source              string   `json:"source"`
	Timestamp           string   `json:"timestamp"`
	LoggedBy            string   `json:"logged_by,omitempty"`
}

// AdaFeedingEndedData is the CloudEvent Data payload for a breast feeding session end.
// Segments hold per-side timing; totals are pre-computed by the dashboard as a fallback.
// StartTime is the RFC3339 session start provided by the frontend clock; if absent, the
// processor falls back to evtTime - TotalDurationS.
type AdaFeedingEndedData struct {
	Segments       []AdaFeedingSegment `json:"segments"`
	TotalDurationS int                 `json:"total_duration_s"`
	LeftTotalS     int                 `json:"left_total_s"`
	RightTotalS    int                 `json:"right_total_s"`
	StartTime      string              `json:"start_time,omitempty"`
	LoggedBy       string              `json:"logged_by,omitempty"`
}

// AdaFeedingSegment is a single side + duration record within a breast feeding session.
type AdaFeedingSegment struct {
	Side      string `json:"side"`
	DurationS int    `json:"duration_s"`
}

// AdaFeedingLoggedData is the payload for a logged bottle or past breast feeding.
// AmountOz is set for single-source bottles; BreastMilkOz and FormulaOz for mixed.
// All amounts are in oz — the native unit at point of entry.
type AdaFeedingLoggedData struct {
	Source       string  `json:"source"`
	AmountOz     float64 `json:"amount_oz,omitempty"`      // single-source bottle
	BreastMilkOz float64 `json:"breast_milk_oz,omitempty"` // mixed bottle
	FormulaOz    float64 `json:"formula_oz,omitempty"`     // mixed bottle
	StartTime    string  `json:"start_time"`               // RFC3339; actual event time
	LoggedBy     string  `json:"logged_by,omitempty"`
}

// AdaFeedingSupplementData is the payload for a supplemental bottle logged mid-session.
// All amounts are in oz at point of entry. A single-source supplement
// (source "breast_milk" or "formula") carries AmountOz; a mixed supplement
// (source "mixed") carries BreastMilkOz and FormulaOz directly. The processor
// routes the amounts onto the correct named columns of the parent feed (#74).
type AdaFeedingSupplementData struct {
	Source       string  `json:"source"`
	AmountOz     float64 `json:"amount_oz,omitempty"`      // single-source supplement
	BreastMilkOz float64 `json:"breast_milk_oz,omitempty"` // mixed supplement
	FormulaOz    float64 `json:"formula_oz,omitempty"`     // mixed supplement
	LoggedBy     string  `json:"logged_by,omitempty"`
}

// AdaDiaperLoggedData is the payload for a logged diaper change.
// Timestamp is RFC3339; actual event time (may be backdated by the dashboard).
type AdaDiaperLoggedData struct {
	Type      string `json:"type"`      // "wet", "dirty", or "mixed"
	Timestamp string `json:"timestamp"` // RFC3339; actual event time
	LoggedBy  string `json:"logged_by,omitempty"`
}

// AdaSleepStartedData is the payload for a sleep session start.
// StartTime is RFC3339; actual event time.
type AdaSleepStartedData struct {
	StartTime string `json:"start_time"` // RFC3339
	SleepType string `json:"sleep_type"` // "nap" or "night"
	LoggedBy  string `json:"logged_by,omitempty"`
}

// AdaSleepEndedData is the payload for a sleep session end.
// EndTime is RFC3339; actual event time.
type AdaSleepEndedData struct {
	EndTime   string `json:"end_time"` // RFC3339
	DurationS int    `json:"duration_s"`
	LoggedBy  string `json:"logged_by,omitempty"`
}

// AdaSleepLoggedData is the payload for a past sleep session logged in one step.
type AdaSleepLoggedData struct {
	StartTime string `json:"start_time"` // RFC3339; actual event time
	EndTime   string `json:"end_time"`   // RFC3339; actual event time
	DurationS int    `json:"duration_s"`
	SleepType string `json:"sleep_type"`
	LoggedBy  string `json:"logged_by,omitempty"`
}

// AdaTummyEndedData is the payload for an active tummy time session end.
type AdaTummyEndedData struct {
	StartTime string `json:"start_time"` // RFC3339; actual event time
	EndTime   string `json:"end_time"`   // RFC3339; actual event time
	DurationS int    `json:"duration_s"`
	LoggedBy  string `json:"logged_by,omitempty"`
}

// AdaTummyLoggedData is the payload for a past tummy time session logged in one step.
type AdaTummyLoggedData struct {
	StartTime string `json:"start_time"` // RFC3339; actual event time
	EndTime   string `json:"end_time"`   // RFC3339; actual event time
	DurationS int    `json:"duration_s"`
	LoggedBy  string `json:"logged_by,omitempty"`
}

// AdaHAUser represents one HA user returned by the gateway user sync.
type AdaHAUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

// AdaUsersSyncedData is published by the gateway after querying HA users.
// AvailableServices is the full list of mobile_app_* notify service names
// discovered from HA, used to populate the device picker in the config screen.
type AdaUsersSyncedData struct {
	Users             []AdaHAUser `json:"users"`
	AvailableServices []string    `json:"available_services"`
}

// AdaCaretakerUpdateData is fired by the HA config screen on caretaker toggle.
type AdaCaretakerUpdateData struct {
	HAUserID    string `json:"ha_user_id"`
	IsCaretaker bool   `json:"is_caretaker"`
	LoggedBy    string `json:"logged_by,omitempty"`
}

// AdaAddChannelData is fired when a user adds a notification channel to a person.
type AdaAddChannelData struct {
	HAUserID string `json:"ha_user_id"` // identifies the person
	Type     string `json:"type"`       // "ha_push" | "sms"
	Address  string `json:"address"`    // mobile_app_* service name or E.164 phone number
	Label    string `json:"label,omitempty"`
	LoggedBy string `json:"logged_by,omitempty"`
}

// AdaRemoveChannelData is fired when a user removes a notification channel.
type AdaRemoveChannelData struct {
	ChannelID string `json:"channel_id"` // UUID of the person_channels row
	HAUserID  string `json:"ha_user_id"` // for ownership verification
	LoggedBy  string `json:"logged_by,omitempty"`
}

// AdaTummyTargetData is fired by the HA config screen on tummy time target save.
type AdaTummyTargetData struct {
	TargetMin int    `json:"target_min"`
	LoggedBy  string `json:"logged_by,omitempty"`
}

// AdaBornData is the payload for the ada.born event fired when birth is confirmed.
// birth_at is a full RFC3339 timestamp constructed by the browser so that the
// user's local timezone is preserved — no server-side timezone inference needed.
type AdaBornData struct {
	BirthAt  string `json:"birth_at"` // RFC3339, e.g. "2026-04-19T14:32:00-05:00"
	LoggedBy string `json:"logged_by,omitempty"`
}

// AdaBedtimeConfigData is the payload for the ada.config.bedtime event.
// GraceMin of 0 means "keep the existing value" — only update bedtime/daytime.
type AdaBedtimeConfigData struct {
	BedtimeHHMM string `json:"bedtime_hhmm"`
	DaytimeHHMM string `json:"daytime_hhmm"`
	GraceMin    int    `json:"grace_min,omitempty"` // 0 means "keep current value"
	LoggedBy    string `json:"logged_by,omitempty"`
}

// AdaFeedingLoggedPastData is the payload for a past feeding logged in one step
// via the ada.feeding.log_past frontend event. It combines breast timing and/or
// bottle amounts. Timing fields are in seconds; liquid amounts are in millilitres
// and converted to oz at ingestion. Source is derived from which fields are non-zero.
type AdaFeedingLoggedPastData struct {
	StartTime    string  `json:"start_time"`
	LeftBreastS  int     `json:"left_breast_s,omitempty"`
	RightBreastS int     `json:"right_breast_s,omitempty"`
	BreastMilkML float64 `json:"breast_milk_ml,omitempty"`
	FormulaML    float64 `json:"formula_ml,omitempty"`
	LoggedBy     string  `json:"logged_by,omitempty"`
}

// AdaFeedingClaimedData is the payload for ada.feeding.claimed, fired when a
// caregiver taps "I've got it" on a feed-due alert. GotItUser is the caregiver's
// display name; ruby-core owns the claim lifecycle and projects it to
// sensor.ada_feeding_claimed_by + input_boolean.ada_feeding_claimed (#81).
type AdaFeedingClaimedData struct {
	GotItUser string `json:"got_it_user"`
	Timestamp string `json:"timestamp,omitempty"`
	LoggedBy  string `json:"logged_by,omitempty"`
}

// AdaTrendsQueryData is the request for a bucketed activity aggregation (#82, ADR-0032).
// The dashboard mints an opaque RequestID per query; ruby-core echoes it in
// sensor.ada_trends so the dashboard renders only the response to its latest request.
// Metric ∈ diapers|feeding|sleep|tummy; View depends on Metric; Period ∈ week|month|year.
type AdaTrendsQueryData struct {
	Metric    string `json:"metric"`
	View      string `json:"view"`
	Period    string `json:"period"`
	RequestID string `json:"request_id"`
}

// AdaGrowthLoggedData is the payload for a logged growth measurement.
// Optional fields use pointer types — nil means "not provided," not zero.
// A weight-only entry has nil LengthIn and HeadCircumferenceIn.
// Timestamp is the measurement date/time (RFC3339) and supports backdating.
// Source is "home" or "pediatrician".
type AdaGrowthLoggedData struct {
	WeightOz            *float64 `json:"weight_oz,omitempty"`
	LengthIn            *float64 `json:"length_in,omitempty"`
	HeadCircumferenceIn *float64 `json:"head_circumference_in,omitempty"`
	Source              string   `json:"source"`
	Timestamp           string   `json:"timestamp"`
	LoggedBy            string   `json:"logged_by,omitempty"`
}
