// Package config provides centralized tuning defaults for Phase 3 reliability patterns
// and Phase 4 audit infrastructure.
// All values are documented in ADR-0022 (DLQ), ADR-0024 (backpressure), ADR-0025 (idempotency),
// and ADR-0019 (audit trail).
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

	// DefaultIdempotencyTTL is how long a processed event ID is retained in the shared
	// idempotency store before expiry (ADR-0025). Dedup only needs to outlive the
	// maximum redelivery window — MaxDeliver(5) × AckWait(30s) + Σ BackOff(15s) ≈ 165s
	// before a message is DLQ'd — so 30m is an ~11× safety margin. It was previously
	// 24h, which retained ~470× more entries than necessary: the consumer marks every
	// processed event including the high-volume state_changed firehose, so the bucket
	// bloated (~97k entries in prod), slowing KV ops until marks timed out and
	// redeliveries leaked through. Correctness no longer depends on this window — the
	// calendar write-through is idempotent at Google (ADR-0042) — so this is bloat
	// hygiene, not the dedup guarantee.
	DefaultIdempotencyTTL = 30 * time.Minute

	// DefaultDLQMaxAge is the retention window for messages in the DLQ stream.
	// This is a starting default; tune as DLQ monitoring tooling matures (ADR-0022).
	DefaultDLQMaxAge = 7 * 24 * time.Hour

	// DefaultAuditMaxAge is the minimum retention for the AUDIT_EVENTS stream.
	// Must survive a prolonged audit-sink outage before messages are discarded (ADR-0019).
	DefaultAuditMaxAge = 72 * time.Hour

	// DefaultCommandsMaxAge is the retention window for the COMMANDS stream.
	// Stale command messages (push notifications) are not worth replaying after this window.
	DefaultCommandsMaxAge = 1 * time.Hour

	// DefaultHAEventsMaxAge bounds the HA_EVENTS firehose (ADR-0034). It must exceed
	// the maximum consumer retry window (MaxDeliver × AckWait + BackOff ≈ a few minutes)
	// so original payloads stay available for DLQ routing, and comfortably covers
	// reconciliation/replay after an outage. Previously unbounded, the stream grew until
	// it exhausted the JetStream store and starved the discard=new KV buckets.
	DefaultHAEventsMaxAge = 48 * time.Hour
)

// Per-stream byte caps (ADR-0034) — defense in depth so no single stream can exhaust
// the JetStream account store and starve the discard=new KV buckets. Each stream uses
// discard=old, so it self-evicts at its cap rather than failing new writes; the sum
// sits under the server's max_file_store. Age limits remain the primary bound.
const (
	MaxBytesHAEvents int64 = 512 * 1024 * 1024 // 512 MiB
	MaxBytesAudit    int64 = 256 * 1024 * 1024 // 256 MiB
	MaxBytesDLQ      int64 = 64 * 1024 * 1024  // 64 MiB
	MaxBytesCommands int64 = 16 * 1024 * 1024  // 16 MiB
	MaxBytesPresence int64 = 32 * 1024 * 1024  // 32 MiB

	// Audit-sink consumer defaults — lower throughput than the engine consumer
	// because audit events are emitted only on critical actions (low volume).

	// DefaultAuditSinkWorkerCount is the worker pool size for the audit-sink consumer.
	DefaultAuditSinkWorkerCount = 5

	// DefaultAuditSinkFetchBatch is the fetch batch size for the audit-sink consumer.
	// Must not exceed DefaultAuditSinkWorkerCount.
	DefaultAuditSinkFetchBatch = 5

	// DefaultAuditSinkMaxAckPending caps outstanding unacknowledged audit messages.
	DefaultAuditSinkMaxAckPending = 32
)

// DefaultBackOff is the JetStream consumer redelivery backoff schedule.
// 4 intervals produce 5 total delivery attempts when combined with DefaultMaxDeliver (ADR-0022).
var DefaultBackOff = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
}
