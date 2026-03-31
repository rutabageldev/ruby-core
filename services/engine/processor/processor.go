// Package processor defines the Logical Processor interface and its supporting
// types. All automation domain logic in the engine is implemented as a
// Processor (ADR-0007).
//
// Processors are self-contained; cross-processor communication MUST go via the
// NATS bus (ADR-0007). Each processor is the sole writer to its NATS KV keyspace
// (ADR-0002). See pkg/natsx/kv.go for the canonical KV bucket reference.
package processor

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/boot"
	"github.com/primaryrutabaga/ruby-core/services/engine/config"
)

// Config bundles the resources passed to every processor at Initialize time.
type Config struct {
	// RuleCfg is the compiled automation rule configuration derived from YAML files.
	RuleCfg *config.CompiledConfig
	// NC is the NATS connection used for publishing commands and subscribing to subjects.
	NC *nats.Conn
	// JS is the JetStream context used for KV bucket access.
	JS nats.JetStreamContext
	// Pool is the shared PostgreSQL connection pool.
	// It is non-nil only when at least one StatefulProcessor is registered.
	// Stateless processors must not access Pool; it may be nil.
	Pool *pgxpool.Pool
	// HA holds Home Assistant connection parameters for processors that push
	// sensor state to HA. It is non-nil only when stateful processors are
	// registered. Fetched in main.go via boot.FetchHAConfig.
	HA *boot.HAConfig
}

// Processor is the interface all logical processors must satisfy.
//
//   - Initialize is called once at startup; it receives Config containing the
//     compiled rule config and NATS/JetStream access. Returning an error causes
//     the engine to exit.
//   - Subscriptions returns the NATS subject patterns this processor handles.
//     The host routes each incoming event to every processor whose subscription
//     list contains a matching pattern.
//   - ProcessEvent is called for each routed event. It must return an error only
//     for transient failures; the caller treats errors as NAK signals.
//   - Shutdown is called when the engine is shutting down. Implementations should
//     release resources and return promptly.
type Processor interface {
	Initialize(cfg Config) error
	Subscriptions() []string
	ProcessEvent(subject string, data []byte) error
	Shutdown()
}

// StatefulProcessor is a Processor that requires a PostgreSQL connection pool.
// The ProcessorHost verifies that Config.Pool is non-nil before calling
// Initialize on any processor that returns true from RequiresStorage.
type StatefulProcessor interface {
	Processor
	RequiresStorage() bool
}
