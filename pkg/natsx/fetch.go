package natsx

import (
	"errors"
	"time"

	"github.com/nats-io/nats.go"
)

// FetchRetryBackoff is how long a pull-consumer loop waits after a transient fetch
// error before retrying. A disconnected Fetch returns immediately, so this sleep is
// what stops a hot-spin while nats.go reconnects (#18).
const FetchRetryBackoff = time.Second

// FetchAction is what a pull-consumer loop should do after a Fetch error.
type FetchAction int

const (
	// FetchRetryNow: fetch again immediately (a normal pull timeout — no messages).
	FetchRetryNow FetchAction = iota
	// FetchBackoff: a transient connection error (disconnected / reconnecting). Log,
	// wait FetchRetryBackoff, fetch again. The loop must NOT exit — once nats.go
	// reconnects, the durable consumer resumes (#18).
	FetchBackoff
	// FetchStop: the context was cancelled (graceful shutdown, or a ClosedHandler
	// signalling permanent NATS loss). Exit the loop.
	FetchStop
)

// ClassifyFetchErr decides how a pull-consumer loop should react to a Fetch error,
// given the loop context's error. Context cancellation always wins (FetchStop); a plain
// timeout is normal (FetchRetryNow); every other error — including "nats: Server
// Shutdown", ErrConnectionClosed, ErrNoResponders — is transient (FetchBackoff) so a
// NATS bounce does not kill the consumer.
func ClassifyFetchErr(err error, ctxErr error) FetchAction {
	if ctxErr != nil {
		return FetchStop
	}
	if err == nil || errors.Is(err, nats.ErrTimeout) {
		return FetchRetryNow
	}
	return FetchBackoff
}
