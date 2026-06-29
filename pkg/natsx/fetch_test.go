//go:build fast

package natsx

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
)

func TestClassifyFetchErr(t *testing.T) {
	cancelled := context.Canceled
	cases := []struct {
		name   string
		err    error
		ctxErr error
		want   FetchAction
	}{
		{"nil err, ctx live", nil, nil, FetchRetryNow},
		{"timeout, ctx live", nats.ErrTimeout, nil, FetchRetryNow},
		{"server shutdown, ctx live", errors.New("nats: Server Shutdown"), nil, FetchBackoff},
		{"connection closed, ctx live", nats.ErrConnectionClosed, nil, FetchBackoff},
		{"no responders, ctx live", nats.ErrNoResponders, nil, FetchBackoff},
		{"ctx cancelled wins over backoff", errors.New("boom"), cancelled, FetchStop},
		{"ctx cancelled wins over timeout", nats.ErrTimeout, cancelled, FetchStop},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyFetchErr(c.err, c.ctxErr); got != c.want {
				t.Errorf("ClassifyFetchErr(%v, %v) = %d, want %d", c.err, c.ctxErr, got, c.want)
			}
		})
	}
}
