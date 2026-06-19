//go:build fast

package natsx

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestStreamLimitsDrifted(t *testing.T) {
	base := &nats.StreamConfig{MaxAge: 48 * time.Hour, MaxBytes: 512 << 20, MaxMsgs: -1}

	cases := []struct {
		name     string
		existing *nats.StreamConfig
		want     bool
	}{
		{"identical", &nats.StreamConfig{MaxAge: 48 * time.Hour, MaxBytes: 512 << 20, MaxMsgs: -1}, false},
		{"max_age differs (was unbounded)", &nats.StreamConfig{MaxAge: 0, MaxBytes: 512 << 20, MaxMsgs: -1}, true},
		{"max_bytes differs (was unbounded)", &nats.StreamConfig{MaxAge: 48 * time.Hour, MaxBytes: -1, MaxMsgs: -1}, true},
		{"max_msgs differs", &nats.StreamConfig{MaxAge: 48 * time.Hour, MaxBytes: 512 << 20, MaxMsgs: 1000}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := streamLimitsDrifted(c.existing, base); got != c.want {
				t.Errorf("streamLimitsDrifted = %v, want %v", got, c.want)
			}
		})
	}
}
