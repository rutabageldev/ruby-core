package ada

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	goNats "github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

// eventRoutes maps the frontend event type string (from the "event" field in the
// payload) to the NATS subject for that event type.
var eventRoutes = map[string]string{
	"ada.feeding.end":         schemas.AdaEventFeedingEnded,
	"ada.feeding.log":         schemas.AdaEventFeedingLogged,
	"ada.feeding.supplement":  schemas.AdaEventFeedingSupplemented,
	"ada.diaper.log":          schemas.AdaEventDiaperLogged,
	"ada.sleep.start":         schemas.AdaEventSleepStarted,
	"ada.sleep.end":           schemas.AdaEventSleepEnded,
	"ada.sleep.log":           schemas.AdaEventSleepLogged,
	"ada.tummy.end":           schemas.AdaEventTummyEnded,
	"ada.tummy.log":           schemas.AdaEventTummyLogged,
	"ada.feeding.log_past":    schemas.AdaEventFeedingLoggedPast,
	"ada.born":                schemas.AdaEventBorn,
	"ada.caretaker.update":    schemas.AdaEventCaretakerUpdate,
	"ada.config.tummy_target": schemas.AdaEventTummyTarget,
	"ada.channel.add":         schemas.AdaEventAddChannel,
	"ada.channel.remove":      schemas.AdaEventRemoveChannel,
	"ada.config.bedtime":      schemas.AdaEventBedtimeConfig,
	"ada.growth.log":          schemas.AdaEventGrowthLogged,
	"ada.feeding.claimed":     schemas.AdaEventFeedingClaimed,
	"ada.trends.query":        schemas.AdaEventTrendsQuery,
	"ada.feeding.update":      schemas.AdaEventFeedingUpdate,
	"ada.feeding.delete":      schemas.AdaEventFeedingDelete,
	"ada.diaper.update":       schemas.AdaEventDiaperUpdate,
	"ada.diaper.delete":       schemas.AdaEventDiaperDelete,
	"ada.sleep.update":        schemas.AdaEventSleepUpdate,
	"ada.sleep.delete":        schemas.AdaEventSleepDelete,
	"ada.tummy.update":        schemas.AdaEventTummyUpdate,
	"ada.tummy.delete":        schemas.AdaEventTummyDelete,
	"ada.growth.update":       schemas.AdaEventGrowthUpdate,
	"ada.growth.delete":       schemas.AdaEventGrowthDelete,

	// Medications & Emergency (ROADMAP-0011) — registry + routines (effort 0011.1).
	"ada.medication.upsert":         schemas.AdaEventMedicationUpsert,
	"ada.medication.delete":         schemas.AdaEventMedicationDelete,
	"ada.medication.routine.upsert": schemas.AdaEventMedicationRoutineUpsert,
	"ada.medication.routine.delete": schemas.AdaEventMedicationRoutineDelete,

	// Dose events + series (effort 0011.2).
	"ada.medication.given":        schemas.AdaEventMedicationGiven,
	"ada.medication.skipped":      schemas.AdaEventMedicationSkipped,
	"ada.medication.series.start": schemas.AdaEventMedicationSeriesStart,
	"ada.medication.series.end":   schemas.AdaEventMedicationSeriesEnd,
	"ada.medication.event.update": schemas.AdaEventMedicationEventUpdate,
	"ada.medication.event.delete": schemas.AdaEventMedicationEventDelete,

	// Emergency card (effort 0011.4).
	"ada.emergency.row.upsert": schemas.AdaEventEmergencyRowUpsert,
	"ada.emergency.row.delete": schemas.AdaEventEmergencyRowDelete,
	"ada.emergency.reorder":    schemas.AdaEventEmergencyReorder,
}

// Publish wraps payload in a CloudEvent and publishes to the appropriate
// ha.events.ada.* NATS subject. Used by both the HTTP handler and the
// gateway WebSocket ada_event handler.
func Publish(nc *goNats.Conn, payload map[string]any, log *slog.Logger) error {
	eventType, _ := payload["event"].(string)
	subject, ok := eventRoutes[eventType]
	if !ok {
		log.Warn("ada: unknown event type", slog.String("event", eventType))
		return fmt.Errorf("ada: unknown event type %q", eventType)
	}

	id := newID()
	evt := schemas.CloudEvent{
		SpecVersion:   schemas.CloudEventsSpecVersion,
		ID:            id,
		Source:        "ruby_gateway",
		Type:          subject,
		Time:          time.Now().UTC().Format(time.RFC3339),
		DataSchema:    schemas.CloudEventDataSchemaVersionV1,
		CorrelationID: id,
		CausationID:   id,
		Data:          payload,
	}

	b, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("ada: marshal CloudEvent: %w", err)
	}

	if err := nc.Publish(subject, b); err != nil {
		return fmt.Errorf("ada: publish %s: %w", subject, err)
	}

	log.Info("ada: event published",
		slog.String("event", eventType),
		slog.String("subject", subject),
		slog.String("id", id),
	)
	return nil
}

// PublishUsersSynced wraps the synced user list in a CloudEvent and publishes
// it to ha.events.ada.users_synced on NATS. Called directly by the gateway
// after querying HA — not routed through eventRoutes.
// availableServices is the full list of mobile_app_* notify service names
// discovered from HA, forwarded so the engine can populate the device picker.
func PublishUsersSynced(nc *goNats.Conn, users []schemas.AdaHAUser, availableServices []string, log *slog.Logger) error {
	subject := schemas.AdaEventUsersSynced
	id := newID()
	evt := schemas.CloudEvent{
		SpecVersion:   schemas.CloudEventsSpecVersion,
		ID:            id,
		Source:        "ruby_gateway",
		Type:          subject,
		Time:          time.Now().UTC().Format(time.RFC3339),
		DataSchema:    schemas.CloudEventDataSchemaVersionV1,
		CorrelationID: id,
		CausationID:   id,
		Data: map[string]any{
			"users":              users,
			"available_services": availableServices,
		},
	}
	b, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("ada: marshal users_synced: %w", err)
	}
	if err := nc.Publish(subject, b); err != nil {
		return fmt.Errorf("ada: publish users_synced: %w", err)
	}
	log.Info("ada: users_synced published", slog.Int("count", len(users)))
	return nil
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}
