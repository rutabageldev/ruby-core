//go:build fast

package ha

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

// TestPushSkippedWhenHADisabled verifies that an empty base URL (HA_INGEST_ENABLED=
// false on non-prod) makes PushState/Notify no-ops, so non-prod engines never push
// to the shared HA (ADR-0033).
func TestPushSkippedWhenHADisabled(t *testing.T) {
	c := &Client{baseURL: "", log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := c.PushState(context.Background(), "sensor.ada_x", "1", nil); err != nil {
		t.Errorf("PushState with empty base URL = %v, want nil", err)
	}
	if err := c.Notify(context.Background(), "mobile_app_x", "t", "m"); err != nil {
		t.Errorf("Notify with empty base URL = %v, want nil", err)
	}
}
