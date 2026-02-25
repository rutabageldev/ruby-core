package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/idempotency"
	"github.com/primaryrutabaga/ruby-core/pkg/natsx"
)

// handleResult describes the outcome of processing a single message.
type handleResult int

const (
	resultAck  handleResult = iota // message processed successfully
	resultNak                      // processing failed; redeliver with backoff
	resultSkip                     // duplicate message; ack without processing
)

// Recorder is the interface implemented by audit.Publisher.
// Defining it here keeps the consumer decoupled from the audit package (ADR-0019).
// Inject a NoopRecorder in tests; inject *audit.Publisher in production.
type Recorder interface {
	Record(correlationID, causationID, action, natsSubject, outcome string)
}

// NoopRecorder is a Recorder that does nothing. Use in tests or when audit is disabled.
type NoopRecorder struct{}

func (NoopRecorder) Record(_, _, _, _, _ string) {}

// Consumer runs a pull-based JetStream worker pool for the engine (ADR-0024).
type Consumer struct {
	sub       *nats.Subscription
	idStore   idempotency.Store
	process   func(data []byte) error
	audit     Recorder
	log       *slog.Logger
	workerN   int
	batchSize int
	backOff   []time.Duration // NAK delay schedule; mirrors consumer BackOff config
}

// logger returns c.log if set, otherwise slog.Default().
// This allows Consumer structs constructed directly in tests (without a logger) to
// still produce output without panicking on a nil *slog.Logger.
func (c *Consumer) logger() *slog.Logger {
	if c.log != nil {
		return c.log
	}
	return slog.Default()
}

// NewConsumer constructs a Consumer. Returns an error if batchSize > workerN.
// Pass a NoopRecorder for audit if audit publishing is not required.
func NewConsumer(
	sub *nats.Subscription,
	idStore idempotency.Store,
	process func(data []byte) error,
	workerN, batchSize int,
	backOff []time.Duration,
	log *slog.Logger,
	audit Recorder,
) (*Consumer, error) {
	if batchSize > workerN {
		return nil, fmt.Errorf("engine: batchSize (%d) must not exceed workerN (%d)", batchSize, workerN)
	}
	if audit == nil {
		audit = NoopRecorder{}
	}
	return &Consumer{
		sub:       sub,
		idStore:   idStore,
		process:   process,
		audit:     audit,
		log:       log,
		workerN:   workerN,
		batchSize: batchSize,
		backOff:   backOff,
	}, nil
}

// nakDelay returns the delay to use with NakWithDelay for the given delivery number.
// When numDelivered exceeds the backOff schedule (i.e. final attempt), returns 0
// so the server fires the max-delivery advisory without an extra wait.
func nakDelay(backOff []time.Duration, numDelivered uint64) time.Duration {
	if len(backOff) == 0 || numDelivered == 0 || numDelivered > uint64(len(backOff)) {
		return 0
	}
	return backOff[numDelivered-1]
}

// Run starts the fetch loop, dispatching messages to a fixed worker pool.
// It returns when ctx is cancelled or a non-recoverable fetch error occurs.
func (c *Consumer) Run(ctx context.Context) error {
	sem := make(chan struct{}, c.workerN)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		msgs, err := c.sub.Fetch(c.batchSize, nats.MaxWait(2*time.Second))
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				continue
			}
			if errors.Is(err, context.Canceled) ||
				errors.Is(err, nats.ErrConnectionClosed) ||
				errors.Is(err, nats.ErrSubscriptionClosed) {
				return nil
			}
			return fmt.Errorf("engine: fetch: %w", err)
		}

		for _, msg := range msgs {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return nil
			}
			go func(m *nats.Msg) {
				defer func() { <-sem }()
				c.handle(m)
			}(msg)
		}
	}
}

// handle processes a single message: checks idempotency, calls process, then acks/naks.
// Structured log entries include correlationid and causationid from the CloudEvent payload.
func (c *Consumer) handle(msg *nats.Msg) {
	meta, err := msg.Metadata()
	if err != nil {
		c.logger().Error("engine: metadata error", slog.String("error", err.Error()))
		_ = msg.Nak()
		return
	}

	eventID := extractEventID(msg.Header, msg.Data, meta)
	correlationID, causationID := extractCorrelationFields(msg.Data)

	result, err := c.decide(eventID, msg.Data)
	if err != nil {
		c.logger().Error("engine: decide error",
			slog.String("eventid", eventID),
			slog.String("correlationid", correlationID),
			slog.String("error", err.Error()),
		)
		_ = msg.Nak()
		return
	}

	switch result {
	case resultAck:
		if err := c.idStore.Mark(eventID); err != nil {
			c.logger().Warn("engine: idempotency mark error",
				slog.String("eventid", eventID),
				slog.String("correlationid", correlationID),
				slog.String("error", err.Error()),
			)
		}
		c.audit.Record(correlationID, causationID, "event.processed", msg.Subject, "success")
		c.logger().Info("engine: event processed",
			slog.String("eventid", eventID),
			slog.String("correlationid", correlationID),
			slog.String("subject", msg.Subject),
		)
		_ = msg.Ack()

	case resultNak:
		c.audit.Record(correlationID, causationID, "event.failed", msg.Subject, "failure")
		if d := nakDelay(c.backOff, meta.NumDelivered); d > 0 {
			_ = msg.NakWithDelay(d)
		} else {
			_ = msg.Nak() // final attempt: no delay, let advisory fire promptly
		}

	case resultSkip:
		c.audit.Record(correlationID, causationID, "event.discarded", msg.Subject, "duplicate")
		_ = msg.Ack() // ack duplicates to remove from pending
	}
}

// decide evaluates idempotency and calls the process function.
// It is a pure decision function, separated from NATS types to enable unit testing.
func (c *Consumer) decide(eventID string, data []byte) (handleResult, error) {
	seen, err := c.idStore.Seen(eventID)
	if err != nil {
		return resultNak, fmt.Errorf("idempotency check: %w", err)
	}
	if seen {
		c.logger().Info("engine: duplicate event, discarding",
			slog.String("eventid", eventID),
		)
		return resultSkip, nil
	}
	if err := c.process(data); err != nil {
		c.logger().Warn("engine: process error, will nak",
			slog.String("eventid", eventID),
			slog.String("error", err.Error()),
		)
		return resultNak, nil
	}
	return resultAck, nil
}

// extractEventID derives a stable event identifier for idempotency tracking (ADR-0025).
// Priority: Nats-Msg-Id header → CloudEvent id field → stream sequence fallback.
func extractEventID(headers nats.Header, data []byte, meta *nats.MsgMetadata) string {
	// 1. Nats-Msg-Id header (set by publishers using NATS dedup header)
	if id := headers.Get("Nats-Msg-Id"); id != "" {
		return id
	}
	// 2. CloudEvent id field (fast single-field unmarshal; small payloads in practice)
	var ce struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &ce); err == nil && ce.ID != "" {
		return ce.ID
	}
	// 3. Stream sequence — always unique within the stream (handles at-least-once redelivery)
	return fmt.Sprintf("%s.%d", meta.Stream, meta.Sequence.Stream)
}

// extractCorrelationFields extracts the correlationid and causationid extensions from
// a CloudEvent payload in a single JSON pass. Returns empty strings for non-CloudEvent
// or malformed payloads; callers must treat empty as "unavailable" rather than an error.
func extractCorrelationFields(data []byte) (correlationID, causationID string) {
	var ce struct {
		CorrelationID string `json:"correlationid"`
		CausationID   string `json:"causationid"`
	}
	_ = json.Unmarshal(data, &ce)
	return ce.CorrelationID, ce.CausationID
}

// ---------------------------------------------------------------------------
// DLQForwarder — routes dead-lettered messages to the DLQ stream (ADR-0022)
// ---------------------------------------------------------------------------

// maxDeliverAdvisory is the payload emitted by the NATS server when a consumer
// exceeds MaxDeliver attempts for a message.
type maxDeliverAdvisory struct {
	Stream     string `json:"stream"`
	Consumer   string `json:"consumer"`
	StreamSeq  uint64 `json:"stream_seq"`
	Deliveries int    `json:"deliveries"`
}

// DLQForwarder subscribes to the NATS server max-delivery advisory for a specific
// consumer and republishes the original payload to the DLQ stream (ADR-0022).
//
// Why advisory-based: ConsumerConfig has no Republish field in nats.go v1.48.0.
// StreamConfig.RePublish copies ALL messages, not just dead-lettered ones.
// The advisory is the only server-side signal for max-delivery exhaustion.
type DLQForwarder struct {
	js       nats.JetStreamContext
	sub      *nats.Subscription
	msgCh    chan *nats.Msg
	log      *slog.Logger
	stream   string // e.g. "HA_EVENTS"
	consumer string // e.g. "engine_processor"
}

// logger returns f.log if set, otherwise slog.Default().
func (f *DLQForwarder) logger() *slog.Logger {
	if f.log != nil {
		return f.log
	}
	return slog.Default()
}

// NewDLQForwarder subscribes to the max-delivery advisory for the given stream/consumer
// and returns a DLQForwarder ready to be started with Run.
func NewDLQForwarder(nc *nats.Conn, js nats.JetStreamContext, stream, consumer string, log *slog.Logger) (*DLQForwarder, error) {
	advisorySubj := fmt.Sprintf("$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.%s.%s", stream, consumer)
	msgCh := make(chan *nats.Msg, 64)
	sub, err := nc.ChanSubscribe(advisorySubj, msgCh)
	if err != nil {
		return nil, fmt.Errorf("engine: dlq forwarder subscribe %q: %w", advisorySubj, err)
	}
	return &DLQForwarder{
		js:       js,
		sub:      sub,
		msgCh:    msgCh,
		log:      log,
		stream:   stream,
		consumer: consumer,
	}, nil
}

// Run processes advisory messages until ctx is cancelled.
func (f *DLQForwarder) Run(ctx context.Context) error {
	for {
		select {
		case msg, ok := <-f.msgCh:
			if !ok {
				return nil
			}
			f.handleAdvisory(msg)
		case <-ctx.Done():
			_ = f.sub.Unsubscribe()
			return nil
		}
	}
}

// handleAdvisory fetches the original message by sequence and publishes it to the DLQ stream.
// Failures are logged but non-fatal — the consumer has already moved on.
//
// Risk: if HA_EVENTS retention has evicted the message before the advisory fires, the
// DLQ routing silently drops. Mitigated by EnsureHAEventsStream setting no MaxAge.
func (f *DLQForwarder) handleAdvisory(msg *nats.Msg) {
	var adv maxDeliverAdvisory
	if err := json.Unmarshal(msg.Data, &adv); err != nil {
		f.logger().Error("engine: dlq: unmarshal advisory", slog.String("error", err.Error()))
		return
	}

	orig, err := f.js.GetMsg(f.stream, adv.StreamSeq)
	if err != nil {
		f.logger().Error("engine: dlq: get original message, dropped from DLQ",
			slog.String("stream", f.stream),
			slog.Uint64("seq", adv.StreamSeq),
			slog.String("error", err.Error()),
		)
		return
	}

	// BuildDLQSubject requires lowercase tokens (ADR-0027).
	dlqSubj, err := natsx.BuildDLQSubject(strings.ToLower(f.stream), f.consumer)
	if err != nil {
		f.logger().Error("engine: dlq: build subject",
			slog.String("stream", f.stream),
			slog.String("consumer", f.consumer),
			slog.String("error", err.Error()),
		)
		return
	}

	if _, err := f.js.Publish(dlqSubj, orig.Data); err != nil {
		f.logger().Error("engine: dlq: publish failed",
			slog.Uint64("seq", adv.StreamSeq),
			slog.String("subject", dlqSubj),
			slog.String("error", err.Error()),
		)
		return
	}
	f.logger().Info("engine: dlq: routed",
		slog.String("stream", f.stream),
		slog.Uint64("seq", adv.StreamSeq),
		slog.String("subject", dlqSubj),
		slog.Int("deliveries", adv.Deliveries),
	)
}
