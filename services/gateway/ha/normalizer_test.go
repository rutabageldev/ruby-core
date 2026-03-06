//go:build fast

package ha_test

import (
	"testing"

	"github.com/primaryrutabaga/ruby-core/services/gateway/ha"
)

func TestNormalizer_PassesAllWithEmptyPasslist(t *testing.T) {
	n := ha.NewNormalizer(nil)
	attrs := map[string]any{"state": "home", "battery": 82, "gps": "secret"}
	got := n.Apply("person", attrs)
	if len(got) != 3 {
		t.Errorf("expected all 3 attrs, got %d: %v", len(got), got)
	}
}

func TestNormalizer_FiltersUnallowedAttributes(t *testing.T) {
	n := ha.NewNormalizer(map[string][]string{
		"person": {"state"},
	})
	attrs := map[string]any{
		"state":   "home",
		"battery": 82,
		"gps_lat": 51.5,
	}
	got := n.Apply("person", attrs)
	if len(got) != 1 {
		t.Errorf("expected 1 attr (state), got %d: %v", len(got), got)
	}
	if got["state"] != "home" {
		t.Errorf("state = %v, want %q", got["state"], "home")
	}
}

func TestNormalizer_StateAlwaysIncluded(t *testing.T) {
	// The passlist allows only "battery_level" but "state" must always be kept.
	n := ha.NewNormalizer(map[string][]string{
		"device_tracker": {"battery_level"},
	})
	attrs := map[string]any{
		"state":         "not_home",
		"battery_level": 55,
		"source_type":   "gps",
	}
	got := n.Apply("device_tracker", attrs)
	if _, ok := got["state"]; !ok {
		t.Error("state should always be included")
	}
	if _, ok := got["battery_level"]; !ok {
		t.Error("battery_level should be included (in passlist)")
	}
	if _, ok := got["source_type"]; ok {
		t.Error("source_type should be filtered out")
	}
}

func TestNormalizer_UnknownDomainPassesAll(t *testing.T) {
	n := ha.NewNormalizer(map[string][]string{
		"person": {"state"},
	})
	// "sensor" has no passlist entry → all attributes pass through.
	attrs := map[string]any{"state": "on", "power": 150.0}
	got := n.Apply("sensor", attrs)
	if len(got) != 2 {
		t.Errorf("unknown domain: expected 2 attrs, got %d: %v", len(got), got)
	}
}
