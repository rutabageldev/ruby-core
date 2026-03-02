package ha_test

import (
	"testing"
	"time"

	"github.com/primaryrutabaga/ruby-core/services/gateway/ha"
)

func TestParseUTC_RFC3339(t *testing.T) {
	t.Helper()
	ts, err := ha.ParseUTC("2026-03-01T10:30:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts.Location() != time.UTC {
		t.Errorf("location = %v, want UTC", ts.Location())
	}
	if ts.Hour() != 10 || ts.Minute() != 30 {
		t.Errorf("time = %v, want 10:30:00 UTC", ts)
	}
}

func TestParseUTC_RFC3339Nano(t *testing.T) {
	ts, err := ha.ParseUTC("2026-03-01T10:30:00.123456789Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts.Nanosecond() != 123456789 {
		t.Errorf("nanosecond = %d, want 123456789", ts.Nanosecond())
	}
}

func TestParseUTC_NormalisesOffset(t *testing.T) {
	// A "+02:00" offset should normalise to UTC, not be treated as UTC+2.
	ts, err := ha.ParseUTC("2026-03-01T12:30:00+02:00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts.Hour() != 10 {
		t.Errorf("UTC hour = %d, want 10 (12:30 +02:00 = 10:30 UTC)", ts.Hour())
	}
}

func TestParseUTC_InvalidFormat(t *testing.T) {
	_, err := ha.ParseUTC("not-a-time")
	if err == nil {
		t.Fatal("expected error for invalid time, got nil")
	}
}

func TestSplitEntityID_Valid(t *testing.T) {
	cases := []struct {
		input, wantDomain, wantName string
	}{
		{"person.wife", "person", "wife"},
		{"device_tracker.wife_phone", "device_tracker", "wife_phone"},
		{"light.living_room", "light", "living_room"},
	}
	for _, tc := range cases {
		d, n, err := ha.SplitEntityID(tc.input)
		if err != nil {
			t.Errorf("SplitEntityID(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if d != tc.wantDomain || n != tc.wantName {
			t.Errorf("SplitEntityID(%q) = (%q, %q), want (%q, %q)",
				tc.input, d, n, tc.wantDomain, tc.wantName)
		}
	}
}

func TestSplitEntityID_Invalid(t *testing.T) {
	_, _, err := ha.SplitEntityID("nodot")
	if err == nil {
		t.Fatal("expected error for entity ID without dot, got nil")
	}
}
