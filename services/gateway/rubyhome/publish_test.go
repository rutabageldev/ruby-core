//go:build fast

package rubyhome

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/primaryrutabaga/ruby-core/pkg/natsx"
	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

// TestEventRoutes verifies every ruby_home write event maps to its shared-contract
// subject and that no unexpected routes exist.
func TestEventRoutes(t *testing.T) {
	want := map[string]string{
		"calendar.event.upsert":               schemas.HomeEventCalendarUpsert,
		"calendar.event.delete":               schemas.HomeEventCalendarDelete,
		"ruby_home.childcare.provider.upsert": schemas.HomeEventChildcareProviderUpsert,
		"ruby_home.childcare.provider.delete": schemas.HomeEventChildcareProviderDelete,
		"ruby_home.directory.person.upsert":   schemas.HomeEventDirectoryPersonUpsert,
		"ruby_home.directory.person.delete":   schemas.HomeEventDirectoryPersonDelete,
	}
	if len(eventRoutes) != len(want) {
		t.Fatalf("eventRoutes has %d entries, want %d", len(eventRoutes), len(want))
	}
	for ev, subj := range want {
		got, ok := eventRoutes[ev]
		if !ok {
			t.Errorf("eventRoutes missing %q", ev)
			continue
		}
		if got != subj {
			t.Errorf("%q routes to %q, want %q", ev, got, subj)
		}
	}
}

// TestSubjectsValidPerADR0027 asserts every routed subject lands under the HA_EVENTS
// prefix and is composed of valid ADR-0027 tokens (lowercase, underscores; no dots
// within a token).
func TestSubjectsValidPerADR0027(t *testing.T) {
	for ev, subj := range eventRoutes {
		if !strings.HasPrefix(subj, "ha.events.") {
			t.Errorf("%q subject %q is not under the ha.events. prefix", ev, subj)
		}
		for _, tok := range strings.Split(subj, ".") {
			if !natsx.IsValidToken(tok) {
				t.Errorf("%q subject %q contains invalid token %q", ev, subj, tok)
			}
		}
	}
}

// TestPublishUnknownEvent verifies an unknown event returns an error before touching
// NATS (nil conn proves no publish is attempted).
func TestPublishUnknownEvent(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := Publish(context.Background(), nil, map[string]any{"event": "calendar.nope"}, log); err == nil {
		t.Fatal("expected error for unknown event, got nil")
	}
}

// TestPublishMissingEventField verifies a payload with no event field is rejected.
func TestPublishMissingEventField(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := Publish(context.Background(), nil, map[string]any{}, log); err == nil {
		t.Fatal("expected error for missing event field, got nil")
	}
}

// TestEnsureCalendarIdempotencyKey covers the #138 fix: a calendar create with no
// idempotency_key gets a deterministic content-derived one (stable across re-publishes);
// an HA-supplied key is preserved; non-upsert events are untouched.
func TestEnsureCalendarIdempotencyKey(t *testing.T) {
	upsert := func() map[string]any {
		return map[string]any{
			"event":   "calendar.event.upsert",
			"summary": "Dentist",
			"start":   map[string]any{"datetime": "2026-06-30T09:00:00-04:00", "timezone": "America/New_York"},
			"end":     map[string]any{"datetime": "2026-06-30T09:30:00-04:00", "timezone": "America/New_York"},
		}
	}

	// Absent → injected, deterministic across two equal payloads.
	a, b := upsert(), upsert()
	ensureCalendarIdempotencyKey("calendar.event.upsert", a)
	ensureCalendarIdempotencyKey("calendar.event.upsert", b)
	ka, _ := a["idempotency_key"].(string)
	kb, _ := b["idempotency_key"].(string)
	if ka == "" {
		t.Fatal("expected an injected idempotency_key")
	}
	if ka != kb {
		t.Errorf("same content produced different keys: %q vs %q", ka, kb)
	}

	// Different content → different key.
	c := upsert()
	c["summary"] = "Doctor"
	ensureCalendarIdempotencyKey("calendar.event.upsert", c)
	if c["idempotency_key"] == ka {
		t.Error("different content produced the same key")
	}

	// HA-supplied key is preserved.
	d := upsert()
	d["idempotency_key"] = "ha-supplied-123"
	ensureCalendarIdempotencyKey("calendar.event.upsert", d)
	if d["idempotency_key"] != "ha-supplied-123" {
		t.Errorf("HA-supplied key overwritten: %v", d["idempotency_key"])
	}

	// Non-upsert events are untouched.
	del := map[string]any{"event": "calendar.event.delete", "google_event_id": "g1"}
	ensureCalendarIdempotencyKey("calendar.event.delete", del)
	if _, ok := del["idempotency_key"]; ok {
		t.Error("delete event should not get an idempotency_key")
	}
}
