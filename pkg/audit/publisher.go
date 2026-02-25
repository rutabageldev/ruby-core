// Package audit implements non-blocking audit event publishing per ADR-0019.
//
// Services use a Publisher to record critical actions without blocking primary logic.
// A bounded internal channel decouples Record() from the NATS write path:
//   - Record() enqueues immediately; a background goroutine drains and publishes.
//   - If the channel is full, the event is dropped and a Warn is logged rather than blocking.
//
// This satisfies the ADR-0019 requirement that publishing to the audit stream
// MUST NOT block the execution of the primary action.
//
// Trade-off: nc.Publish() itself is non-blocking in the common case (the NATS client
// buffers writes internally). The channel is the explicit, bounded guarantee — if the
// NATS write buffer is also full, nc.Publish() could theoretically block briefly.
// For the low audit-event volumes in Ruby Core this is not a concern in practice,
// but the channel-based design makes the non-blocking contract explicit and testable.
package audit

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/natsx"
	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

// publishChannelCap is the maximum number of audit events that can be queued
// before Record() begins dropping events with a Warn log.
const publishChannelCap = 256

// Publisher publishes audit events fire-and-forget per ADR-0019.
// Construct with NewPublisher; call Close() during graceful shutdown.
type Publisher struct {
	nc        *nats.Conn
	source    string // ADR-0027 source token, e.g. "ruby_engine"
	log       *slog.Logger
	ch        chan schemas.AuditEvent
	closeOnce sync.Once
}

// NewPublisher returns a Publisher for the given service source.
// source is normalised to a valid ADR-0027 token via natsx.SubjectToken
// (e.g. "ruby-core-engine" → "ruby_engine").
// The background drain goroutine is started immediately.
func NewPublisher(nc *nats.Conn, source string, log *slog.Logger) *Publisher {
	p := &Publisher{
		nc:     nc,
		source: natsx.SubjectToken(source),
		log:    log,
		ch:     make(chan schemas.AuditEvent, publishChannelCap),
	}
	go p.drain()
	return p
}

// Record enqueues an audit event for asynchronous publication.
// It returns immediately without blocking the caller (ADR-0019).
// If the internal buffer is full, the event is dropped and a Warn is logged.
func (p *Publisher) Record(correlationID, causationID, action, natsSubject, outcome string) {
	evt := schemas.NewAuditEvent(newID(), p.source, correlationID, causationID, schemas.AuditData{
		Actor:   p.source,
		Action:  action,
		Subject: natsSubject,
		Outcome: outcome,
	})

	// Recover guards against the unlikely race where Close() is called concurrently
	// with Record() during graceful shutdown.
	defer func() {
		if r := recover(); r != nil {
			p.log.Warn("audit: record called after close, event dropped",
				slog.String("action", action),
				slog.String("outcome", outcome),
			)
		}
	}()

	select {
	case p.ch <- evt:
	default:
		p.log.Warn("audit: publish channel full, event dropped",
			slog.String("action", action),
			slog.String("outcome", outcome),
		)
	}
}

// Close drains remaining events and stops the background goroutine.
// Must be called during graceful shutdown after all producers have stopped.
func (p *Publisher) Close() {
	p.closeOnce.Do(func() { close(p.ch) })
}

// drain is the background goroutine that serialises publishes to NATS.
// It exits cleanly when the channel is closed by Close().
func (p *Publisher) drain() {
	for evt := range p.ch {
		// Convert the dot-separated action to a valid ADR-0027 subject token.
		// e.g. "event.processed" → "event_processed"
		typeToken := strings.ReplaceAll(evt.Data.Action, ".", "_")

		subj, err := natsx.BuildAuditSubject(p.source, typeToken)
		if err != nil {
			p.log.Warn("audit: build subject failed",
				slog.String("source", p.source),
				slog.String("action", evt.Data.Action),
				slog.String("error", err.Error()),
			)
			continue
		}

		data, err := json.Marshal(evt)
		if err != nil {
			p.log.Warn("audit: marshal failed", slog.String("error", err.Error()))
			continue
		}

		if err := p.nc.Publish(subj, data); err != nil {
			p.log.Warn("audit: publish failed",
				slog.String("subject", subj),
				slog.String("error", err.Error()),
			)
		}
	}
}

// newID returns a short random hex string suitable for use as a CloudEvent ID.
func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}
