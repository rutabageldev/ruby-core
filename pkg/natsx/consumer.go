package natsx

import (
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/config"
)

// PullConsumerConfig holds configuration for a durable pull-based JetStream consumer (ADR-0024).
type PullConsumerConfig struct {
	// Stream is the JetStream stream name to bind to.
	Stream string
	// Durable is the consumer durable name (persisted on the server).
	Durable string
	// FilterSubject narrows which subjects this consumer receives.
	FilterSubject string
	// MaxDeliver is the maximum number of delivery attempts before a message is considered poison (ADR-0022).
	MaxDeliver int
	// MaxAckPending is the maximum number of unacknowledged messages the server will hold in flight (ADR-0024).
	MaxAckPending int
	// AckWait is how long the server waits for an ack before redelivering (ADR-0024).
	AckWait time.Duration
	// BackOff defines the server-side redelivery delay schedule (ADR-0022).
	BackOff []time.Duration
	// WorkerCount is the size of the fixed worker pool. Must be >= FetchBatch (ADR-0024).
	WorkerCount int
	// FetchBatch is the number of messages requested per Fetch call. Must be <= WorkerCount.
	FetchBatch int
}

// DefaultPullConsumerConfig returns a PullConsumerConfig pre-populated with Phase 3 defaults
// from pkg/config. The caller provides only the stream, durable name, and filter subject.
func DefaultPullConsumerConfig(stream, durable, filterSubj string) PullConsumerConfig {
	return PullConsumerConfig{
		Stream:        stream,
		Durable:       durable,
		FilterSubject: filterSubj,
		MaxDeliver:    config.DefaultMaxDeliver,
		MaxAckPending: config.DefaultMaxAckPending,
		AckWait:       config.DefaultAckWait,
		BackOff:       config.DefaultBackOff,
		WorkerCount:   config.DefaultWorkerCount,
		FetchBatch:    config.DefaultFetchBatch,
	}
}

// EnsurePullConsumer creates (or verifies) a durable pull consumer and returns a bound
// pull subscription. It uses the two-step NATS pattern: AddConsumer for server-side config,
// then PullSubscribe with Bind to attach (ADR-0024).
//
// The consumer is idempotent: if it already exists, the subscription binds to it directly.
// If the existing consumer has a different configuration, AddConsumer will return an error.
func EnsurePullConsumer(js nats.JetStreamContext, cfg PullConsumerConfig) (*nats.Subscription, error) {
	if cfg.FetchBatch > cfg.WorkerCount {
		return nil, fmt.Errorf("natsx: FetchBatch (%d) must not exceed WorkerCount (%d)",
			cfg.FetchBatch, cfg.WorkerCount)
	}

	consumerCfg := &nats.ConsumerConfig{
		Durable:       cfg.Durable,
		FilterSubject: cfg.FilterSubject,
		AckPolicy:     nats.AckExplicitPolicy,
		MaxDeliver:    cfg.MaxDeliver,
		BackOff:       cfg.BackOff,
		AckWait:       cfg.AckWait,
		MaxAckPending: cfg.MaxAckPending,
	}

	// Create the consumer only if it does not already exist.
	_, err := js.ConsumerInfo(cfg.Stream, cfg.Durable)
	if err != nil {
		if !errors.Is(err, nats.ErrConsumerNotFound) {
			return nil, fmt.Errorf("natsx: consumer info %q: %w", cfg.Durable, err)
		}
		// Consumer does not exist — create it.
		if _, err = js.AddConsumer(cfg.Stream, consumerCfg); err != nil {
			return nil, fmt.Errorf("natsx: add consumer %q: %w", cfg.Durable, err)
		}
	}

	// Bind a pull subscription to the pre-existing durable consumer.
	sub, err := js.PullSubscribe(cfg.FilterSubject, cfg.Durable,
		nats.Bind(cfg.Stream, cfg.Durable),
	)
	if err != nil {
		return nil, fmt.Errorf("natsx: pull subscribe %q: %w", cfg.Durable, err)
	}
	return sub, nil
}
