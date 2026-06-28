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
)
