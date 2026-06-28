//go:build fast

package config

import "testing"

// The idempotency TTL only needs to outlive the maximum redelivery window before a
// message is DLQ'd; it must be comfortably above it (so no in-flight retry slips past
// dedup) yet small enough to keep the bucket from bloating (ADR-0025, PLAN-0034).
func TestIdempotencyTTLBoundedByRedeliveryWindow(t *testing.T) {
	var backoff int64
	for _, d := range DefaultBackOff {
		backoff += d.Nanoseconds()
	}
	// Max retry window ≈ MaxDeliver × AckWait + Σ BackOff.
	window := int64(DefaultMaxDeliver)*DefaultAckWait.Nanoseconds() + backoff

	if DefaultIdempotencyTTL.Nanoseconds() <= window {
		t.Errorf("DefaultIdempotencyTTL (%s) must exceed the redelivery window (%dns)",
			DefaultIdempotencyTTL, window)
	}
	if DefaultIdempotencyTTL.Minutes() > 30 {
		t.Errorf("DefaultIdempotencyTTL (%s) is larger than 30m — risks bucket bloat",
			DefaultIdempotencyTTL)
	}
}
