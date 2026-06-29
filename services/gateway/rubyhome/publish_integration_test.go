//go:build integration

package rubyhome_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"

	"github.com/primaryrutabaga/ruby-core/pkg/natsx"
	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/gateway/ada"
	"github.com/primaryrutabaga/ruby-core/services/gateway/rubyhome"
)

// startNATS spins up a JetStream-enabled NATS testcontainer and returns a connected
// conn (cleaned up via t.Cleanup). Mirrors pkg/natsx/consumer_integration_test.go.
func startNATS(t *testing.T) *natsgo.Conn {
	t.Helper()
	ctx := context.Background()

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

// TestRubyHomeAndAdaLandInHAEvents proves the new ruby_home write path publishes
// CloudEvents to the correct ha.events.* subjects in the HA_EVENTS stream, and that
// the existing ada path still routes unchanged (no regression from the dual path).
func TestRubyHomeAndAdaLandInHAEvents(t *testing.T) {
	nc := startNATS(t)
	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	if err := natsx.EnsureHAEventsStream(js); err != nil {
		t.Fatalf("EnsureHAEventsStream: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// New domain-neutral path: a calendar write event.
	if err := rubyhome.Publish(context.Background(), nc, map[string]any{
		"event":           "calendar.event.upsert",
		"summary":         "Dentist",
		"idempotency_key": "test-key-1",
	}, log); err != nil {
		t.Fatalf("rubyhome.Publish: %v", err)
	}
	assertCloudEventStored(t, js, schemas.HomeEventCalendarUpsert)

	// New domain-neutral path: a childcare overlay write event.
	if err := rubyhome.Publish(context.Background(), nc, map[string]any{
		"event":        "ruby_home.childcare.provider.upsert",
		"display_name": "Maya",
	}, log); err != nil {
		t.Fatalf("rubyhome.Publish (childcare): %v", err)
	}
	assertCloudEventStored(t, js, schemas.HomeEventChildcareProviderUpsert)

	// Regression: the existing ada path still routes onto HA_EVENTS unchanged.
	if err := ada.Publish(context.Background(), nc, map[string]any{
		"event": "ada.diaper.log",
		"type":  "wet",
	}, log); err != nil {
		t.Fatalf("ada.Publish: %v", err)
	}
	assertCloudEventStored(t, js, schemas.AdaEventDiaperLogged)
}

// assertCloudEventStored verifies a CloudEvent was stored in HA_EVENTS on subject,
// with Type == subject and Source == ruby_gateway.
func assertCloudEventStored(t *testing.T, js natsgo.JetStreamContext, subject string) {
	t.Helper()

	var (
		msg *natsgo.RawStreamMsg
		err error
	)
	for range 20 {
		msg, err = js.GetLastMsg("HA_EVENTS", subject)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("no message stored on %q: %v", subject, err)
	}

	var evt schemas.CloudEvent
	if err := json.Unmarshal(msg.Data, &evt); err != nil {
		t.Fatalf("unmarshal CloudEvent on %q: %v", subject, err)
	}
	if evt.Type != subject {
		t.Errorf("CloudEvent.Type = %q, want %q", evt.Type, subject)
	}
	if evt.Source != "ruby_gateway" {
		t.Errorf("CloudEvent.Source = %q, want ruby_gateway", evt.Source)
	}
}
