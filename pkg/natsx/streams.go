package natsx

import (
	"errors"
	"fmt"
	"time"

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

// EnsureAuditStream creates the AUDIT_EVENTS JetStream stream if it does not already exist.
// The stream captures all audit.> subjects published by any service (ADR-0019).
// Messages are retained for DefaultAuditMaxAge (72h) to survive a prolonged audit-sink outage.
//
// Stream name: AUDIT_EVENTS (consistent with the HA_EVENTS and DLQ naming convention).
// ADR-0019 refers to this conceptually as "audit.events"; this implementation uses
// AUDIT_EVENTS to align with the SCREAMING_SNAKE_CASE convention established for other streams.
//
// Subject format: audit.{source}.{type} (e.g. audit.ruby_engine.event_processed).
// This inverts the standard ADR-0027 {source}.{class}.{type} order for the audit class.
// The inversion is necessary: a leading-wildcard filter *.audit.> overlaps with the
// reserved dlq.> namespace, causing NATS to reject the stream. Using audit.> avoids this.
func EnsureAuditStream(js nats.JetStreamContext) error {
	return ensureStream(js, &nats.StreamConfig{
		Name:      "AUDIT_EVENTS",
		Subjects:  []string{"audit.>"},
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
		MaxAge:    config.DefaultAuditMaxAge,
	})
}

// EnsureCommandsStream creates the COMMANDS JetStream stream if it does not already exist.
// The stream captures all ruby_engine.commands.> subjects published by the engine.
// Messages are retained for 1 hour — stale notifications are not worth replaying.
func EnsureCommandsStream(js nats.JetStreamContext) error {
	return ensureStream(js, &nats.StreamConfig{
		Name:      "COMMANDS",
		Subjects:  []string{"ruby_engine.commands.>"},
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
		MaxAge:    config.DefaultCommandsMaxAge,
	})
}

// EnsurePresenceStream creates the PRESENCE JetStream stream if it does not already exist.
// The stream captures all ruby_presence.events.> subjects published by the presence service.
// Messages are retained for 24 hours.
func EnsurePresenceStream(js nats.JetStreamContext) error {
	return ensureStream(js, &nats.StreamConfig{
		Name:      "PRESENCE",
		Subjects:  []string{"ruby_presence.events.>"},
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
		MaxAge:    24 * time.Hour,
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
