// Package ada provides the HTTP handler for Ada baby-tracking event ingestion.
// Accepts POST /ada/events, maps the event type to ha.events.ada.{type},
// and publishes a CloudEvent to the existing HA_EVENTS JetStream stream.
package ada

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	goNats "github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

// eventRoutes maps the frontend event type string (from the "event" field in the
// request body) to the NATS subject for that event type.
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

// Handler publishes Ada dashboard actions as CloudEvents to HA_EVENTS.
type Handler struct {
	nc  *goNats.Conn
	log *slog.Logger
}

// New returns a Handler that publishes to the given NATS connection.
func New(nc *goNats.Conn, log *slog.Logger) *Handler {
	return &Handler{nc: nc, log: log}
}

// ServeHTTP handles POST /ada/events.
// Decodes the request body, routes by the "event" field, wraps in a CloudEvent,
// and publishes to the appropriate ha.events.ada.* subject.
// Returns 202 Accepted on success; the payload is fire-and-forget once published.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		h.log.Warn("ada: decode request body", slog.String("error", err.Error()))
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	eventType, _ := raw["event"].(string)
	subject, ok := eventRoutes[eventType]
	if !ok {
		h.log.Warn("ada: unknown event type", slog.String("event", eventType))
		http.Error(w, fmt.Sprintf("unknown event type %q", eventType), http.StatusBadRequest)
		return
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
		Data:          raw,
	}

	payload, err := json.Marshal(evt)
	if err != nil {
		h.log.Error("ada: marshal CloudEvent", slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := h.nc.Publish(subject, payload); err != nil {
		h.log.Error("ada: publish", slog.String("subject", subject), slog.String("error", err.Error()))
		http.Error(w, "publish failed", http.StatusInternalServerError)
		return
	}

	h.log.Info("ada: event published",
		slog.String("event", eventType),
		slog.String("subject", subject),
		slog.String("id", id),
	)
	w.WriteHeader(http.StatusAccepted)
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}
