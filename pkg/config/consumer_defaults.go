// Package config provides centralized tuning defaults for Phase 3 reliability patterns.
// All values are documented in ADR-0022 (DLQ), ADR-0024 (backpressure), and ADR-0025 (idempotency).
package config

import "time"

const (
	// DefaultMaxDeliver is the maximum number of delivery attempts before a message
	// is considered a poison pill and routed to the DLQ (ADR-0022).
	DefaultMaxDeliver = 5

	// DefaultMaxAckPending is the maximum number of outstanding unacknowledged messages
	// a consumer may hold at once. Acts as the primary backpressure lever (ADR-0024).
	DefaultMaxAckPending = 128

	// DefaultAckWait is how long the server waits for an ack before redelivering (ADR-0024).
	DefaultAckWait = 30 * time.Second

	// DefaultFetchBatch is the number of messages requested per Fetch call.
	// Must not exceed DefaultWorkerCount (ADR-0024).
	DefaultFetchBatch = 20

	// DefaultWorkerCount is the fixed size of the consumer worker pool (ADR-0024).
	// Must be >= DefaultFetchBatch.
	DefaultWorkerCount = 20

	// DefaultIdempotencyTTL is how long a processed event ID is retained in the
	// idempotency store before expiry (ADR-0025).
	DefaultIdempotencyTTL = 24 * time.Hour

	// DefaultDLQMaxAge is the retention window for messages in the DLQ stream.
	// This is a starting default; tune as DLQ monitoring tooling matures (ADR-0022).
	DefaultDLQMaxAge = 7 * 24 * time.Hour
)

// DefaultBackOff is the JetStream consumer redelivery backoff schedule.
// 4 intervals produce 5 total delivery attempts when combined with DefaultMaxDeliver (ADR-0022).
var DefaultBackOff = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
}
