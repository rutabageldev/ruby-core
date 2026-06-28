// Package idempotency provides a hybrid idempotency tracking store backed by
// an in-memory TTL cache (fast path) and a NATS KV bucket (durable path).
// See ADR-0025 for design rationale and the acknowledged race window.
package idempotency

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// Store is the interface for checking and marking processed event IDs.
type Store interface {
	// Seen reports whether id has been seen and processed recently.
	Seen(id string) (bool, error)
	// Mark records id as processed. Should be called after the side-effect succeeds.
	Mark(id string) error
	// Close releases background resources (e.g. eviction goroutine).
	Close() error
}

// hybridStore checks the in-memory cache first (fast path), then NATS KV (durable path).
// Writes go to both stores. KV write failures are logged but do not surface as errors,
// consistent with ADR-0025's acknowledged small race window.
type hybridStore struct {
	mem *memStore
	kv  *kvStore
}

// NewHybridStore returns a Store backed by an in-memory TTL cache and a NATS KV bucket.
// In production, pass the nats.KeyValue returned by CreateOrBindKVBucket.
func NewHybridStore(kv nats.KeyValue, ttl time.Duration) Store {
	return &hybridStore{
		mem: newMemStore(ttl),
		kv:  &kvStore{kv: kv},
	}
}

// Seen checks the memory cache first; on a miss, falls through to the KV store.
// A KV hit also warms the memory cache to speed up subsequent checks.
func (h *hybridStore) Seen(id string) (bool, error) {
	if ok, err := h.mem.Seen(id); err != nil || ok {
		return ok, err
	}
	ok, err := h.kv.Seen(id)
	if err != nil {
		return false, err
	}
	if ok {
		// Warm the memory cache for future lookups.
		_ = h.mem.Mark(id)
	}
	return ok, nil
}

// Mark writes id to both the memory cache and the KV bucket.
// A KV write failure is logged but not returned (ADR-0025 race window acknowledgement).
func (h *hybridStore) Mark(id string) error {
	if err := h.mem.Mark(id); err != nil {
		return err
	}
	if err := h.kv.Mark(id); err != nil {
		slog.Warn("idempotency: kv mark failed (non-fatal, race window acknowledged)",
			slog.String("id", id),
			slog.String("error", err.Error()),
		)
	}
	return nil
}

// Close stops the memory store's background eviction goroutine.
func (h *hybridStore) Close() error {
	return h.mem.Close()
}

// CreateOrBindKVBucket opens the named NATS KV bucket or creates it with the given TTL.
// Idempotent: safe to call on every service start.
//
// A bucket's TTL is fixed at creation; binding an existing bucket does NOT apply a new
// ttl. So when the requested ttl differs from the live bucket's configured TTL, we WARN
// rather than silently run with the stale value — the operator must delete + recreate
// the bucket to apply the change (see the idempotency-KV runbook). This catches the
// footgun where lowering DefaultIdempotencyTTL has no effect until the bucket is purged.
func CreateOrBindKVBucket(js nats.JetStreamContext, bucket string, ttl time.Duration) (nats.KeyValue, error) {
	kv, err := js.KeyValue(bucket)
	if err == nil {
		if st, serr := kv.Status(); serr == nil && st.TTL() != ttl {
			slog.Warn("idempotency: kv bucket TTL differs from configured value — delete+recreate the bucket to apply",
				slog.String("bucket", bucket),
				slog.Duration("bucket_ttl", st.TTL()),
				slog.Duration("configured_ttl", ttl),
				slog.Uint64("entries", st.Values()),
			)
		}
		return kv, nil
	}
	if !errors.Is(err, nats.ErrBucketNotFound) {
		return nil, fmt.Errorf("idempotency: kv open %q: %w", bucket, err)
	}
	kv, err = js.CreateKeyValue(&nats.KeyValueConfig{
		Bucket: bucket,
		TTL:    ttl,
	})
	if err != nil {
		return nil, fmt.Errorf("idempotency: kv create %q: %w", bucket, err)
	}
	return kv, nil
}
