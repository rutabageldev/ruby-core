//go:build fast

package boot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestOnNATSClosed(t *testing.T) {
	t.Run("graceful shutdown (ctx cancelled) is a no-op", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // SIGTERM path cancels ctx before nc.Close() fires the handler
		var lost atomic.Bool
		OnNATSClosed(ctx, cancel, &lost, discardLog())(nil)
		if lost.Load() {
			t.Error("graceful shutdown must not be flagged as a crash")
		}
	})

	t.Run("outage (ctx live) flags loss and cancels", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var lost atomic.Bool
		OnNATSClosed(ctx, cancel, &lost, discardLog())(nil)
		if !lost.Load() {
			t.Error("permanent NATS loss must set the crash flag")
		}
		if ctx.Err() == nil {
			t.Error("permanent NATS loss must cancel the root context")
		}
	})
}

func TestWithRetryLabeled(t *testing.T) {
	t.Run("success on first attempt does not retry", func(t *testing.T) {
		calls := 0
		if err := withRetryLabeled("test", func() error { calls++; return nil }); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 1 {
			t.Errorf("calls = %d, want 1", calls)
		}
	})

	t.Run("retries then succeeds", func(t *testing.T) {
		calls := 0
		err := withRetryLabeled("test", func() error {
			calls++
			if calls < 2 {
				return errors.New("transient")
			}
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 2 {
			t.Errorf("calls = %d, want 2", calls)
		}
	})
}
