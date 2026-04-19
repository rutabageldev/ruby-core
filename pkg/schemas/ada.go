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
)

// AdaFeedingEndedData is the CloudEvent Data payload for a breast feeding session end.
// Segments hold per-side timing; totals are pre-computed by the dashboard.
type AdaFeedingEndedData struct {
	Segments       []AdaFeedingSegment `json:"segments"`
	TotalDurationS int                 `json:"total_duration_s"`
	LeftTotalS     int                 `json:"left_total_s"`
	RightTotalS    int                 `json:"right_total_s"`
	LoggedBy       string              `json:"logged_by,omitempty"`
}

// AdaFeedingSegment is a single side + duration record within a breast feeding session.
type AdaFeedingSegment struct {
	Side      string `json:"side"`
	DurationS int    `json:"dur"`
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
// AmountOz is in oz at point of entry.
type AdaFeedingSupplementData struct {
	Source   string  `json:"source"`
	AmountOz float64 `json:"amount_oz"`
	LoggedBy string  `json:"logged_by,omitempty"`
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
