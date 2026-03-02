// Package nats contains the NATS publishing logic for the gateway.
package nats

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	goNats "github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

// Publisher wraps a NATS connection and publishes HA events and gateway
// health heartbeats as CloudEvents.
type Publisher struct {
	nc *goNats.Conn
}

// New returns a Publisher for the given NATS connection.
func New(nc *goNats.Conn) *Publisher {
	return &Publisher{nc: nc}
}

// PublishHAEvent publishes a HA state_changed event as a CloudEvent to
// ha.events.{domain}.{entityName}.
//
//   - entityID: the HA entity ID, e.g. "person.wife"
//   - state:    the new entity state string, e.g. "home"
//   - attrs:    the filtered attribute map (post lean projection)
//   - lastChanged: the HA last_changed timestamp (RFC3339 UTC)
func (p *Publisher) PublishHAEvent(entityID, state string, attrs map[string]any, lastChanged string) error {
	domain, entityName, err := splitEntityID(entityID)
	if err != nil {
		return err
	}

	id := newID()
	evt := schemas.CloudEvent{
		SpecVersion:   schemas.CloudEventsSpecVersion,
		ID:            id,
		Source:        "ha",
		Type:          "state_changed",
		Time:          lastChanged,
		DataSchema:    schemas.CloudEventDataSchemaVersionV1,
		Subject:       entityID,
		CorrelationID: id, // root event: correlationID == its own ID
		CausationID:   id,
	}

	// Merge state into attrs for a complete data payload.
	data := make(map[string]any, len(attrs)+1)
	for k, v := range attrs {
		data[k] = v
	}
	data["state"] = state
	data["last_changed"] = lastChanged
	evt.Data = data

	payload, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("publisher: marshal event: %w", err)
	}

	subject := fmt.Sprintf("ha.events.%s.%s", domain, entityName)
	return p.nc.Publish(subject, payload)
}

// PublishHealth publishes a gateway health heartbeat CloudEvent to the
// "gateway.health" NATS subject every tick from the caller's goroutine.
func (p *Publisher) PublishHealth(haConnected bool) error {
	id := newID()
	now := time.Now().UTC().Format(time.RFC3339)
	evt := schemas.CloudEvent{
		SpecVersion:   schemas.CloudEventsSpecVersion,
		ID:            id,
		Source:        "ruby_gateway",
		Type:          "gateway.health",
		Time:          now,
		CorrelationID: id,
		CausationID:   id,
		Data: map[string]any{
			"ha_connected": haConnected,
			"time":         now,
		},
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("publisher: marshal health: %w", err)
	}
	return p.nc.Publish("gateway.health", payload)
}

// splitEntityID splits an HA entity ID into (domain, entityName).
// e.g. "person.wife" → ("person", "wife")
// e.g. "device_tracker.wife_phone" → ("device_tracker", "wife_phone")
func splitEntityID(entityID string) (domain, entityName string, err error) {
	for i, c := range entityID {
		if c == '.' {
			return entityID[:i], entityID[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("publisher: invalid entity ID %q: missing domain separator", entityID)
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}
