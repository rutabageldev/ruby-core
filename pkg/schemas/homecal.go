package schemas

// Home-calendar + household-overlay event subject constants — NATS subjects for the
// domain-neutral ruby_home_event write path (ROADMAP-0012). Producer: the gateway
// (Slice B). Consumers: the engine calendar processor (Slices C/D). Subjects follow
// ADR-0027 (lowercase tokens, underscores only; no dots within a token) and land in
// the HA_EVENTS stream (ha.events.>).
const (
	// Calendar write events (Slice C consumer).
	HomeEventCalendarUpsert = "ha.events.calendar.event_upsert"
	HomeEventCalendarDelete = "ha.events.calendar.event_delete"

	// Household-overlay childcare provider events (Slice D consumer).
	HomeEventChildcareProviderUpsert = "ha.events.ruby_home.childcare.provider_upsert"
	HomeEventChildcareProviderDelete = "ha.events.ruby_home.childcare.provider_delete"

	// Household-overlay directory person events (#155 §3): add/rename/deactivate people.
	HomeEventDirectoryPersonUpsert = "ha.events.ruby_home.directory.person_upsert"
	HomeEventDirectoryPersonDelete = "ha.events.ruby_home.directory.person_delete"

	// CalendarReminderDue is published by the engine when an event reminder fires.
	HomeEventCalendarReminderDue = "ha.events.calendar.reminder_due"
)

// CalendarEventDate mirrors Google's EventDateTime: a date (all-day) XOR a
// datetime+timezone (timed). Exactly one of Date / DateTime is set per the all_day
// flag on the enclosing payload.
type CalendarEventDate struct {
	Date     string `json:"date,omitempty"`     // YYYY-MM-DD for all-day
	DateTime string `json:"datetime,omitempty"` // RFC3339 for timed
	TimeZone string `json:"timezone,omitempty"` // IANA zone, with DateTime
}

// Calendar edit/delete scope (ADR-0044). Absent/empty means ScopeAll for back-compat.
const (
	ScopeAll              = "all"                // the whole series (or a single event)
	ScopeThis             = "this"               // only the addressed occurrence
	ScopeThisAndFollowing = "this_and_following" // deferred (ADR-0044 obligation 5)
)

// CalendarUpsertData is the payload of a calendar.event.upsert write event
// (ROADMAP-0012). Absent GoogleEventID means create; present means update. For a
// per-occurrence edit (Scope=this) RecurringEventID + OriginalStart address the
// occurrence (ADR-0044, #155 §2).
type CalendarUpsertData struct {
	GoogleEventID    string            `json:"google_event_id,omitempty"`
	Summary          string            `json:"summary"`
	Start            CalendarEventDate `json:"start"`
	End              CalendarEventDate `json:"end"`
	AllDay           bool              `json:"all_day"`
	Recurrence       []string          `json:"recurrence,omitempty"`
	Location         string            `json:"location,omitempty"`
	Description      string            `json:"description,omitempty"`
	Subjects         []string          `json:"subjects,omitempty"`  // person_ids — overlay (Slice D)
	Childcare        *string           `json:"childcare,omitempty"` // provider_id | null — overlay (Slice D)
	Etag             string            `json:"etag,omitempty"`      // update only → If-Match
	IdempotencyKey   string            `json:"idempotency_key,omitempty"`
	LoggedBy         string            `json:"logged_by,omitempty"`
	Scope            string            `json:"scope,omitempty"`              // all (default) | this | this_and_following
	RecurringEventID string            `json:"recurring_event_id,omitempty"` // series master, for Scope=this
	OriginalStart    string            `json:"original_start,omitempty"`     // RFC3339 occurrence, for Scope=this
}

// CalendarDeleteData is the payload of a calendar.event.delete write event. Scope=this
// cancels a single occurrence (RecurringEventID + OriginalStart); Scope=all (default)
// deletes the whole series (ADR-0044).
type CalendarDeleteData struct {
	GoogleEventID    string `json:"google_event_id"`
	LoggedBy         string `json:"logged_by,omitempty"`
	Scope            string `json:"scope,omitempty"`
	RecurringEventID string `json:"recurring_event_id,omitempty"`
	OriginalStart    string `json:"original_start,omitempty"`
}

// ChildcareProviderUpsertData is the payload of ruby_home.childcare.provider.upsert
// (create when ID absent, else update). Local overlay only (Slice D).
type ChildcareProviderUpsertData struct {
	ID           string  `json:"id,omitempty"`
	DisplayName  string  `json:"display_name"`
	PersonID     *string `json:"person_id,omitempty"`
	Relationship *string `json:"relationship,omitempty"`
	Archived     bool    `json:"archived,omitempty"`
}

// ChildcareProviderDeleteData is the payload of ruby_home.childcare.provider.delete
// (delete = archive, preserving frequency history).
type ChildcareProviderDeleteData struct {
	ID string `json:"id"`
}

// DirectoryPersonUpsertData is the payload of ruby_home.directory.person.upsert
// (create when ID absent, else update by id — #155 §3). Local overlay only. Unlike
// calendar events, a person upsert is a full record: the consumer (HA) sends the whole
// person object, so omitted fields are written as their zero/empty value.
type DirectoryPersonUpsertData struct {
	ID               string `json:"id,omitempty"`
	DisplayName      string `json:"display_name"`
	Kind             string `json:"kind,omitempty"`                // "person" (default) | "group"
	Email            string `json:"email,omitempty"`               // primary address; reconciles attendees
	Family           string `json:"family,omitempty"`              // optional family grouping
	Color            string `json:"color,omitempty"`               // overlay colour
	HAPersonEntityID string `json:"ha_person_entity_id,omitempty"` // links to an HA person entity
}

// DirectoryPersonDeleteData is the payload of ruby_home.directory.person.delete
// (delete = deactivate; the row is retained so historical associations resolve).
type DirectoryPersonDeleteData struct {
	ID string `json:"id"`
}
