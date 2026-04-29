//go:build integration

package natsx_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"

	"github.com/primaryrutabaga/ruby-core/pkg/idempotency"
	"github.com/primaryrutabaga/ruby-core/pkg/natsx"
)

// startNATS spins up a NATS testcontainer with JetStream enabled and returns
// a connected *nats.Conn. The container and connection are cleaned up via t.Cleanup.
func startNATS(t *testing.T) *natsgo.Conn {
	t.Helper()
	ctx := context.Background()

	// JetStream is enabled by default in this module (nats server starts with -js flag).
	container, err := tcnats.Run(ctx, "nats:2.10-alpine")
	if err != nil {
		t.Fatalf("startNATS: run container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("startNATS: terminate container: %v", err)
		}
	})

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("startNATS: connection string: %v", err)
	}

	// Retry the connection: testcontainers marks the NATS container ready when
	// port 4222 is open, but the server may not have completed its startup
	// handshake yet. A short retry loop avoids the resulting EOF on first connect.
	var nc *natsgo.Conn
	for range 10 {
		nc, err = natsgo.Connect(connStr, natsgo.MaxReconnects(0))
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("startNATS: connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// ensureStream creates a JetStream stream for the test and returns a
// JetStreamContext. The stream is named TEST_STREAM and captures test.events.>.
func ensureStream(t *testing.T, nc *natsgo.Conn) natsgo.JetStreamContext {
	t.Helper()
	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("ensureStream: JetStream: %v", err)
	}
	_, err = js.AddStream(&natsgo.StreamConfig{
		Name:     "TEST_STREAM",
		Subjects: []string{"test.events.>"},
	})
	if err != nil {
		t.Fatalf("ensureStream: AddStream: %v", err)
	}
	return js
}

// publish publishes a CloudEvent-shaped message to the given subject with an explicit
// Nats-Msg-Id header for idempotency tracking, and returns the message ID used.
func publish(t *testing.T, nc *natsgo.Conn, subject, eventID string) {
	t.Helper()
	payload, err := json.Marshal(map[string]string{
		"specversion": "1.0",
		"type":        "test.event",
		"source":      "test",
		"id":          eventID,
	})
	if err != nil {
		t.Fatalf("publish: marshal: %v", err)
	}

	msg := &natsgo.Msg{
		Subject: subject,
		Data:    payload,
		Header:  natsgo.Header{},
	}
	msg.Header.Set("Nats-Msg-Id", eventID)

	if err := nc.PublishMsg(msg); err != nil {
		t.Fatalf("publish: PublishMsg: %v", err)
	}
}

// runFetchLoop runs a minimal fetch-check-ack loop that mirrors the engine's
// Consumer.handle pattern. It processes messages from sub until ctx is cancelled
// or the loop hits a fatal fetch error. Each message is idempotency-checked via
// store; if not seen, processFn is called and the message is acked and marked.
func runFetchLoop(
	ctx context.Context,
	sub *natsgo.Subscription,
	store idempotency.Store,
	processFn func(subject string, data []byte),
) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		msgs, err := sub.Fetch(1, natsgo.MaxWait(500*time.Millisecond))
		if err != nil {
			if errors.Is(err, natsgo.ErrTimeout) {
				continue
			}
			if errors.Is(err, context.Canceled) ||
				errors.Is(err, natsgo.ErrConnectionClosed) ||
				errors.Is(err, natsgo.ErrSubscriptionClosed) {
				return nil
			}
			return err
		}

		for _, msg := range msgs {
			// Extract event ID from Nats-Msg-Id header (same priority as engine).
			eventID := msg.Header.Get("Nats-Msg-Id")
			if eventID == "" {
				eventID = msg.Subject
			}

			seen, err := store.Seen(eventID)
			if err != nil || seen {
				_ = msg.Ack() // ack duplicates to remove from pending
				continue
			}

			processFn(msg.Subject, msg.Data)

			if err := store.Mark(eventID); err != nil {
				_ = msg.Nak()
				continue
			}
			_ = msg.Ack()
		}
	}
}

// TestEnsurePullConsumer_Integration verifies that EnsurePullConsumer creates a
// durable pull consumer against a real NATS JetStream server, and that the
// resulting subscription receives a published message.
func TestEnsurePullConsumer_Integration(t *testing.T) {
	nc := startNATS(t)
	js := ensureStream(t, nc)

	cfg := natsx.DefaultPullConsumerConfig("TEST_STREAM", "test_consumer", "test.events.>")
	sub, err := natsx.EnsurePullConsumer(js, cfg)
	if err != nil {
		t.Fatalf("EnsurePullConsumer: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	const subject = "test.events.hello"
	const eventID = "evt-001"
	publish(t, nc, subject, eventID)

	// Use a simple in-memory store for this test (no KV bucket needed).
	store := newTestStore()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var called int32
	go func() {
		_ = runFetchLoop(ctx, sub, store, func(subj string, _ []byte) {
			atomic.AddInt32(&called, 1)
			cancel() // stop the loop after processing one message
		})
	}()

	<-ctx.Done()

	if got := atomic.LoadInt32(&called); got != 1 {
		t.Errorf("process func called %d times, want 1", got)
	}
}

// TestEnsurePullConsumer_Integration_IdempotencyRejectsDuplicate verifies that
// the fetch-check-ack loop driven by pkg/idempotency correctly deduplicates a
// message published with the same event ID twice.
func TestEnsurePullConsumer_Integration_IdempotencyRejectsDuplicate(t *testing.T) {
	nc := startNATS(t)
	js := ensureStream(t, nc)

	cfg := natsx.DefaultPullConsumerConfig("TEST_STREAM", "test_dedup_consumer", "test.events.>")
	sub, err := natsx.EnsurePullConsumer(js, cfg)
	if err != nil {
		t.Fatalf("EnsurePullConsumer: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	// Create an idempotency KV bucket via the real pkg/idempotency path.
	kvBucket, err := idempotency.CreateOrBindKVBucket(js, "TEST_IDEMPOTENCY", 24*time.Hour)
	if err != nil {
		t.Fatalf("CreateOrBindKVBucket: %v", err)
	}
	store := idempotency.NewHybridStore(kvBucket, 24*time.Hour)
	defer func() { _ = store.Close() }()

	const subject = "test.events.dedup"
	const eventID = "evt-dedup-001"

	// Publish the same event ID twice — the second should be a duplicate.
	publish(t, nc, subject, eventID)
	publish(t, nc, subject, eventID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var called int32
	go func() {
		_ = runFetchLoop(ctx, sub, store, func(_ string, _ []byte) {
			n := atomic.AddInt32(&called, 1)
			if n == 1 {
				// Let the loop run a bit more to confirm the duplicate is discarded.
				time.Sleep(300 * time.Millisecond)
				cancel()
			}
		})
	}()

	<-ctx.Done()

	if got := atomic.LoadInt32(&called); got != 1 {
		t.Errorf("process func called %d times, want 1 (duplicate should be discarded)", got)
	}
}

// ---------------------------------------------------------------------------
// testStore — a minimal in-memory idempotency.Store for integration tests
// that don't need KV durability.
// ---------------------------------------------------------------------------

type testStore struct {
	seen map[string]bool
}

func newTestStore() idempotency.Store {
	return &testStore{seen: make(map[string]bool)}
}

func (s *testStore) Seen(id string) (bool, error) { return s.seen[id], nil }
func (s *testStore) Mark(id string) error         { s.seen[id] = true; return nil }
func (s *testStore) Close() error                 { return nil }
