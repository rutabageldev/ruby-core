//go:build fast

package rubyhome

import (
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
	if err := Publish(nil, map[string]any{"event": "calendar.nope"}, log); err == nil {
		t.Fatal("expected error for unknown event, got nil")
	}
}

// TestPublishMissingEventField verifies a payload with no event field is rejected.
func TestPublishMissingEventField(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := Publish(nil, map[string]any{}, log); err == nil {
		t.Fatal("expected error for missing event field, got nil")
	}
}
