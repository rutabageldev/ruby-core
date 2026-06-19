package natsx

import (
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/config"
)

// EnsureHAEventsStream creates or reconciles the HA_EVENTS JetStream stream.
// The stream captures all ha.events.> subjects published by the gateway and adapters.
// Bounded by both age and bytes (ADR-0034): the age limit keeps original payloads
// available well past the consumer retry/DLQ-routing window, and the byte cap is a hard
// ceiling so this high-volume firehose can never exhaust the JetStream store — its prior
// unbounded retention filled the account and starved the discard=new KV buckets.
func EnsureHAEventsStream(js nats.JetStreamContext) error {
	return ensureStream(js, &nats.StreamConfig{
		Name:      "HA_EVENTS",
		Subjects:  []string{"ha.events.>"},
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
		MaxAge:    config.DefaultHAEventsMaxAge,
		MaxBytes:  config.MaxBytesHAEvents,
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
		MaxBytes:  config.MaxBytesDLQ,
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
		MaxBytes:  config.MaxBytesAudit,
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
		MaxBytes:  config.MaxBytesCommands,
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
		MaxBytes:  config.MaxBytesPresence,
	})
}

// ensureStream creates a stream if absent, or reconciles its mutable retention limits
// if it already exists with drifted config. Idempotent. Reconciliation is what lets a
// limit change in code (e.g. a new MaxAge/MaxBytes) actually take effect on an existing
// deployment instead of being silently ignored — the gap that let HA_EVENTS grow
// unbounded after its limits were assumed but never applied (ADR-0034).
func ensureStream(js nats.JetStreamContext, cfg *nats.StreamConfig) error {
	info, err := js.StreamInfo(cfg.Name)
	if errors.Is(err, nats.ErrStreamNotFound) {
		if _, err = js.AddStream(cfg); err != nil {
			return fmt.Errorf("add stream %q: %w", cfg.Name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("stream info %q: %w", cfg.Name, err)
	}
	// Only retention limits are reconciled — these are safe to UpdateStream. Immutable
	// fields (name, storage, retention policy, subjects) are never changed here.
	if streamLimitsDrifted(&info.Config, cfg) {
		if _, err = js.UpdateStream(cfg); err != nil {
			return fmt.Errorf("update stream %q: %w", cfg.Name, err)
		}
	}
	return nil
}

// streamLimitsDrifted reports whether the live stream's mutable retention limits differ
// from the desired config. Pure, so the reconcile trigger is unit-testable.
func streamLimitsDrifted(existing, desired *nats.StreamConfig) bool {
	return existing.MaxAge != desired.MaxAge ||
		existing.MaxBytes != desired.MaxBytes ||
		existing.MaxMsgs != desired.MaxMsgs
}
