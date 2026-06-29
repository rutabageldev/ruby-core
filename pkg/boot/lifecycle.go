package boot

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/nats-io/nats.go"
)

// OnNATSClosed builds a nats.ClosedHandler that signals permanent NATS loss (#18). The
// nats.go connection auto-reconnects (see resilienceOpts); when reconnection is finally
// exhausted the connection closes and this fires. If the service has NOT begun a graceful
// shutdown (ctx still live), it records the loss and cancels ctx so the process exits and
// Docker restarts it from a clean bootstrap. During an intentional shutdown the service
// cancels ctx first and only then closes the connection (defer nc.Close runs last), so
// ctx.Err() != nil here and this is a no-op — distinguishing a crash from a clean stop
// without any extra detection flag. `lost` drives the process exit code in main().
func OnNATSClosed(ctx context.Context, cancel context.CancelFunc, lost *atomic.Bool, log *slog.Logger) func(*nats.Conn) {
	return func(_ *nats.Conn) {
		if ctx.Err() != nil {
			return
		}
		lost.Store(true)
		log.Error("nats: connection permanently lost after reconnect attempts; exiting for restart")
		cancel()
	}
}
