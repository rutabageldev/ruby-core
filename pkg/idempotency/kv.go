package idempotency

import (
	"errors"
	"strings"

	"github.com/nats-io/nats.go"
)

// kvClient is the subset of nats.KeyValue used by kvStore.
// The narrow interface enables unit testing without a live NATS connection.
type kvClient interface {
	Get(key string) (nats.KeyValueEntry, error)
	Put(key string, value []byte) (uint64, error)
}

// kvStore is a NATS KV-backed idempotency store.
// TTL is enforced at the bucket level (KeyValueConfig.TTL).
type kvStore struct {
	kv kvClient
}

// Seen reports whether id exists in the KV bucket (and has not expired).
func (s *kvStore) Seen(id string) (bool, error) {
	_, err := s.kv.Get(sanitizeKey(id))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, nats.ErrKeyNotFound) {
		return false, nil
	}
	return false, err
}

// Mark writes id to the KV bucket as a presence marker.
func (s *kvStore) Mark(id string) error {
	_, err := s.kv.Put(sanitizeKey(id), []byte{1})
	return err
}

// sanitizeKey replaces characters not valid in NATS KV keys.
// NATS KV allows [a-zA-Z0-9_\-.]; colons are the only common problematic character
// in our IDs (e.g. "HA_EVENTS:42"). Dots are preserved to avoid colliding distinct IDs.
func sanitizeKey(id string) string {
	return strings.ReplaceAll(id, ":", "_")
}
