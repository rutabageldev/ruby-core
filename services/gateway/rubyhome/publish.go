// Package rubyhome routes domain-neutral ruby_home_event write events from Home
// Assistant onto NATS (ROADMAP-0012, Slice B). It is the domain-neutral successor
// to the ada package's write path: new home-automation write contracts (calendar,
// childcare, …) register their event string here instead of getting a bespoke HA
// event type. The NATS subject is derived from the payload "event" string.
package rubyhome

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	goNats "github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/natsx"
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
func Publish(ctx context.Context, nc *goNats.Conn, payload map[string]any, log *slog.Logger) error {
	eventType, _ := payload["event"].(string)
	subject, ok := eventRoutes[eventType]
	if !ok {
		log.Warn("ruby_home: unknown event type", slog.String("event", eventType))
		return fmt.Errorf("ruby_home: unknown event type %q", eventType)
	}

	ensureCalendarIdempotencyKey(eventType, payload)

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

	if err := natsx.PublishWithContext(ctx, nc, subject, b); err != nil {
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

// ensureCalendarIdempotencyKey guarantees a calendar create can be made idempotent at
// Google (ADR-0042). The engine derives the deterministic Google event id from the
// payload's idempotency_key (falling back to the random CloudEvent id), so without a
// STABLE key a gateway re-publish of the same logical create — an HA reconnect replay or
// a double-fire — would derive a different id and double-insert. When the HA producer
// already supplied a key we keep it (the ideal: a unique-per-action id); otherwise we
// derive one from the stable content fields so re-publishes converge. (Two genuinely
// identical creates collapse to one — almost always a desirable dedup of a double-submit.)
func ensureCalendarIdempotencyKey(eventType string, payload map[string]any) {
	if eventType != "calendar.event.upsert" {
		return
	}
	if k, ok := payload["idempotency_key"].(string); ok && k != "" {
		return
	}
	seed := []any{
		payload["summary"], payload["start"], payload["end"],
		payload["recurrence"], payload["logged_by"],
	}
	// json.Marshal sorts map keys, so the nested start/end objects hash deterministically.
	b, _ := json.Marshal(seed)
	sum := sha256.Sum256(b)
	payload["idempotency_key"] = hex.EncodeToString(sum[:])
}
