package natsx

import (
	"errors"
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/config"
)

// EnsureHAEventsStream creates the HA_EVENTS JetStream stream if it does not already exist.
// The stream captures all ha.events.> subjects published by the gateway and adapters.
// No MaxAge or MaxMsgs limit is set — messages are retained until storage is exhausted.
// This ensures original payloads remain available for DLQ routing during the full BackOff window.
func EnsureHAEventsStream(js nats.JetStreamContext) error {
	return ensureStream(js, &nats.StreamConfig{
		Name:      "HA_EVENTS",
		Subjects:  []string{"ha.events.>"},
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
	})
}

// EnsureDLQStream creates the DLQ JetStream stream if it does not already exist.
// The wildcard subjects dlq.> capture dead-lettered messages from all consumers (ADR-0022).
// Messages are retained for DefaultDLQMaxAge (7 days) for manual inspection and reprocessing.
func EnsureDLQStream(js nats.JetStreamContext) error {
	return ensureStream(js, &nats.StreamConfig{
		Name:      "DLQ",
		Subjects:  []string{"dlq.>"},
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
		MaxAge:    config.DefaultDLQMaxAge,
	})
}

// ensureStream creates a stream only if it does not already exist. Idempotent.
func ensureStream(js nats.JetStreamContext, cfg *nats.StreamConfig) error {
	_, err := js.StreamInfo(cfg.Name)
	if err == nil {
		return nil
	}
	if !errors.Is(err, nats.ErrStreamNotFound) {
		return fmt.Errorf("stream info %q: %w", cfg.Name, err)
	}
	if _, err = js.AddStream(cfg); err != nil {
		return fmt.Errorf("add stream %q: %w", cfg.Name, err)
	}
	return nil
}
