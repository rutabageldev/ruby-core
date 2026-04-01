// Package ada provides the HTTP handler for Ada baby-tracking event ingestion.
// Accepts POST /ada/events, maps the event type to ha.events.ada.{type},
// and publishes a CloudEvent to the existing HA_EVENTS JetStream stream.
package ada

import (
	"encoding/json"
	"log/slog"
	"net/http"

	goNats "github.com/nats-io/nats.go"
)

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

	if err := Publish(h.nc, raw, h.log); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}
