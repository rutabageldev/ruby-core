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
