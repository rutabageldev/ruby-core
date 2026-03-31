// Package ada implements the Ada baby tracking stateful processor (ADR-0029).
// It persists feeding, diaper, sleep, and tummy time events to PostgreSQL and
// pushes derived sensor state to Home Assistant after each event.
package ada

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nats-io/nats.go"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/engine/processor"
	adaha "github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/ha"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/store"
)

const (
	defaultFeedIntervalHours = 2.5

	// ada_config keys
	cfgKeyFeedIntervalHours = "feed_interval_hours"
	cfgKeyNextFeedingTarget = "next_feeding_target"

	// HA sensor entity IDs
	sensorLastFeedingTime    = "sensor.ada_last_feeding_time"
	sensorLastFeedingSource  = "sensor.ada_last_feeding_source"
	sensorNextFeedingTarget  = "sensor.ada_next_feeding_target"
	sensorTodayFeedingCount  = "sensor.ada_today_feeding_count"
	sensorTodayFeedingOz     = "sensor.ada_today_feeding_oz"
	sensorLastDiaperTime     = "sensor.ada_last_diaper_time"
	sensorLastDiaperType     = "sensor.ada_last_diaper_type"
	sensorTodayDiaperCount   = "sensor.ada_today_diaper_count"
	sensorTodayDiaperWet     = "sensor.ada_today_diaper_wet"
	sensorTodayDiaperDirty   = "sensor.ada_today_diaper_dirty"
	sensorSleepState         = "sensor.ada_sleep_state"
	sensorLastSleepChange    = "sensor.ada_last_sleep_change"
	sensorTodaySleepHours    = "sensor.ada_today_sleep_hours"
	sensorTodaySleepNapCount = "sensor.ada_today_sleep_nap_count"
	sensorTodayTummyMin      = "sensor.ada_today_tummy_time_min"
	sensorTodayTummySessions = "sensor.ada_today_tummy_time_sessions"
)

// Processor implements processor.StatefulProcessor for Ada baby tracking.
// It persists feeding, diaper, sleep, and tummy time events to Postgres
// and pushes derived sensor state to Home Assistant.
// There is no background polling goroutine — elapsed-time display is computed
// client-side by the dashboard from stored timestamps.
type Processor struct {
	q               *store.Queries
	ha              *adaha.Client
	lastHAConnected bool
	healthSub       *nats.Subscription
	log             *slog.Logger
}

// compile-time interface check
var _ processor.StatefulProcessor = (*Processor)(nil)

// New returns a Processor. Initialize must be called before use.
func New(log *slog.Logger) *Processor {
	return &Processor{log: log}
}

func (p *Processor) RequiresStorage() bool { return true }

func (p *Processor) Subscriptions() []string {
	return []string{
		"ha.events.ada.>",
		// React to feed interval changes made in the HA dashboard.
		"ha.events.input_number.ada_alert_threshold_h",
		// Note: gateway.health is a bare NATS publish (not on HA_EVENTS JetStream)
		// and is handled via a bare nc.Subscribe in Initialize, not listed here.
	}
}

// Initialize wires the processor: creates the query client, HA push client,
// bare gateway.health subscription, and restores sensor state from Postgres.
func (p *Processor) Initialize(cfg processor.Config) error {
	p.q = store.New(cfg.Pool)
	p.ha = adaha.NewClient(cfg.HA.URL, cfg.HA.Token, p.log)
	// Assume HA is connected at startup; gateway.health will correct if not.
	p.lastHAConnected = true

	// gateway.health is a bare NATS publish — not captured by HA_EVENTS JetStream.
	// Subscribe directly on the connection; torn down in Shutdown.
	var err error
	p.healthSub, err = cfg.NC.Subscribe("gateway.health", func(msg *nats.Msg) {
		p.handleHealthEvent(msg.Data)
	})
	if err != nil {
		return fmt.Errorf("ada: subscribe gateway.health: %w", err)
	}

	// Restore sensor state from Postgres so HA sensors are current immediately
	// after an engine restart. Errors are non-fatal.
	p.restoreSensors(context.Background())

	p.log.Info("ada: processor initialized")
	return nil
}

// Shutdown unsubscribes the bare gateway.health subscription.
// The pool and HA client are owned by the engine and must not be closed here.
func (p *Processor) Shutdown() {
	if p.healthSub != nil {
		_ = p.healthSub.Unsubscribe()
	}
	p.log.Info("ada: processor shut down")
}

// ProcessEvent routes an incoming event by CloudEvent type to the appropriate handler.
// DB errors are returned to trigger NAK+retry; HA push failures are non-fatal.
func (p *Processor) ProcessEvent(subject string, data []byte) error {
	var evt schemas.CloudEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return fmt.Errorf("ada: unmarshal CloudEvent: %w", err)
	}

	ctx := context.Background()
	switch evt.Type {
	case schemas.AdaEventFeedingEnded:
		return p.handleFeedingEnded(ctx, evt)
	case schemas.AdaEventFeedingLogged:
		return p.handleFeedingLogged(ctx, evt)
	case schemas.AdaEventFeedingSupplemented:
		return p.handleFeedingSupplemented(ctx, evt)
	case schemas.AdaEventDiaperLogged:
		return p.handleDiaperLogged(ctx, evt)
	case schemas.AdaEventSleepStarted:
		return p.handleSleepStarted(ctx, evt)
	case schemas.AdaEventSleepEnded:
		return p.handleSleepEnded(ctx, evt)
	case schemas.AdaEventSleepLogged:
		return p.handleSleepLogged(ctx, evt)
	case schemas.AdaEventTummyEnded:
		return p.handleTummyEnded(ctx, evt)
	case schemas.AdaEventTummyLogged:
		return p.handleTummyLogged(ctx, evt)
	case "ha.events.input_number.ada_alert_threshold_h":
		return p.handleThresholdChange(ctx, evt)
	default:
		p.log.Warn("ada: unknown event type", slog.String("subject", subject), slog.String("type", evt.Type))
		return nil // ACK unknown events silently
	}
}

// handleHealthEvent processes a gateway.health bare NATS message.
// Detects the false→true transition on ha_connected and restores sensors.
// Runs on the NATS client goroutine — must not block.
func (p *Processor) handleHealthEvent(data []byte) {
	var evt schemas.CloudEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		p.log.Warn("ada: unmarshal gateway.health", slog.String("error", err.Error()))
		return
	}
	connected, _ := evt.Data["ha_connected"].(bool)
	if connected && !p.lastHAConnected {
		p.log.Info("ada: HA reconnected — restoring sensors")
		go p.restoreSensors(context.Background())
	}
	p.lastHAConnected = connected
}

// ── Feeding handlers ─────────────────────────────────────────────────────────

func (p *Processor) handleFeedingEnded(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaFeedingEndedData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode feeding_ended: %w", err)
	}

	evtTime, err := parseRFC3339(evt.Time)
	if err != nil {
		return fmt.Errorf("ada: parse event time: %w", err)
	}

	sessionStart := evtTime.Add(-time.Duration(d.TotalDurationS) * time.Second)

	// Determine source from segments
	source := breastSource(d.Segments)

	feedingID, err := p.q.InsertFeeding(ctx, &store.InsertFeedingParams{
		Timestamp: toTimestamptz(sessionStart),
		Source:    source,
		LoggedBy:  d.LoggedBy,
	})
	if err != nil {
		return fmt.Errorf("ada: insert feeding_ended: %w", err)
	}

	// Insert segments with reconstructed absolute timestamps (forward from start)
	segStart := sessionStart
	for _, seg := range d.Segments {
		segEnd := segStart.Add(time.Duration(seg.DurationS) * time.Second)
		if err := p.q.InsertFeedingSegment(ctx, &store.InsertFeedingSegmentParams{
			FeedingID: feedingID,
			Side:      seg.Side,
			StartedAt: toTimestamptz(segStart),
			EndedAt:   toTimestamptz(segEnd),
			DurationS: int32(seg.DurationS), //nolint:gosec // G115: bounded by session duration in seconds, never near int32 max
		}); err != nil {
			return fmt.Errorf("ada: insert feeding segment: %w", err)
		}
		segStart = segEnd
	}

	p.pushFeedingSensors(ctx, sessionStart, source)
	return nil
}

func (p *Processor) handleFeedingLogged(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaFeedingLoggedData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode feeding_logged: %w", err)
	}

	startTime, err := parseRFC3339(d.StartTime)
	if err != nil {
		return fmt.Errorf("ada: parse feeding start_time: %w", err)
	}

	feedingID, err := p.q.InsertFeeding(ctx, &store.InsertFeedingParams{
		Timestamp: toTimestamptz(startTime),
		Source:    d.Source,
		LoggedBy:  d.LoggedBy,
	})
	if err != nil {
		return fmt.Errorf("ada: insert feeding_logged: %w", err)
	}

	if isBottleSource(d.Source) {
		if err := p.q.InsertFeedingBottleDetail(ctx, &store.InsertFeedingBottleDetailParams{
			FeedingID:    feedingID,
			AmountOz:     numericFromFloat(d.AmountOz),
			BreastMilkOz: numericFromFloat(d.BreastMilkOz),
			FormulaOz:    numericFromFloat(d.FormulaOz),
		}); err != nil {
			return fmt.Errorf("ada: insert feeding bottle detail: %w", err)
		}
	}

	p.pushFeedingSensors(ctx, startTime, d.Source)
	return nil
}

func (p *Processor) handleFeedingSupplemented(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaFeedingSupplementData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode feeding_supplement: %w", err)
	}

	// Supplements have no explicit timestamp — use the CloudEvent time (real-time action).
	evtTime, err := parseRFC3339(evt.Time)
	if err != nil {
		return fmt.Errorf("ada: parse event time: %w", err)
	}

	feedingID, err := p.q.InsertFeeding(ctx, &store.InsertFeedingParams{
		Timestamp: toTimestamptz(evtTime),
		Source:    d.Source,
		LoggedBy:  d.LoggedBy,
	})
	if err != nil {
		return fmt.Errorf("ada: insert feeding_supplement: %w", err)
	}

	if err := p.q.InsertFeedingBottleDetail(ctx, &store.InsertFeedingBottleDetailParams{
		FeedingID: feedingID,
		AmountOz:  numericFromFloat(d.AmountOz),
	}); err != nil {
		return fmt.Errorf("ada: insert supplement bottle detail: %w", err)
	}

	p.pushFeedingSensors(ctx, evtTime, d.Source)
	return nil
}

// ── Diaper handler ───────────────────────────────────────────────────────────

func (p *Processor) handleDiaperLogged(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaDiaperLoggedData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode diaper_logged: %w", err)
	}

	ts, err := parseRFC3339(d.Timestamp)
	if err != nil {
		return fmt.Errorf("ada: parse diaper timestamp: %w", err)
	}

	if err := p.q.InsertDiaper(ctx, &store.InsertDiaperParams{
		Timestamp: toTimestamptz(ts),
		Type:      d.Type,
		LoggedBy:  d.LoggedBy,
	}); err != nil {
		return fmt.Errorf("ada: insert diaper: %w", err)
	}

	p.pushDiaperSensors(ctx, ts, d.Type)
	return nil
}

// ── Sleep handlers ───────────────────────────────────────────────────────────

func (p *Processor) handleSleepStarted(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaSleepStartedData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode sleep_started: %w", err)
	}

	startTime, err := parseRFC3339(d.StartTime)
	if err != nil {
		return fmt.Errorf("ada: parse sleep start_time: %w", err)
	}

	if _, err := p.q.InsertSleepStart(ctx, &store.InsertSleepStartParams{
		StartTime: toTimestamptz(startTime),
		SleepType: d.SleepType,
		LoggedBy:  d.LoggedBy,
	}); err != nil {
		return fmt.Errorf("ada: insert sleep start: %w", err)
	}

	p.pushSleepStartedSensors(ctx, startTime)
	return nil
}

func (p *Processor) handleSleepEnded(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaSleepEndedData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode sleep_ended: %w", err)
	}

	endTime, err := parseRFC3339(d.EndTime)
	if err != nil {
		return fmt.Errorf("ada: parse sleep end_time: %w", err)
	}

	if err := p.q.UpdateSleepEnd(ctx, toTimestamptz(endTime)); err != nil {
		return fmt.Errorf("ada: update sleep end: %w", err)
	}

	p.pushSleepEndedSensors(ctx, endTime)
	return nil
}

func (p *Processor) handleSleepLogged(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaSleepLoggedData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode sleep_logged: %w", err)
	}

	startTime, err := parseRFC3339(d.StartTime)
	if err != nil {
		return fmt.Errorf("ada: parse sleep start_time: %w", err)
	}
	endTime, err := parseRFC3339(d.EndTime)
	if err != nil {
		return fmt.Errorf("ada: parse sleep end_time: %w", err)
	}

	if err := p.q.InsertSleepSession(ctx, &store.InsertSleepSessionParams{
		StartTime: toTimestamptz(startTime),
		EndTime:   toTimestamptz(endTime),
		SleepType: d.SleepType,
		LoggedBy:  d.LoggedBy,
	}); err != nil {
		return fmt.Errorf("ada: insert sleep session: %w", err)
	}

	p.pushSleepEndedSensors(ctx, endTime)
	return nil
}

// ── Tummy time handlers ───────────────────────────────────────────────────────

func (p *Processor) handleTummyEnded(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaTummyEndedData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode tummy_ended: %w", err)
	}
	return p.insertTummyAndPush(ctx, d.StartTime, d.EndTime, d.DurationS, d.LoggedBy)
}

func (p *Processor) handleTummyLogged(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaTummyLoggedData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode tummy_logged: %w", err)
	}
	return p.insertTummyAndPush(ctx, d.StartTime, d.EndTime, d.DurationS, d.LoggedBy)
}

func (p *Processor) insertTummyAndPush(ctx context.Context, startStr, endStr string, durationS int, loggedBy string) error {
	startTime, err := parseRFC3339(startStr)
	if err != nil {
		return fmt.Errorf("ada: parse tummy start_time: %w", err)
	}
	endTime, err := parseRFC3339(endStr)
	if err != nil {
		return fmt.Errorf("ada: parse tummy end_time: %w", err)
	}

	if err := p.q.InsertTummySession(ctx, &store.InsertTummySessionParams{
		StartTime: toTimestamptz(startTime),
		EndTime:   toTimestamptz(endTime),
		DurationS: int32(durationS), //nolint:gosec // G115: bounded by session duration in seconds, never near int32 max
		LoggedBy:  loggedBy,
	}); err != nil {
		return fmt.Errorf("ada: insert tummy session: %w", err)
	}

	p.pushTummySensors(ctx)
	return nil
}

// ── Threshold change handler ─────────────────────────────────────────────────

func (p *Processor) handleThresholdChange(ctx context.Context, evt schemas.CloudEvent) error {
	// HA input_number state_changed event; state is the new numeric value as a string.
	stateVal, _ := evt.Data["state"].(string)
	if stateVal == "" {
		p.log.Warn("ada: threshold change missing state value")
		return nil
	}
	intervalH, err := strconv.ParseFloat(stateVal, 64)
	if err != nil || intervalH <= 0 {
		p.log.Warn("ada: invalid threshold value", slog.String("state", stateVal))
		return nil
	}

	if err := p.q.UpsertConfig(ctx, &store.UpsertConfigParams{
		Key:   cfgKeyFeedIntervalHours,
		Value: stateVal,
	}); err != nil {
		return fmt.Errorf("ada: upsert feed_interval_hours: %w", err)
	}

	// Recompute next_feeding_target using the new interval and the last feeding time.
	last, err := p.q.GetLastFeeding(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // no feedings yet; nothing to recompute
		}
		return fmt.Errorf("ada: get last feeding for threshold update: %w", err)
	}

	lastFeedingTime := last.Timestamp.Time
	nextTarget := lastFeedingTime.Add(time.Duration(intervalH * float64(time.Hour)))
	nextTargetStr := nextTarget.UTC().Format(time.RFC3339)

	if err := p.q.UpsertConfig(ctx, &store.UpsertConfigParams{
		Key:   cfgKeyNextFeedingTarget,
		Value: nextTargetStr,
	}); err != nil {
		return fmt.Errorf("ada: upsert next_feeding_target: %w", err)
	}

	if haErr := p.ha.PushState(ctx, sensorNextFeedingTarget, nextTargetStr, nil); haErr != nil {
		p.log.Warn("ada: push next_feeding_target after threshold change", slog.String("error", haErr.Error()))
	}
	return nil
}

// ── Sensor push helpers ───────────────────────────────────────────────────────

// pushFeedingSensors pushes all feeding-related sensors after a feeding event.
// lastFeedingTime is the actual event time (never time.Now()).
func (p *Processor) pushFeedingSensors(ctx context.Context, lastFeedingTime time.Time, source string) {
	lastTimeStr := lastFeedingTime.UTC().Format(time.RFC3339)

	// Compute next feeding target using stored interval (or default).
	intervalH := p.getFeedIntervalHours(ctx)
	nextTarget := lastFeedingTime.Add(time.Duration(intervalH * float64(time.Hour)))
	nextTargetStr := nextTarget.UTC().Format(time.RFC3339)

	// Persist target for restoreSensors and threshold change handler.
	if err := p.q.UpsertConfig(ctx, &store.UpsertConfigParams{
		Key:   cfgKeyNextFeedingTarget,
		Value: nextTargetStr,
	}); err != nil {
		p.log.Warn("ada: upsert next_feeding_target", slog.String("error", err.Error()))
	}

	agg, err := p.q.GetTodayFeedingAggregates(ctx)
	if err != nil {
		p.log.Warn("ada: get today feeding aggregates", slog.String("error", err.Error()))
	}

	pushes := []struct{ id, state string }{
		{sensorLastFeedingTime, lastTimeStr},
		{sensorLastFeedingSource, source},
		{sensorNextFeedingTarget, nextTargetStr},
	}
	if agg != nil {
		pushes = append(pushes,
			struct{ id, state string }{sensorTodayFeedingCount, strconv.Itoa(int(agg.Count))},
			struct{ id, state string }{sensorTodayFeedingOz, strconv.FormatFloat(agg.TotalOz, 'f', 2, 64)},
		)
	}
	p.pushAll(ctx, pushes)
}

// pushDiaperSensors pushes all diaper-related sensors after a diaper event.
// lastDiaperTime is the actual event time (never time.Now()).
func (p *Processor) pushDiaperSensors(ctx context.Context, lastDiaperTime time.Time, diaperType string) {
	lastTimeStr := lastDiaperTime.UTC().Format(time.RFC3339)

	agg, err := p.q.GetTodayDiaperAggregates(ctx)
	if err != nil {
		p.log.Warn("ada: get today diaper aggregates", slog.String("error", err.Error()))
	}

	pushes := []struct{ id, state string }{
		{sensorLastDiaperTime, lastTimeStr},
		{sensorLastDiaperType, diaperType},
	}
	if agg != nil {
		pushes = append(pushes,
			struct{ id, state string }{sensorTodayDiaperCount, strconv.Itoa(int(agg.Total))},
			struct{ id, state string }{sensorTodayDiaperWet, strconv.Itoa(int(agg.Wet))},
			struct{ id, state string }{sensorTodayDiaperDirty, strconv.Itoa(int(agg.Dirty))},
		)
	}
	p.pushAll(ctx, pushes)
}

// pushSleepStartedSensors pushes sensors after a sleep session starts.
func (p *Processor) pushSleepStartedSensors(ctx context.Context, startTime time.Time) {
	p.pushAll(ctx, []struct{ id, state string }{
		{sensorSleepState, "sleeping"},
		{sensorLastSleepChange, startTime.UTC().Format(time.RFC3339)},
	})
}

// pushSleepEndedSensors pushes sensors after a sleep session ends.
func (p *Processor) pushSleepEndedSensors(ctx context.Context, endTime time.Time) {
	agg, err := p.q.GetTodaySleepAggregates(ctx)
	if err != nil {
		p.log.Warn("ada: get today sleep aggregates", slog.String("error", err.Error()))
	}

	pushes := []struct{ id, state string }{
		{sensorSleepState, "awake"},
		{sensorLastSleepChange, endTime.UTC().Format(time.RFC3339)},
	}
	if agg != nil {
		pushes = append(pushes,
			struct{ id, state string }{sensorTodaySleepHours, strconv.FormatFloat(agg.TotalHours, 'f', 2, 64)},
			struct{ id, state string }{sensorTodaySleepNapCount, strconv.Itoa(int(agg.NapCount))},
		)
	}
	p.pushAll(ctx, pushes)
}

// pushTummySensors pushes tummy time aggregate sensors.
func (p *Processor) pushTummySensors(ctx context.Context) {
	agg, err := p.q.GetTodayTummyAggregates(ctx)
	if err != nil {
		p.log.Warn("ada: get today tummy aggregates", slog.String("error", err.Error()))
		return
	}
	p.pushAll(ctx, []struct{ id, state string }{
		{sensorTodayTummyMin, strconv.Itoa(int(agg.TotalMinutes))},
		{sensorTodayTummySessions, strconv.Itoa(int(agg.Sessions))},
	})
}

// pushAll pushes a batch of entity_id/state pairs, logging Warn on any error.
func (p *Processor) pushAll(ctx context.Context, pushes []struct{ id, state string }) {
	for _, push := range pushes {
		if err := p.ha.PushState(ctx, push.id, push.state, nil); err != nil {
			p.log.Warn("ada: push sensor", slog.String("entity_id", push.id), slog.String("error", err.Error()))
		}
	}
}

// ── restoreSensors ────────────────────────────────────────────────────────────

// restoreSensors reads last-known state from Postgres and pushes to HA.
// Called once in Initialize and from handleHealthEvent on HA reconnect.
// pgx.ErrNoRows is normal — log at Debug, not Warn.
// All other errors are logged at Warn; sensor pushes are best-effort.
func (p *Processor) restoreSensors(ctx context.Context) {
	// 1. Last feeding
	if f, err := p.q.GetLastFeeding(ctx); err == nil {
		lastTime := f.Timestamp.Time.UTC().Format(time.RFC3339)
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorLastFeedingTime, lastTime},
			{sensorLastFeedingSource, f.Source},
		})
	} else if !errors.Is(err, pgx.ErrNoRows) {
		p.log.Warn("ada: restore last feeding", slog.String("error", err.Error()))
	}

	// 2. Last diaper
	if d, err := p.q.GetLastDiaper(ctx); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorLastDiaperTime, d.Timestamp.Time.UTC().Format(time.RFC3339)},
			{sensorLastDiaperType, d.Type},
		})
	} else if !errors.Is(err, pgx.ErrNoRows) {
		p.log.Warn("ada: restore last diaper", slog.String("error", err.Error()))
	}

	// 3. Sleep state
	if active, err := p.q.GetActiveSleepSession(ctx); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorSleepState, "sleeping"},
			{sensorLastSleepChange, active.Time.UTC().Format(time.RFC3339)},
		})
	} else if errors.Is(err, pgx.ErrNoRows) {
		// No active session — push awake + last sleep end if available
		p.pushAll(ctx, []struct{ id, state string }{{sensorSleepState, "awake"}})
		if lastEnd, err := p.q.GetLastSleepEnd(ctx); err == nil {
			p.pushAll(ctx, []struct{ id, state string }{
				{sensorLastSleepChange, lastEnd.Time.UTC().Format(time.RFC3339)},
			})
		} else if !errors.Is(err, pgx.ErrNoRows) {
			p.log.Warn("ada: restore last sleep end", slog.String("error", err.Error()))
		}
	} else {
		p.log.Warn("ada: restore active sleep session", slog.String("error", err.Error()))
	}

	// 4. Next feeding target
	if cfg, err := p.q.GetConfig(ctx, cfgKeyNextFeedingTarget); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{{sensorNextFeedingTarget, cfg.Value}})
	} else if !errors.Is(err, pgx.ErrNoRows) {
		p.log.Warn("ada: restore next_feeding_target", slog.String("error", err.Error()))
	}

	// 5–8. Today aggregates
	if agg, err := p.q.GetTodayFeedingAggregates(ctx); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorTodayFeedingCount, strconv.Itoa(int(agg.Count))},
			{sensorTodayFeedingOz, strconv.FormatFloat(agg.TotalOz, 'f', 2, 64)},
		})
	} else {
		p.log.Warn("ada: restore today feeding aggregates", slog.String("error", err.Error()))
	}

	if agg, err := p.q.GetTodayDiaperAggregates(ctx); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorTodayDiaperCount, strconv.Itoa(int(agg.Total))},
			{sensorTodayDiaperWet, strconv.Itoa(int(agg.Wet))},
			{sensorTodayDiaperDirty, strconv.Itoa(int(agg.Dirty))},
		})
	} else {
		p.log.Warn("ada: restore today diaper aggregates", slog.String("error", err.Error()))
	}

	if agg, err := p.q.GetTodaySleepAggregates(ctx); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorTodaySleepHours, strconv.FormatFloat(agg.TotalHours, 'f', 2, 64)},
			{sensorTodaySleepNapCount, strconv.Itoa(int(agg.NapCount))},
		})
	} else {
		p.log.Warn("ada: restore today sleep aggregates", slog.String("error", err.Error()))
	}

	if agg, err := p.q.GetTodayTummyAggregates(ctx); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorTodayTummyMin, strconv.Itoa(int(agg.TotalMinutes))},
			{sensorTodayTummySessions, strconv.Itoa(int(agg.Sessions))},
		})
	} else {
		p.log.Warn("ada: restore today tummy aggregates", slog.String("error", err.Error()))
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// getFeedIntervalHours reads feed_interval_hours from ada_config.
// Returns defaultFeedIntervalHours if not set.
func (p *Processor) getFeedIntervalHours(ctx context.Context) float64 {
	cfg, err := p.q.GetConfig(ctx, cfgKeyFeedIntervalHours)
	if err != nil {
		return defaultFeedIntervalHours
	}
	h, err := strconv.ParseFloat(cfg.Value, 64)
	if err != nil || h <= 0 {
		return defaultFeedIntervalHours
	}
	return h
}

// remarshal round-trips an event data map through JSON to populate a typed struct.
func remarshal(data map[string]any, dst any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// parseRFC3339 parses an RFC3339 timestamp string.
func parseRFC3339(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse RFC3339 %q: %w", s, err)
	}
	return t, nil
}

// toTimestamptz converts a time.Time to pgtype.Timestamptz.
func toTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// numericFromFloat converts a float64 to pgtype.Numeric.
// Zero values produce a valid zero numeric (not NULL).
func numericFromFloat(f float64) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(strconv.FormatFloat(f, 'f', 2, 64))
	return n
}

// breastSource derives a feeding source string from breast feeding segments.
func breastSource(segments []schemas.AdaFeedingSegment) string {
	sides := make(map[string]bool)
	for _, s := range segments {
		sides[s.Side] = true
	}
	if len(sides) == 1 {
		for side := range sides {
			return "breast_" + side
		}
	}
	return "breast"
}

// isBottleSource reports whether a feeding source string indicates a bottle feeding.
func isBottleSource(source string) bool {
	switch source {
	case "bottle_breast", "bottle_formula", "mixed":
		return true
	}
	return false
}
