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

	"github.com/google/uuid"
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
	sensorTodayDiaperMixed   = "sensor.ada_today_diaper_mixed"
	sensorSleepState         = "sensor.ada_sleep_state"
	sensorLastSleepChange    = "sensor.ada_last_sleep_change"
	sensorTodaySleepHours    = "sensor.ada_today_sleep_hours"
	sensorTodaySleepNapCount = "sensor.ada_today_sleep_nap_count"
	sensorTodayTummyMin      = "sensor.ada_today_tummy_time_min"
	sensorTodayTummySessions = "sensor.ada_today_tummy_time_sessions"
	sensorSleepSessionMin    = "sensor.ada_sleep_session_min"
	sensorDiaperHistory      = "sensor.ada_diaper_history"
	sensorSleepHistory       = "sensor.ada_sleep_history"
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
	stopCh          chan struct{}
	lastRefreshDate time.Time // date of last daily-aggregate push (for midnight rollover)
	lastFullRefresh time.Time // time of last full sensor restore (for 4h safety net)
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
	p.refreshAllSensors(context.Background())

	// Seed ticker state so the first tick doesn't trigger immediate rollover/restore.
	p.lastRefreshDate = startOfDay(time.Now().Local())
	p.lastFullRefresh = time.Now()
	p.stopCh = make(chan struct{})
	go p.runTicker()

	p.log.Info("ada: processor initialized")
	return nil
}

// Shutdown unsubscribes the bare gateway.health subscription and stops the
// background ticker goroutine. The pool and HA client are owned by the engine
// and must not be closed here.
func (p *Processor) Shutdown() {
	if p.healthSub != nil {
		_ = p.healthSub.Unsubscribe()
	}
	if p.stopCh != nil {
		close(p.stopCh)
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
	case schemas.AdaEventFeedingLoggedPast:
		return p.handleFeedingLoggedPast(ctx, evt)
	case schemas.AdaEventBorn:
		return p.handleBornEvent(ctx, evt)
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
		go p.refreshAllSensors(context.Background())
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

	p.pushFeedingSensors(ctx)
	return nil
}

func (p *Processor) handleFeedingLogged(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaFeedingLoggedData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode feeding_logged: %w", err)
	}

	// Normalize dashboard-style source names to canonical DB values so that
	// isBottleSource and feedingDisplaySource work correctly downstream.
	source := normalizeSource(d.Source)

	startTime, err := parseRFC3339(d.StartTime)
	if err != nil {
		return fmt.Errorf("ada: parse feeding start_time: %w", err)
	}

	feedingID, err := p.q.InsertFeeding(ctx, &store.InsertFeedingParams{
		Timestamp: toTimestamptz(startTime),
		Source:    source,
		LoggedBy:  d.LoggedBy,
	})
	if err != nil {
		return fmt.Errorf("ada: insert feeding_logged: %w", err)
	}

	if isBottleSource(source) {
		if err := p.q.InsertFeedingBottleDetail(ctx, &store.InsertFeedingBottleDetailParams{
			FeedingID:    feedingID,
			AmountOz:     numericFromFloat(d.AmountOz),
			BreastMilkOz: numericFromFloat(d.BreastMilkOz),
			FormulaOz:    numericFromFloat(d.FormulaOz),
		}); err != nil {
			return fmt.Errorf("ada: insert feeding bottle detail: %w", err)
		}
	}

	p.pushFeedingSensors(ctx)
	return nil
}

// handleFeedingLoggedPast handles ada.feeding.log_past — a historical feeding
// entered in one shot with breast timing (seconds per side) and/or bottle amounts
// (millilitres). Source is derived from which fields are non-zero. Breast segments
// are reconstructed with absolute timestamps forwarded from start_time. Bottle
// amounts are converted from ml to oz at ingestion.
func (p *Processor) handleFeedingLoggedPast(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaFeedingLoggedPastData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode feeding_logged_past: %w", err)
	}

	startTime, err := parseRFC3339(d.StartTime)
	if err != nil {
		return fmt.Errorf("ada: parse feeding_logged_past start_time: %w", err)
	}

	hasBreast := d.LeftBreastS > 0 || d.RightBreastS > 0
	hasBottle := d.BreastMilkML > 0 || d.FormulaML > 0

	var source string
	switch {
	case hasBreast && d.LeftBreastS > 0 && d.RightBreastS > 0:
		source = "breast"
	case hasBreast && d.LeftBreastS > 0:
		source = "breast_left"
	case hasBreast:
		source = "breast_right"
	case d.BreastMilkML > 0 && d.FormulaML > 0:
		source = "mixed"
	case d.BreastMilkML > 0:
		source = "bottle_breast"
	default:
		source = "bottle_formula"
	}

	feedingID, err := p.q.InsertFeeding(ctx, &store.InsertFeedingParams{
		Timestamp: toTimestamptz(startTime),
		Source:    source,
		LoggedBy:  d.LoggedBy,
	})
	if err != nil {
		return fmt.Errorf("ada: insert feeding_logged_past: %w", err)
	}

	// Insert breast segments, reconstructing absolute timestamps forward from start.
	segCursor := startTime
	if d.LeftBreastS > 0 {
		segEnd := segCursor.Add(time.Duration(d.LeftBreastS) * time.Second)
		if err := p.q.InsertFeedingSegment(ctx, &store.InsertFeedingSegmentParams{
			FeedingID: feedingID,
			Side:      "left",
			StartedAt: toTimestamptz(segCursor),
			EndedAt:   toTimestamptz(segEnd),
			DurationS: int32(d.LeftBreastS), //nolint:gosec // G115: bounded by session duration in seconds
		}); err != nil {
			return fmt.Errorf("ada: insert left segment for log_past: %w", err)
		}
		segCursor = segEnd
	}
	if d.RightBreastS > 0 {
		segEnd := segCursor.Add(time.Duration(d.RightBreastS) * time.Second)
		if err := p.q.InsertFeedingSegment(ctx, &store.InsertFeedingSegmentParams{
			FeedingID: feedingID,
			Side:      "right",
			StartedAt: toTimestamptz(segCursor),
			EndedAt:   toTimestamptz(segEnd),
			DurationS: int32(d.RightBreastS), //nolint:gosec // G115: bounded by session duration in seconds
		}); err != nil {
			return fmt.Errorf("ada: insert right segment for log_past: %w", err)
		}
	}

	// Insert bottle detail with ml→oz conversion if any liquid amounts are present.
	if hasBottle {
		const mlPerOz = 29.5735
		breastMilkOz := d.BreastMilkML / mlPerOz
		formulaOz := d.FormulaML / mlPerOz
		if err := p.q.InsertFeedingBottleDetail(ctx, &store.InsertFeedingBottleDetailParams{
			FeedingID:    feedingID,
			AmountOz:     numericFromFloat(breastMilkOz + formulaOz),
			BreastMilkOz: numericFromFloat(breastMilkOz),
			FormulaOz:    numericFromFloat(formulaOz),
		}); err != nil {
			return fmt.Errorf("ada: insert bottle detail for log_past: %w", err)
		}
	}

	p.pushFeedingSensors(ctx)
	return nil
}

func (p *Processor) handleFeedingSupplemented(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaFeedingSupplementData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode feeding_supplement: %w", err)
	}

	// A supplement is a bottle top-off during the same breast feeding session.
	// Attach the bottle detail to the most recent feeding row rather than
	// creating a new one — this keeps ada_today_feeding_count accurate.
	feedingID, err := p.q.GetLastFeedingID(ctx)
	if err != nil {
		return fmt.Errorf("ada: get last feeding for supplement: %w", err)
	}

	if err := p.q.InsertFeedingBottleDetail(ctx, &store.InsertFeedingBottleDetailParams{
		FeedingID: feedingID,
		AmountOz:  numericFromFloat(d.AmountOz),
	}); err != nil {
		return fmt.Errorf("ada: insert supplement bottle detail: %w", err)
	}

	// Push only oz sensors — last_feeding_time, last_feeding_source,
	// next_feeding_target, and today_feeding_count must not change.
	p.pushSupplementOzSensor(ctx)
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

// ── Birth profile handler ─────────────────────────────────────────────────────

func (p *Processor) handleBornEvent(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaBornData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode born: %w", err)
	}

	birthAt, err := time.Parse(time.RFC3339, d.BirthAt)
	if err != nil {
		return fmt.Errorf("ada: parse birth_at %q: %w", d.BirthAt, err)
	}

	if err := p.q.UpsertProfile(ctx, toTimestamptz(birthAt)); err != nil {
		return fmt.Errorf("ada: upsert profile: %w", err)
	}

	p.log.Info("ada: birth profile saved",
		slog.String("birth_at", birthAt.UTC().Format(time.RFC3339)),
		slog.String("logged_by", d.LoggedBy),
	)
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
// It queries GetLastFeeding to determine the actual most-recent feeding by timestamp,
// so backdated entries never displace a later feeding from the last-fed display.
func (p *Processor) pushFeedingSensors(ctx context.Context) {
	last, err := p.q.GetLastFeeding(ctx)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			p.log.Warn("ada: get last feeding for sensor push", slog.String("error", err.Error()))
		}
		return
	}

	lastFeedingTime := last.Timestamp.Time
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
		{sensorLastFeedingSource, feedingDisplaySource(last.Source, last.HasBottleDetail)},
		{sensorNextFeedingTarget, nextTargetStr},
	}
	if agg != nil {
		pushes = append(pushes,
			struct{ id, state string }{sensorTodayFeedingCount, strconv.Itoa(int(agg.Count))},
			struct{ id, state string }{sensorTodayFeedingOz, strconv.FormatFloat(agg.TotalOz, 'f', 2, 64)},
		)
	}
	p.pushAll(ctx, pushes)
	p.pushFeedingHistory(ctx)
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
			struct{ id, state string }{sensorTodayDiaperMixed, strconv.Itoa(int(agg.Mixed))},
		)
	}
	p.pushAll(ctx, pushes)
	p.pushDiaperHistory(ctx)
}

// pushSupplementOzSensor pushes today_feeding_oz and last_feeding_source after
// a supplement event. Count, last time, and next target must not change.
// last_feeding_source is updated to "supplemented" — a supplement always follows
// a breast feeding session, so the combined display label is always "supplemented".
func (p *Processor) pushSupplementOzSensor(ctx context.Context) {
	agg, err := p.q.GetTodayFeedingAggregates(ctx)
	if err != nil {
		p.log.Warn("ada: get today feeding aggregates for supplement", slog.String("error", err.Error()))
		return
	}
	p.pushAll(ctx, []struct{ id, state string }{
		{sensorTodayFeedingOz, strconv.FormatFloat(agg.TotalOz, 'f', 2, 64)},
		{sensorLastFeedingSource, "supplemented"},
	})
	p.pushFeedingHistory(ctx)
}

// pushSleepStartedSensors pushes sensors after a sleep session starts.
// sensorSleepSessionMin is set to the elapsed minutes from startTime, which
// correctly reflects a backdated start (e.g. "started 15 min ago" → pushes 15).
func (p *Processor) pushSleepStartedSensors(ctx context.Context, startTime time.Time) {
	p.pushAll(ctx, []struct{ id, state string }{
		{sensorSleepState, "sleeping"},
		{sensorLastSleepChange, startTime.UTC().Format(time.RFC3339)},
		{sensorSleepSessionMin, strconv.Itoa(sleepElapsedMin(startTime))},
	})
	p.pushSleepHistory(ctx)
}

// pushSleepEndedSensors pushes sensors after a sleep session ends.
// sensorSleepSessionMin is reset to "0" — the session is over.
func (p *Processor) pushSleepEndedSensors(ctx context.Context, endTime time.Time) {
	agg, err := p.q.GetTodaySleepAggregates(ctx)
	if err != nil {
		p.log.Warn("ada: get today sleep aggregates", slog.String("error", err.Error()))
	}

	pushes := []struct{ id, state string }{
		{sensorSleepState, "awake"},
		{sensorLastSleepChange, endTime.UTC().Format(time.RFC3339)},
		{sensorSleepSessionMin, "0"},
	}
	if agg != nil {
		pushes = append(pushes,
			struct{ id, state string }{sensorTodaySleepHours, strconv.FormatFloat(agg.TotalHours, 'f', 2, 64)},
			struct{ id, state string }{sensorTodaySleepNapCount, strconv.Itoa(int(agg.NapCount))},
		)
	}
	p.pushAll(ctx, pushes)
	p.pushSleepHistory(ctx)
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

// ── Sensor restore / periodic refresh ────────────────────────────────────────

// refreshAllSensors re-pushes the complete Ada sensor set from Postgres.
// Called on engine startup, HA reconnect, and the 4-hour safety-net ticker tick.
func (p *Processor) refreshAllSensors(ctx context.Context) {
	p.pushLastEventSensors(ctx)
	p.pushDailyAggregates(ctx)
	p.pushActiveSleepState(ctx)
}

// pushLastEventSensors pushes sensors derived from the most recent event of each
// type: last feeding time/source/next-target and last diaper time/type.
// These change only when a new event is logged, not on daily rollover.
func (p *Processor) pushLastEventSensors(ctx context.Context) {
	if f, err := p.q.GetLastFeeding(ctx); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorLastFeedingTime, f.Timestamp.Time.UTC().Format(time.RFC3339)},
			{sensorLastFeedingSource, feedingDisplaySource(f.Source, f.HasBottleDetail)},
		})
	} else if !errors.Is(err, pgx.ErrNoRows) {
		p.log.Warn("ada: restore last feeding", slog.String("error", err.Error()))
	}

	if cfg, err := p.q.GetConfig(ctx, cfgKeyNextFeedingTarget); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{{sensorNextFeedingTarget, cfg.Value}})
	} else if !errors.Is(err, pgx.ErrNoRows) {
		p.log.Warn("ada: restore next_feeding_target", slog.String("error", err.Error()))
	}

	if d, err := p.q.GetLastDiaper(ctx); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorLastDiaperTime, d.Timestamp.Time.UTC().Format(time.RFC3339)},
			{sensorLastDiaperType, d.Type},
		})
	} else if !errors.Is(err, pgx.ErrNoRows) {
		p.log.Warn("ada: restore last diaper", slog.String("error", err.Error()))
	}
}

// pushDailyAggregates pushes all today_* aggregate sensors from Postgres.
// Called on startup, HA reconnect, midnight rollover, and the 4-hour safety net.
func (p *Processor) pushDailyAggregates(ctx context.Context) {
	if agg, err := p.q.GetTodayFeedingAggregates(ctx); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorTodayFeedingCount, strconv.Itoa(int(agg.Count))},
			{sensorTodayFeedingOz, strconv.FormatFloat(agg.TotalOz, 'f', 2, 64)},
		})
	} else {
		p.log.Warn("ada: restore today feeding aggregates", slog.String("error", err.Error()))
	}
	p.pushFeedingHistory(ctx)

	if agg, err := p.q.GetTodayDiaperAggregates(ctx); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorTodayDiaperCount, strconv.Itoa(int(agg.Total))},
			{sensorTodayDiaperWet, strconv.Itoa(int(agg.Wet))},
			{sensorTodayDiaperDirty, strconv.Itoa(int(agg.Dirty))},
			{sensorTodayDiaperMixed, strconv.Itoa(int(agg.Mixed))},
		})
	} else {
		p.log.Warn("ada: restore today diaper aggregates", slog.String("error", err.Error()))
	}
	p.pushDiaperHistory(ctx)

	if agg, err := p.q.GetTodaySleepAggregates(ctx); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorTodaySleepHours, strconv.FormatFloat(agg.TotalHours, 'f', 2, 64)},
			{sensorTodaySleepNapCount, strconv.Itoa(int(agg.NapCount))},
		})
	} else {
		p.log.Warn("ada: restore today sleep aggregates", slog.String("error", err.Error()))
	}
	p.pushSleepHistory(ctx)

	if agg, err := p.q.GetTodayTummyAggregates(ctx); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorTodayTummyMin, strconv.Itoa(int(agg.TotalMinutes))},
			{sensorTodayTummySessions, strconv.Itoa(int(agg.Sessions))},
		})
	} else {
		p.log.Warn("ada: restore today tummy aggregates", slog.String("error", err.Error()))
	}
}

// pushActiveSleepState pushes the current sleep state and session elapsed time.
// If a session is active: state="sleeping", session_min=elapsed minutes from start.
// If no active session: state="awake", session_min="0", last_sleep_change=last end time.
func (p *Processor) pushActiveSleepState(ctx context.Context) {
	active, err := p.q.GetActiveSleepSession(ctx)
	if err == nil {
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorSleepState, "sleeping"},
			{sensorLastSleepChange, active.Time.UTC().Format(time.RFC3339)},
			{sensorSleepSessionMin, strconv.Itoa(sleepElapsedMin(active.Time))},
		})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		p.log.Warn("ada: restore active sleep session", slog.String("error", err.Error()))
		return
	}
	// No active session — push awake state and last sleep end if available.
	p.pushAll(ctx, []struct{ id, state string }{
		{sensorSleepState, "awake"},
		{sensorSleepSessionMin, "0"},
	})
	if lastEnd, err := p.q.GetLastSleepEnd(ctx); err == nil {
		p.pushAll(ctx, []struct{ id, state string }{
			{sensorLastSleepChange, lastEnd.Time.UTC().Format(time.RFC3339)},
		})
	} else if !errors.Is(err, pgx.ErrNoRows) {
		p.log.Warn("ada: restore last sleep end", slog.String("error", err.Error()))
	}
}

// ── Background ticker ─────────────────────────────────────────────────────────

// runTicker runs the background ticker goroutine. Started in Initialize,
// stopped via stopCh in Shutdown.
func (p *Processor) runTicker() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.onTick(context.Background())
		case <-p.stopCh:
			return
		}
	}
}

// onTick runs on each 60-second ticker tick. It handles three concerns:
//
//  1. 4-hour safety net: full sensor re-push to recover from any HA state loss.
//  2. Midnight rollover: re-push daily aggregate sensors when the calendar date changes.
//  3. Sleep session timer: push sensorSleepSessionMin with current elapsed minutes
//     (only when a session is active; no-op when awake to avoid redundant pushes).
func (p *Processor) onTick(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	today := startOfDay(time.Now().Local())

	// 4-hour safety net: full refresh covers all three sub-pushes.
	if time.Since(p.lastFullRefresh) >= 4*time.Hour {
		p.log.Info("ada: 4-hour safety net — full sensor refresh")
		p.refreshAllSensors(ctx)
		p.lastFullRefresh = time.Now()
		p.lastRefreshDate = today
		return
	}

	// Midnight rollover: reset today_* aggregates when the date changes.
	if today.After(p.lastRefreshDate) {
		p.log.Info("ada: midnight rollover — refreshing daily aggregates")
		p.pushDailyAggregates(ctx)
		p.lastRefreshDate = today
	}

	// Sleep session timer: push elapsed minutes if a session is active.
	active, err := p.q.GetActiveSleepSession(ctx)
	if err == nil {
		if haErr := p.ha.PushState(ctx, sensorSleepSessionMin, strconv.Itoa(sleepElapsedMin(active.Time)), nil); haErr != nil {
			p.log.Warn("ada: ticker push sleep_session_min", slog.String("error", haErr.Error()))
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		p.log.Warn("ada: ticker query active sleep session", slog.String("error", err.Error()))
	}
}

// ── Helpers ── (sleep elapsed time) ──────────────────────────────────────────

// sleepElapsedMin returns the number of whole minutes elapsed since startTime.
// Used when pushing sensorSleepSessionMin so the computation is testable in isolation.
func sleepElapsedMin(startTime time.Time) int {
	return int(time.Since(startTime).Minutes())
}

// startOfDay returns midnight of t's date in t's local timezone.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// ── Feeding history cache ─────────────────────────────────────────────────────

// FeedingHistoryEntry is one element of the JSON array pushed as attributes on
// sensor.ada_feeding_history. Only fields relevant to the source type are set;
// zero-value oz fields are omitted from JSON output via omitempty.
type FeedingHistoryEntry struct {
	ID             string   `json:"id"`
	Timestamp      string   `json:"timestamp"`
	Source         string   `json:"source"`
	LeftDurationS  int      `json:"left_duration_s"`
	RightDurationS int      `json:"right_duration_s"`
	AmountOz       *float64 `json:"amount_oz,omitempty"`
	BreastMilkOz   *float64 `json:"breast_milk_oz,omitempty"`
	FormulaOz      *float64 `json:"formula_oz,omitempty"`
}

// buildFeedingHistory converts sqlc rows to JSON-serializable history entries.
// oz fields that are 0 (no bottle detail on a breast feeding) are left nil so
// they are omitted from the JSON payload sent to HA.
func buildFeedingHistory(rows []*store.GetLast24hFeedingsRow) []FeedingHistoryEntry {
	entries := make([]FeedingHistoryEntry, 0, len(rows))
	for _, r := range rows {
		e := FeedingHistoryEntry{
			ID:             uuid.UUID(r.ID.Bytes).String(),
			Timestamp:      r.Timestamp.Time.UTC().Format(time.RFC3339),
			Source:         feedingDisplaySource(r.Source, r.AmountOz != 0 || r.BreastMilkOz != 0 || r.FormulaOz != 0),
			LeftDurationS:  int(r.LeftDurationS),
			RightDurationS: int(r.RightDurationS),
		}
		if r.AmountOz != 0 {
			v := r.AmountOz
			e.AmountOz = &v
		}
		if r.BreastMilkOz != 0 {
			v := r.BreastMilkOz
			e.BreastMilkOz = &v
		}
		if r.FormulaOz != 0 {
			v := r.FormulaOz
			e.FormulaOz = &v
		}
		entries = append(entries, e)
	}
	return entries
}

// pushFeedingHistory queries the last 24h of feedings and pushes them as
// attributes on sensor.ada_feeding_history. The sensor state is the entry
// count so HA has a meaningful scalar to observe. An empty result pushes
// state="0" and an empty entries array.
func (p *Processor) pushFeedingHistory(ctx context.Context) {
	rows, err := p.q.GetLast24hFeedings(ctx)
	if err != nil {
		p.log.Warn("ada: query feeding history failed", slog.String("error", err.Error()))
		return
	}

	entries := buildFeedingHistory(rows)

	attributes := map[string]any{
		"entries":      entries,
		"last_updated": time.Now().UTC().Format(time.RFC3339),
	}

	if err := p.ha.PushState(ctx, "sensor.ada_feeding_history", strconv.Itoa(len(entries)), attributes); err != nil {
		p.log.Warn("ada: push feeding history failed", slog.String("error", err.Error()))
	}
}

// ── Diaper history ────────────────────────────────────────────────────────────

// DiaperHistoryEntry is one element of the JSON array pushed as attributes on
// sensor.ada_diaper_history. Sensor state is the entry count.
type DiaperHistoryEntry struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
}

// buildDiaperHistory converts sqlc rows to JSON-serializable history entries.
func buildDiaperHistory(rows []*store.GetLast24hDiapersRow) []DiaperHistoryEntry {
	entries := make([]DiaperHistoryEntry, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, DiaperHistoryEntry{
			ID:        uuid.UUID(r.ID.Bytes).String(),
			Timestamp: r.Timestamp.Time.UTC().Format(time.RFC3339),
			Type:      r.Type,
		})
	}
	return entries
}

// pushDiaperHistory queries the last 24h of diaper events and pushes them as
// attributes on sensor.ada_diaper_history. Sensor state is the entry count.
func (p *Processor) pushDiaperHistory(ctx context.Context) {
	rows, err := p.q.GetLast24hDiapers(ctx)
	if err != nil {
		p.log.Warn("ada: query diaper history", slog.String("error", err.Error()))
		return
	}
	entries := buildDiaperHistory(rows)
	attributes := map[string]any{
		"entries":      entries,
		"last_updated": time.Now().UTC().Format(time.RFC3339),
	}
	if err := p.ha.PushState(ctx, sensorDiaperHistory, strconv.Itoa(len(entries)), attributes); err != nil {
		p.log.Warn("ada: push diaper history", slog.String("error", err.Error()))
	}
}

// ── Sleep history ─────────────────────────────────────────────────────────────

// SleepHistoryEntry is one element of the JSON array pushed as attributes on
// sensor.ada_sleep_history. EndTime and DurationS are omitted for active sessions.
type SleepHistoryEntry struct {
	ID        string  `json:"id"`
	StartTime string  `json:"start_time"`
	EndTime   *string `json:"end_time,omitempty"`
	SleepType string  `json:"sleep_type"`
	DurationS *int    `json:"duration_s,omitempty"`
}

// buildSleepHistory converts sqlc rows to JSON-serializable history entries.
// Active sessions (EndTime.Valid=false) are included with EndTime and DurationS omitted.
func buildSleepHistory(rows []*store.GetLast24hSleepSessionsRow) []SleepHistoryEntry {
	entries := make([]SleepHistoryEntry, 0, len(rows))
	for _, r := range rows {
		e := SleepHistoryEntry{
			ID:        uuid.UUID(r.ID.Bytes).String(),
			StartTime: r.StartTime.Time.UTC().Format(time.RFC3339),
			SleepType: r.SleepType,
		}
		if r.EndTime.Valid {
			s := r.EndTime.Time.UTC().Format(time.RFC3339)
			e.EndTime = &s
			d := int(r.EndTime.Time.Sub(r.StartTime.Time).Seconds())
			e.DurationS = &d
		}
		entries = append(entries, e)
	}
	return entries
}

// pushSleepHistory queries the last 24h of sleep sessions and pushes them as
// attributes on sensor.ada_sleep_history. Active sessions are included with
// end_time and duration_s omitted. Sensor state is the total session count.
func (p *Processor) pushSleepHistory(ctx context.Context) {
	rows, err := p.q.GetLast24hSleepSessions(ctx)
	if err != nil {
		p.log.Warn("ada: query sleep history", slog.String("error", err.Error()))
		return
	}
	entries := buildSleepHistory(rows)
	attributes := map[string]any{
		"entries":      entries,
		"last_updated": time.Now().UTC().Format(time.RFC3339),
	}
	if err := p.ha.PushState(ctx, sensorSleepHistory, strconv.Itoa(len(entries)), attributes); err != nil {
		p.log.Warn("ada: push sleep history", slog.String("error", err.Error()))
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

// normalizeSource maps dashboard-style source names to canonical DB values.
// The dashboard sends display-style names ("formula", "breast_milk"); the DB
// and all downstream logic expect canonical names ("bottle_formula", "bottle_breast").
func normalizeSource(source string) string {
	switch source {
	case "formula":
		return "bottle_formula"
	case "breast_milk":
		return "bottle_breast"
	default:
		return source
	}
}

// isBottleSource reports whether a feeding source string indicates a bottle feeding.
// Handles both canonical DB values and legacy dashboard-style values.
func isBottleSource(source string) bool {
	switch source {
	case "bottle_breast", "bottle_formula", "mixed",
		"breast_milk", "formula": // legacy dashboard values
		return true
	}
	return false
}

// feedingDisplaySource maps a raw DB feeding source to a human-readable label.
//
//   - breast, breast_left, breast_right (no bottle detail) → "breast"
//   - breast feed + bottle detail                          → "supplemented"
//   - bottle_breast or breast_milk                         → "breast milk"
//   - bottle_formula or formula                            → "formula"
//   - anything else                                        → source as-is
func feedingDisplaySource(source string, hasBottleDetail bool) string {
	isBreastFeed := source == "breast" || source == "breast_left" || source == "breast_right"
	switch {
	case isBreastFeed && hasBottleDetail:
		return "supplemented"
	case isBreastFeed:
		return "breast"
	case source == "bottle_breast" || source == "breast_milk":
		return "breast milk"
	case source == "bottle_formula" || source == "formula":
		return "formula"
	default:
		return source
	}
}
