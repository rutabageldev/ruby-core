package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

// Consumer runs a pull-based JetStream worker pool for the engine (ADR-0024).
type Consumer struct {
	sub       *nats.Subscription
	idStore   idempotency.Store
	process   func(data []byte) error
	workerN   int
	batchSize int
	backOff   []time.Duration // NAK delay schedule; mirrors consumer BackOff config
}

// NewConsumer constructs a Consumer. Returns an error if batchSize > workerN.
func NewConsumer(
	sub *nats.Subscription,
	idStore idempotency.Store,
	process func(data []byte) error,
	workerN, batchSize int,
	backOff []time.Duration,
) (*Consumer, error) {
	if batchSize > workerN {
		return nil, fmt.Errorf("engine: batchSize (%d) must not exceed workerN (%d)", batchSize, workerN)
	}
	return &Consumer{
		sub:       sub,
		idStore:   idStore,
		process:   process,
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
func (c *Consumer) handle(msg *nats.Msg) {
	meta, err := msg.Metadata()
	if err != nil {
		log.Printf("engine: metadata error: %v", err)
		_ = msg.Nak()
		return
	}

	eventID := extractEventID(msg.Header, msg.Data, meta)

	result, err := c.decide(eventID, msg.Data)
	if err != nil {
		log.Printf("engine: decide error for %s: %v", eventID, err)
		_ = msg.Nak()
		return
	}

	switch result {
	case resultAck:
		if err := c.idStore.Mark(eventID); err != nil {
			log.Printf("engine: idempotency mark error for %s: %v", eventID, err)
		}
		_ = msg.Ack()
	case resultNak:
		// Use NakWithDelay so the NATS server honours the configured backoff schedule.
		// Plain Nak() triggers immediate redelivery regardless of BackOff in ConsumerConfig.
		if d := nakDelay(c.backOff, meta.NumDelivered); d > 0 {
			_ = msg.NakWithDelay(d)
		} else {
			_ = msg.Nak() // final attempt: no delay, let advisory fire promptly
		}
	case resultSkip:
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
		log.Printf("engine: duplicate event %q, discarding", eventID)
		return resultSkip, nil
	}
	if err := c.process(data); err != nil {
		log.Printf("engine: process error for %q: %v (will nak)", eventID, err)
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
	stream   string // e.g. "HA_EVENTS"
	consumer string // e.g. "engine_processor"
}

// NewDLQForwarder subscribes to the max-delivery advisory for the given stream/consumer
// and returns a DLQForwarder ready to be started with Run.
func NewDLQForwarder(nc *nats.Conn, js nats.JetStreamContext, stream, consumer string) (*DLQForwarder, error) {
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
		log.Printf("engine: dlq: unmarshal advisory: %v", err)
		return
	}

	orig, err := f.js.GetMsg(f.stream, adv.StreamSeq)
	if err != nil {
		log.Printf("engine: dlq: get original msg stream=%s seq=%d: %v (message dropped from DLQ)",
			f.stream, adv.StreamSeq, err)
		return
	}

	// BuildDLQSubject requires lowercase tokens (ADR-0027).
	dlqSubj, err := natsx.BuildDLQSubject(strings.ToLower(f.stream), f.consumer)
	if err != nil {
		log.Printf("engine: dlq: build subject stream=%s consumer=%s: %v", f.stream, f.consumer, err)
		return
	}

	if _, err := f.js.Publish(dlqSubj, orig.Data); err != nil {
		log.Printf("engine: dlq: publish seq=%d to %s: %v", adv.StreamSeq, dlqSubj, err)
		return
	}
	log.Printf("engine: dlq: routed stream=%s seq=%d to %s (deliveries=%d)",
		f.stream, adv.StreamSeq, dlqSubj, adv.Deliveries)
}
