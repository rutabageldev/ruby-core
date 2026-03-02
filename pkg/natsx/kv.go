package natsx

import (
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// KV bucket name constants — canonical reference for all NATS KV buckets used by Ruby Core.
//
// Ownership follows the single-writer model (ADR-0002): only the designated service may
// write to a bucket; all other services may read. Do not add writes from a non-owner
// service without updating this comment and ADR-0002.
//
//	Bucket              Owner       Readers     Purpose
//	──────────────────  ──────────  ──────────  ───────────────────────────────────────────
//	KVBucketIdempotency engine      —           Processed event IDs; dedup across restarts
//	KVBucketConfig      engine      gateway     Compiled rule config (passlist + critical entities)
//	KVBucketPresence    presence+engine  —       Presence state: presence svc writes key "{personID}" (raw string);
//	                                            engine presence_notify writes key "{type}.{id}" (JSON {state,updated_at})
//	KVBucketGatewayState gateway    —           Last-seen CloudEvent timestamp per HA entity (reconciler)
const (
	KVBucketIdempotency  = "idempotency"
	KVBucketConfig       = "config"
	KVBucketPresence     = "presence"
	KVBucketGatewayState = "gateway_state"
)

// KV key names published to KVBucketConfig by the engine after loading rules.
const (
	KVKeyConfigPasslist         = "config.engine.passlist"          //nolint:gosec // not a credential
	KVKeyConfigCriticalEntities = "config.engine.critical_entities" //nolint:gosec // not a credential
)

// EnsureConfigKV creates or binds the config KV bucket.
// Owned by the engine; the gateway reads from it.
func EnsureConfigKV(js nats.JetStreamContext) (nats.KeyValue, error) {
	return ensureKV(js, KVBucketConfig, 0)
}

// EnsurePresenceKV creates or binds the presence KV bucket.
// Owned by the engine's presence_notify processor.
func EnsurePresenceKV(js nats.JetStreamContext) (nats.KeyValue, error) {
	return ensureKV(js, KVBucketPresence, 0)
}

// EnsureGatewayStateKV creates or binds the gateway_state KV bucket.
// Owned by the gateway reconciler.
func EnsureGatewayStateKV(js nats.JetStreamContext) (nats.KeyValue, error) {
	return ensureKV(js, KVBucketGatewayState, 0)
}

// ensureKV creates a KV bucket if it does not already exist. Idempotent.
// A zero ttl means no per-key TTL (keys persist until explicitly deleted or the
// bucket is destroyed).
func ensureKV(js nats.JetStreamContext, bucket string, ttl time.Duration) (nats.KeyValue, error) {
	kv, err := js.KeyValue(bucket)
	if err == nil {
		return kv, nil
	}
	if !errors.Is(err, nats.ErrBucketNotFound) {
		return nil, fmt.Errorf("kv: bind %q: %w", bucket, err)
	}
	kv, err = js.CreateKeyValue(&nats.KeyValueConfig{
		Bucket: bucket,
		TTL:    ttl,
	})
	if err != nil {
		return nil, fmt.Errorf("kv: create %q: %w", bucket, err)
	}
	return kv, nil
}
