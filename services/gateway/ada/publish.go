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
	"ada.feeding.end":        schemas.AdaEventFeedingEnded,
	"ada.feeding.log":        schemas.AdaEventFeedingLogged,
	"ada.feeding.supplement": schemas.AdaEventFeedingSupplemented,
	"ada.diaper.log":         schemas.AdaEventDiaperLogged,
	"ada.sleep.start":        schemas.AdaEventSleepStarted,
	"ada.sleep.end":          schemas.AdaEventSleepEnded,
	"ada.sleep.log":          schemas.AdaEventSleepLogged,
	"ada.tummy.end":          schemas.AdaEventTummyEnded,
	"ada.tummy.log":          schemas.AdaEventTummyLogged,
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

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}
