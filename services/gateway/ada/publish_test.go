//go:build fast

package ada

import (
	"testing"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

// TestEventRoutesFeedingClaimed verifies the ada.feeding.claimed write-path event
// is routed to its subject (previously logged "unknown event type" and dropped) (#19).
func TestEventRoutesFeedingClaimed(t *testing.T) {
	got, ok := eventRoutes["ada.feeding.claimed"]
	if !ok {
		t.Fatal("eventRoutes missing ada.feeding.claimed")
	}
	if got != schemas.AdaEventFeedingClaimed {
		t.Errorf("ada.feeding.claimed routes to %q, want %q", got, schemas.AdaEventFeedingClaimed)
	}
}

// TestEventRoutesEditDelete verifies all ten edit/delete write-path events route to
// their subjects (#77/#78/#79).
func TestEventRoutesEditDelete(t *testing.T) {
	want := map[string]string{
		"ada.feeding.update": schemas.AdaEventFeedingUpdate,
		"ada.feeding.delete": schemas.AdaEventFeedingDelete,
		"ada.diaper.update":  schemas.AdaEventDiaperUpdate,
		"ada.diaper.delete":  schemas.AdaEventDiaperDelete,
		"ada.sleep.update":   schemas.AdaEventSleepUpdate,
		"ada.sleep.delete":   schemas.AdaEventSleepDelete,
		"ada.tummy.update":   schemas.AdaEventTummyUpdate,
		"ada.tummy.delete":   schemas.AdaEventTummyDelete,
		"ada.growth.update":  schemas.AdaEventGrowthUpdate,
		"ada.growth.delete":  schemas.AdaEventGrowthDelete,
	}
	for event, subject := range want {
		got, ok := eventRoutes[event]
		if !ok {
			t.Errorf("eventRoutes missing %q", event)
			continue
		}
		if got != subject {
			t.Errorf("%q routes to %q, want %q", event, got, subject)
		}
	}
}
