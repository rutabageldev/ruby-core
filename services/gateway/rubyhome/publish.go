// Package rubyhome routes domain-neutral ruby_home_event write events from Home
// Assistant onto NATS (ROADMAP-0012, Slice B). It is the domain-neutral successor
// to the ada package's write path: new home-automation write contracts (calendar,
// childcare, …) register their event string here instead of getting a bespoke HA
// event type. The NATS subject is derived from the payload "event" string.
package rubyhome

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	goNats "github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

// eventRoutes maps the frontend event type string (the "event" field in the
// ruby_home_event payload) to the NATS subject for that event. Subjects are the
// shared contract constants in pkg/schemas (ADR-0014).
var eventRoutes = map[string]string{
	"calendar.event.upsert":               schemas.HomeEventCalendarUpsert,
	"calendar.event.delete":               schemas.HomeEventCalendarDelete,
	"ruby_home.childcare.provider.upsert": schemas.HomeEventChildcareProviderUpsert,
	"ruby_home.childcare.provider.delete": schemas.HomeEventChildcareProviderDelete,
}

// Publish wraps payload in a CloudEvent and publishes it to the ha.events.* subject
// derived from the payload "event" string. An unknown event is logged and returns
// an error without publishing.
func Publish(nc *goNats.Conn, payload map[string]any, log *slog.Logger) error {
	eventType, _ := payload["event"].(string)
	subject, ok := eventRoutes[eventType]
	if !ok {
		log.Warn("ruby_home: unknown event type", slog.String("event", eventType))
		return fmt.Errorf("ruby_home: unknown event type %q", eventType)
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
		return fmt.Errorf("ruby_home: marshal CloudEvent: %w", err)
	}

	if err := nc.Publish(subject, b); err != nil {
		return fmt.Errorf("ruby_home: publish %s: %w", subject, err)
	}

	log.Info("ruby_home: event published",
		slog.String("event", eventType),
		slog.String("subject", subject),
		slog.String("id", id),
	)
	return nil
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}
