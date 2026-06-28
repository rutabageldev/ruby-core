// Package calendar implements the engine-side calendar processor (ROADMAP-0012,
// ADR-0042): the single ingress for calendar writes (write-through to Google +
// the local mirror) and the owner of the incremental sync poller. Google is the
// system of record; ruby-core holds the durable mirror and the local overlay.
//
// All Google access is gated behind CALENDAR_SYNC_ENABLED. Only the environment
// that owns the shared calendar (prod) enables it; elsewhere the processor runs
// without a Google connection and ignores write events (ADR-0033 analog).
package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/primaryrutabaga/ruby-core/pkg/boot"
	"github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
	"github.com/primaryrutabaga/ruby-core/pkg/idempotency"
	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/engine/processor"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/calendar/gcal"

	"github.com/nats-io/nats.go"
)

const (
	idempotencyBucket = "calendar_idempotency"
	idempotencyTTL    = 24 * time.Hour
	pollInterval      = 60 * time.Second
)

// calStore is the subset of store.Queries the processor uses. Abstracting it lets
// write-through, the poller, and reminders be unit-tested with a fake (no DB
// container). It is a superset of calendar.RangeReader, so it can drive ExpandRange.
type calStore interface {
	UpsertEvent(ctx context.Context, arg *store.UpsertEventParams) error
	GetEvent(ctx context.Context, googleEventID string) (*store.CalendarEvent, error)
	DeleteEvent(ctx context.Context, googleEventID string) error
	GetSyncState(ctx context.Context, calendarID string) (*store.SyncState, error)
	UpsertSyncToken(ctx context.Context, arg *store.UpsertSyncTokenParams) error
	MarkFullResync(ctx context.Context, calendarID string) error

	ListSingleEventsInRange(ctx context.Context, arg *store.ListSingleEventsInRangeParams) ([]*store.CalendarEvent, error)
	ListRecurringMasters(ctx context.Context) ([]*store.CalendarEvent, error)
	ListOverrides(ctx context.Context) ([]*store.CalendarEvent, error)

	// Overlay writes (Slice D).
	UpsertProvider(ctx context.Context, arg *store.UpsertProviderParams) error
	ArchiveProvider(ctx context.Context, id pgtype.UUID) error
	DeleteEventSubjects(ctx context.Context, googleEventID string) error
	InsertEventSubject(ctx context.Context, arg *store.InsertEventSubjectParams) error
	DeleteEventChildcare(ctx context.Context, googleEventID string) error
	InsertEventChildcare(ctx context.Context, arg *store.InsertEventChildcareParams) error
}

// Processor is the calendar StatefulProcessor.
type Processor struct {
	log *slog.Logger

	pool *pgxpool.Pool
	q    calStore
	nc   *nats.Conn

	gcal       gcal.Service // nil when sync is disabled
	calendarID string

	ha           *haPusher
	reminderLead time.Duration

	idStore     idempotency.Store
	syncEnabled bool

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New constructs the calendar processor.
func New(log *slog.Logger) *Processor { return &Processor{log: log} }

// RequiresStorage signals the engine to boot Postgres and run migrations.
func (p *Processor) RequiresStorage() bool { return true }

// Subscriptions are the calendar + overlay write subjects routed in via the gateway
// (Slices B/D).
func (p *Processor) Subscriptions() []string {
	return []string{
		schemas.HomeEventCalendarUpsert,
		schemas.HomeEventCalendarDelete,
		schemas.HomeEventChildcareProviderUpsert,
		schemas.HomeEventChildcareProviderDelete,
	}
}

// Initialize wires storage + NATS, and — when sync is enabled — connects Google
// and starts the sync poller. Migrations are owned by the engine (see main.go).
func (p *Processor) Initialize(cfg processor.Config) error {
	p.pool = cfg.Pool
	p.q = store.New(cfg.Pool)
	p.nc = cfg.NC
	p.syncEnabled = os.Getenv("CALENDAR_SYNC_ENABLED") == "true"

	kv, err := idempotency.CreateOrBindKVBucket(cfg.JS, idempotencyBucket, idempotencyTTL)
	if err != nil {
		return fmt.Errorf("calendar: idempotency kv: %w", err)
	}
	p.idStore = idempotency.NewHybridStore(kv, idempotencyTTL)

	if !p.syncEnabled {
		p.log.Warn("calendar: sync disabled (CALENDAR_SYNC_ENABLED != true) — no Google connection; write events ignored")
		return nil
	}

	gcfg, err := boot.FetchGoogleConfig(os.Getenv("VAULT_ADDR"), os.Getenv("VAULT_TOKEN"), os.Getenv("VAULT_GOOGLE_PATH"))
	if err != nil {
		return fmt.Errorf("calendar: fetch google config: %w", err)
	}
	p.calendarID = gcfg.CalendarID

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	svc, err := gcal.NewService(ctx, gcfg)
	if err != nil {
		cancel()
		return fmt.Errorf("calendar: google client: %w", err)
	}
	p.gcal = svc

	p.reminderLead = reminderLeadFromEnv()
	if cfg.HA != nil {
		p.ha = newHAPusher(cfg.HA.URL, cfg.HA.Token)
	} else {
		p.ha = newHAPusher("", "") // HA disabled → sensor pushes are no-ops
	}

	p.wg.Go(func() { p.runPoller(ctx) })
	p.wg.Go(func() { p.runReminders(ctx) })

	p.log.Info("calendar: sync enabled",
		slog.String("calendar_id", p.calendarID),
		slog.Duration("reminder_lead", p.reminderLead),
	)
	return nil
}

// reminderLeadFromEnv reads CALENDAR_REMINDER_LEAD (a Go duration like "10m"),
// defaulting to 10 minutes.
func reminderLeadFromEnv() time.Duration {
	if v := os.Getenv("CALENDAR_REMINDER_LEAD"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 10 * time.Minute
}

// Shutdown cancels the poller and releases resources.
func (p *Processor) Shutdown() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	if p.idStore != nil {
		_ = p.idStore.Close()
	}
}

// ProcessEvent routes a calendar write event to write-through.
func (p *Processor) ProcessEvent(subject string, data []byte) error {
	if !p.syncEnabled {
		// Non-prod: do not touch the shared calendar. Ack and ignore.
		return nil
	}

	var evt schemas.CloudEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		p.log.Warn("calendar: malformed cloudevent", slog.String("error", err.Error()))
		return nil // ack malformed
	}

	ctx := context.Background()
	switch subject {
	case schemas.HomeEventCalendarUpsert:
		return p.handleUpsert(ctx, &evt)
	case schemas.HomeEventCalendarDelete:
		return p.handleDelete(ctx, &evt)
	case schemas.HomeEventChildcareProviderUpsert:
		return p.handleProviderUpsert(ctx, &evt)
	case schemas.HomeEventChildcareProviderDelete:
		return p.handleProviderDelete(ctx, &evt)
	default:
		return nil
	}
}

// decodeData re-marshals the CloudEvent data map into a typed payload.
func decodeData(data map[string]any, out any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
