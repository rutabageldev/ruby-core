package main

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/boot"
	"github.com/primaryrutabaga/ruby-core/services/engine/config"
	"github.com/primaryrutabaga/ruby-core/services/engine/processor"
)

// ProcessorHost manages the lifecycle of all registered Logical Processors
// (ADR-0007) and routes incoming events to the processors that subscribed
// to the matching NATS subject.
type ProcessorHost struct {
	processors []processor.Processor
	log        *slog.Logger
}

// NewProcessorHost creates a host. Processors must be registered via Register
// before Initialize is called.
func NewProcessorHost(log *slog.Logger) *ProcessorHost {
	return &ProcessorHost{log: log}
}

// Register adds a processor to the host. Must be called before Initialize.
func (h *ProcessorHost) Register(p processor.Processor) {
	h.processors = append(h.processors, p)
}

// RequiresStorage reports whether any registered processor implements
// StatefulProcessor and returns true from RequiresStorage. Used by main.go
// to determine whether to boot the Postgres connection pool before Initialize.
func (h *ProcessorHost) RequiresStorage() bool {
	for _, p := range h.processors {
		if sp, ok := p.(processor.StatefulProcessor); ok && sp.RequiresStorage() {
			return true
		}
	}
	return false
}

// Initialize calls Initialize on every registered processor with the provided
// config and resources. pool and ha are passed through to Config and are non-nil
// only when at least one StatefulProcessor is registered (see RequiresStorage).
//
// Coupling note: HA config (ha) is currently fetched unconditionally whenever
// any stateful processor is registered, even if a given processor only needs
// Postgres and not HA. This is acceptable with a single stateful processor (ada).
// If a future stateful processor requires Postgres but not HA, this method should
// be extended to accept a richer options struct (or HA config should be fetched
// per-processor in Initialize rather than centrally here). Don't refactor until
// there is a second stateful processor to drive the design.
func (h *ProcessorHost) Initialize(ruleCfg *config.CompiledConfig, nc *nats.Conn, js nats.JetStreamContext, pool *pgxpool.Pool, ha *boot.HAConfig) error {
	cfg := processor.Config{RuleCfg: ruleCfg, NC: nc, JS: js, Pool: pool, HA: ha}
	for _, p := range h.processors {
		if sp, ok := p.(processor.StatefulProcessor); ok && sp.RequiresStorage() {
			if cfg.Pool == nil {
				return fmt.Errorf("host: processor %T requires storage but Config.Pool is nil", p)
			}
		}
		if err := p.Initialize(cfg); err != nil {
			return fmt.Errorf("host: processor init: %w", err)
		}
	}
	return nil
}

// Process routes an event to every processor whose Subscriptions list matches
// the given subject. A processor subscription matches if the subject equals the
// pattern exactly, or if the pattern ends with ".>" and the subject has that prefix.
// Errors from individual processors are logged but do not prevent other processors
// from running; the error from the first failing processor is returned to the caller
// so the consumer can NAK the message.
func (h *ProcessorHost) Process(subject string, data []byte) error {
	var firstErr error
	matched := false

	for _, p := range h.processors {
		for _, pattern := range p.Subscriptions() {
			if matchesSubject(pattern, subject) {
				matched = true
				if err := p.ProcessEvent(subject, data); err != nil {
					h.log.Warn("host: processor error",
						slog.String("subject", subject),
						slog.String("error", err.Error()),
					)
					if firstErr == nil {
						firstErr = err
					}
				}
				break // a processor is called at most once per event
			}
		}
	}

	if !matched {
		h.log.Debug("host: no processor matched subject", slog.String("subject", subject))
	}
	return firstErr
}

// Shutdown calls Shutdown on every registered processor in reverse registration order.
func (h *ProcessorHost) Shutdown() {
	for i := len(h.processors) - 1; i >= 0; i-- {
		h.processors[i].Shutdown()
	}
}

// matchesSubject reports whether pattern matches subject following NATS wildcard rules.
// Only the trailing ".>" wildcard is supported here; ".*" single-token wildcards are not
// needed by any current processor and are left for a future ADR if required.
func matchesSubject(pattern, subject string) bool {
	if pattern == subject {
		return true
	}
	if strings.HasSuffix(pattern, ".>") {
		prefix := strings.TrimSuffix(pattern, ">")
		return strings.HasPrefix(subject, prefix)
	}
	return false
}
