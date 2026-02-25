package main

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// ---------------------------------------------------------------------------
// Mock idempotency.Store for testing
// ---------------------------------------------------------------------------

type mockStore struct {
	seen    map[string]bool
	seeErr  error
	markErr error
	marked  []string
}

func newMockStore() *mockStore {
	return &mockStore{seen: make(map[string]bool)}
}

func (m *mockStore) Seen(id string) (bool, error) {
	if m.seeErr != nil {
		return false, m.seeErr
	}
	return m.seen[id], nil
}

func (m *mockStore) Mark(id string) error {
	m.marked = append(m.marked, id)
	return m.markErr
}

func (m *mockStore) Close() error { return nil }

// ---------------------------------------------------------------------------
// Consumer.decide tests
// ---------------------------------------------------------------------------

func TestDecide_Success(t *testing.T) {
	c := &Consumer{
		idStore: newMockStore(),
		process: func([]byte) error { return nil },
	}

	result, err := c.decide("evt-001", []byte("data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != resultAck {
		t.Errorf("result = %d, want resultAck (%d)", result, resultAck)
	}
}

func TestDecide_ProcessFailure(t *testing.T) {
	c := &Consumer{
		idStore: newMockStore(),
		process: func([]byte) error { return errors.New("transient error") },
	}

	result, err := c.decide("evt-002", []byte("data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != resultNak {
		t.Errorf("result = %d, want resultNak (%d)", result, resultNak)
	}
}

func TestDecide_Duplicate(t *testing.T) {
	store := newMockStore()
	store.seen["evt-003"] = true

	processed := false
	c := &Consumer{
		idStore: store,
		process: func([]byte) error {
			processed = true
			return nil
		},
	}

	result, err := c.decide("evt-003", []byte("data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != resultSkip {
		t.Errorf("result = %d, want resultSkip (%d)", result, resultSkip)
	}
	if processed {
		t.Error("process() was called for a duplicate event, want it skipped")
	}
}

func TestDecide_IdempotencyCheckError(t *testing.T) {
	store := newMockStore()
	store.seeErr = errors.New("kv unreachable")
	c := &Consumer{
		idStore: store,
		process: func([]byte) error { return nil },
	}

	result, err := c.decide("evt-004", []byte("data"))
	if err == nil {
		t.Fatal("expected error from idempotency failure, got nil")
	}
	if result != resultNak {
		t.Errorf("result = %d, want resultNak (%d) on idempotency error", result, resultNak)
	}
}

// ---------------------------------------------------------------------------
// extractEventID tests
// ---------------------------------------------------------------------------

func TestExtractEventID_NatsMsgIdHeader(t *testing.T) {
	headers := nats.Header{}
	headers.Set("Nats-Msg-Id", "header-id-123")
	meta := &nats.MsgMetadata{
		Stream:   "HA_EVENTS",
		Sequence: nats.SequencePair{Stream: 1},
	}

	id := extractEventID(headers, []byte(`{"id":"ce-id-456"}`), meta)
	if id != "header-id-123" {
		t.Errorf("id = %q, want %q (Nats-Msg-Id header should take priority)", id, "header-id-123")
	}
}

func TestExtractEventID_CloudEventID(t *testing.T) {
	meta := &nats.MsgMetadata{
		Stream:   "HA_EVENTS",
		Sequence: nats.SequencePair{Stream: 7},
	}

	id := extractEventID(nil, []byte(`{"id":"ce-abc","source":"ha","type":"light"}`), meta)
	if id != "ce-abc" {
		t.Errorf("id = %q, want %q (CloudEvent id field)", id, "ce-abc")
	}
}

func TestExtractEventID_FallbackSequence(t *testing.T) {
	meta := &nats.MsgMetadata{
		Stream:   "HA_EVENTS",
		Sequence: nats.SequencePair{Stream: 42},
	}

	id := extractEventID(nil, []byte(`not-json`), meta)
	want := "HA_EVENTS.42"
	if id != want {
		t.Errorf("id = %q, want %q (stream sequence fallback)", id, want)
	}
}

func TestExtractEventID_FallbackOnEmptyCloudEventID(t *testing.T) {
	meta := &nats.MsgMetadata{
		Stream:   "HA_EVENTS",
		Sequence: nats.SequencePair{Stream: 99},
	}

	id := extractEventID(nil, []byte(`{"id":"","source":"ha"}`), meta)
	want := "HA_EVENTS.99"
	if id != want {
		t.Errorf("id = %q, want %q (empty CloudEvent id should fall back to sequence)", id, want)
	}
}

// ---------------------------------------------------------------------------
// NewConsumer validation tests
// ---------------------------------------------------------------------------

func TestNewConsumer_FetchBatchExceedsWorkerN(t *testing.T) {
	_, err := NewConsumer(nil, newMockStore(), func([]byte) error { return nil }, 10, 11, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error when batchSize > workerN, got nil")
	}
}

func TestNewConsumer_ValidConfig(t *testing.T) {
	c, err := NewConsumer(nil, newMockStore(), func([]byte) error { return nil }, 20, 20, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil Consumer")
	}
}

// ---------------------------------------------------------------------------
// nakDelay tests
// ---------------------------------------------------------------------------

func TestNakDelay(t *testing.T) {
	backOff := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	cases := []struct {
		numDelivered uint64
		want         time.Duration
	}{
		{1, 1 * time.Second}, // first attempt → 1s delay before retry
		{2, 2 * time.Second}, // second attempt → 2s
		{3, 4 * time.Second}, // third → 4s
		{4, 8 * time.Second}, // fourth → 8s
		{5, 0},               // fifth (final, MaxDeliver=5) → no delay, advisory fires promptly
		{6, 0},               // beyond schedule → no delay
	}
	for _, tc := range cases {
		got := nakDelay(backOff, tc.numDelivered)
		if got != tc.want {
			t.Errorf("nakDelay(backOff, %d) = %v, want %v", tc.numDelivered, got, tc.want)
		}
	}
}

func TestNakDelay_EmptyBackOff(t *testing.T) {
	if d := nakDelay(nil, 3); d != 0 {
		t.Errorf("nakDelay(nil, 3) = %v, want 0", d)
	}
}

// ---------------------------------------------------------------------------
// maxDeliverAdvisory parsing test
// ---------------------------------------------------------------------------

func TestMaxDeliverAdvisory_Parse(t *testing.T) {
	payload := `{
		"type": "io.nats.jetstream.advisory.v1.max_deliver",
		"id": "some-id",
		"timestamp": "2026-02-23T10:00:00Z",
		"stream": "HA_EVENTS",
		"consumer": "engine_processor",
		"stream_seq": 42,
		"consumer_seq": 42,
		"deliveries": 5
	}`

	var adv maxDeliverAdvisory
	if err := json.Unmarshal([]byte(payload), &adv); err != nil {
		t.Fatalf("unmarshal advisory: %v", err)
	}
	if adv.Stream != "HA_EVENTS" {
		t.Errorf("Stream = %q, want %q", adv.Stream, "HA_EVENTS")
	}
	if adv.StreamSeq != 42 {
		t.Errorf("StreamSeq = %d, want 42", adv.StreamSeq)
	}
	if adv.Deliveries != 5 {
		t.Errorf("Deliveries = %d, want 5", adv.Deliveries)
	}
}
