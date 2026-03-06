//go:build fast

package idempotency

import (
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// ---------------------------------------------------------------------------
// Test helpers: mock kvClient and nats.KeyValueEntry
// ---------------------------------------------------------------------------

// mockKVClient implements kvClient for unit tests.
type mockKVClient struct {
	data     map[string][]byte
	getErr   error
	putErr   error
	putCalls []string // records keys passed to Put
}

func newMockKV() *mockKVClient {
	return &mockKVClient{data: make(map[string][]byte)}
}

func (m *mockKVClient) Get(key string) (nats.KeyValueEntry, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	v, ok := m.data[key]
	if !ok {
		return nil, nats.ErrKeyNotFound
	}
	return &mockEntry{val: v}, nil
}

func (m *mockKVClient) Put(key string, value []byte) (uint64, error) {
	m.putCalls = append(m.putCalls, key)
	if m.putErr != nil {
		return 0, m.putErr
	}
	if m.data == nil {
		m.data = make(map[string][]byte)
	}
	m.data[key] = value
	return 1, nil
}

// mockEntry implements nats.KeyValueEntry.
type mockEntry struct{ val []byte }

func (e *mockEntry) Bucket() string             { return "test" }
func (e *mockEntry) Key() string                { return "" }
func (e *mockEntry) Value() []byte              { return e.val }
func (e *mockEntry) Revision() uint64           { return 1 }
func (e *mockEntry) Delta() uint64              { return 0 }
func (e *mockEntry) Created() time.Time         { return time.Time{} }
func (e *mockEntry) Operation() nats.KeyValueOp { return nats.KeyValuePut }

// ---------------------------------------------------------------------------
// memStore tests
// ---------------------------------------------------------------------------

func TestMemStore_Seen_NotPresent(t *testing.T) {
	s := newMemStore(time.Hour)
	defer func() { _ = s.Close() }()

	ok, err := s.Seen("event-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("Seen() = true for unknown id, want false")
	}
}

func TestMemStore_MarkThenSeen(t *testing.T) {
	s := newMemStore(time.Hour)
	defer func() { _ = s.Close() }()

	if err := s.Mark("event-001"); err != nil {
		t.Fatalf("Mark() error: %v", err)
	}
	ok, err := s.Seen("event-001")
	if err != nil {
		t.Fatalf("Seen() error: %v", err)
	}
	if !ok {
		t.Error("Seen() = false after Mark(), want true")
	}
}

func TestMemStore_TTLExpiry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TTL expiry test in short mode")
	}
	ttl := 50 * time.Millisecond
	s := newMemStore(ttl)
	defer func() { _ = s.Close() }()

	if err := s.Mark("event-exp"); err != nil {
		t.Fatalf("Mark() error: %v", err)
	}
	// Wait longer than TTL
	time.Sleep(ttl + 10*time.Millisecond)

	ok, err := s.Seen("event-exp")
	if err != nil {
		t.Fatalf("Seen() error: %v", err)
	}
	if ok {
		t.Error("Seen() = true after TTL expiry, want false")
	}
}

func TestMemStore_Close_StopsGoroutine(t *testing.T) {
	s := newMemStore(time.Hour)
	// Closing twice should not panic.
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	// Verify stopCh is closed by checking a subsequent send would not block.
	select {
	case <-s.stopCh:
		// good — channel is closed
	default:
		t.Error("stopCh was not closed after Close()")
	}
}

// ---------------------------------------------------------------------------
// kvStore tests
// ---------------------------------------------------------------------------

func TestKVStore_Seen_NotPresent(t *testing.T) {
	s := &kvStore{kv: newMockKV()}
	ok, err := s.Seen("event-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("Seen() = true for missing key, want false")
	}
}

func TestKVStore_Seen_Present(t *testing.T) {
	m := newMockKV()
	m.data["event_001"] = []byte{1} // pre-populate (sanitized key)
	s := &kvStore{kv: m}

	ok, err := s.Seen("event:001") // colon → underscore sanitization
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("Seen() = false for present key, want true")
	}
}

func TestKVStore_Seen_GetError(t *testing.T) {
	m := newMockKV()
	m.getErr = errors.New("kv read error")
	s := &kvStore{kv: m}

	_, err := s.Seen("event-001")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestKVStore_Mark_SanitizesKey(t *testing.T) {
	m := newMockKV()
	s := &kvStore{kv: m}

	if err := s.Mark("HA_EVENTS:42"); err != nil {
		t.Fatalf("Mark() error: %v", err)
	}
	if len(m.putCalls) != 1 {
		t.Fatalf("Put called %d times, want 1", len(m.putCalls))
	}
	if m.putCalls[0] != "HA_EVENTS_42" {
		t.Errorf("Put key = %q, want %q", m.putCalls[0], "HA_EVENTS_42")
	}
}

func TestKVStore_Mark_PutError(t *testing.T) {
	m := newMockKV()
	m.putErr = errors.New("kv write error")
	s := &kvStore{kv: m}

	err := s.Mark("event-001")
	if err == nil {
		t.Fatal("expected error from Put failure, got nil")
	}
}

// ---------------------------------------------------------------------------
// hybridStore tests
// ---------------------------------------------------------------------------

func TestHybridStore_Seen_MissOnBoth(t *testing.T) {
	h := &hybridStore{
		mem: newMemStore(time.Hour),
		kv:  &kvStore{kv: newMockKV()},
	}
	defer func() { _ = h.Close() }()

	ok, err := h.Seen("event-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("Seen() = true on empty store, want false")
	}
}

func TestHybridStore_MarkThenSeen_HitsMemCache(t *testing.T) {
	m := newMockKV()
	h := &hybridStore{
		mem: newMemStore(time.Hour),
		kv:  &kvStore{kv: m},
	}
	defer func() { _ = h.Close() }()

	if err := h.Mark("event-001"); err != nil {
		t.Fatalf("Mark() error: %v", err)
	}

	// Force getErr so kv.Seen would fail — mem cache should be hit first
	m.getErr = errors.New("should not be called")

	ok, err := h.Seen("event-001")
	if err != nil {
		t.Fatalf("Seen() error: %v", err)
	}
	if !ok {
		t.Error("Seen() = false after Mark(), want true")
	}
}

func TestHybridStore_KVWriteFailureInMark_ReturnsNil(t *testing.T) {
	m := newMockKV()
	m.putErr = errors.New("kv write error")
	h := &hybridStore{
		mem: newMemStore(time.Hour),
		kv:  &kvStore{kv: m},
	}
	defer func() { _ = h.Close() }()

	// KV write failure should not surface as an error from Mark (ADR-0025 race window).
	if err := h.Mark("event-001"); err != nil {
		t.Errorf("Mark() returned error on KV write failure, want nil: %v", err)
	}
}

func TestHybridStore_KVHit_WarmsMemCache(t *testing.T) {
	m := newMockKV()
	m.data["event_001"] = []byte{1}
	h := &hybridStore{
		mem: newMemStore(time.Hour),
		kv:  &kvStore{kv: m},
	}
	defer func() { _ = h.Close() }()

	// First Seen: miss in mem, hit in kv (warms mem cache)
	ok, err := h.Seen("event:001")
	if err != nil || !ok {
		t.Fatalf("first Seen(): ok=%v err=%v, want true nil", ok, err)
	}

	// Poison kv so any further kv.Get call fails
	m.getErr = errors.New("should not reach kv on second call")

	// Second Seen: should be served from mem cache now
	ok, err = h.Seen("event:001")
	if err != nil {
		t.Fatalf("second Seen() error: %v", err)
	}
	if !ok {
		t.Error("second Seen() = false, want true (mem cache should have warmed)")
	}
}

// ---------------------------------------------------------------------------
// sanitizeKey tests
// ---------------------------------------------------------------------------

func TestSanitizeKey(t *testing.T) {
	testCases := []struct {
		id   string
		want string
	}{
		{"HA_EVENTS:42", "HA_EVENTS_42"},
		{"plain", "plain"},
		{"with.dot", "with.dot"}, // dots are preserved
		{"a:b:c", "a_b_c"},       // multiple colons replaced
		{"a.b_c-d", "a.b_c-d"},   // dots, underscores, dashes unchanged
	}

	for _, tc := range testCases {
		t.Run(tc.id, func(t *testing.T) {
			got := sanitizeKey(tc.id)
			if got != tc.want {
				t.Errorf("sanitizeKey(%q) = %q, want %q", tc.id, got, tc.want)
			}
		})
	}
}
