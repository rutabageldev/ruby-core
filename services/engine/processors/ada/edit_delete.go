package ada

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/ada/store"
)

// Edit & delete handlers (#77, #78, #79). Updates are full-resolution replacements:
// the payload carries the complete composition of the event and the stored record is
// rewritten to match, then all derived sensors are recomputed. Deletes are soft
// (deleted_at), so every read path (which filters deleted_at IS NULL) recomputes too.

// eventTest reads the envelope-level "test" marker from a CloudEvent payload.
// The dashboard stamps test:true on every event it fires while live-test mode is
// on (ADR-0031); absence means real data. Carried on inserts only — edits never
// change a row's test-ness.
func eventTest(evt schemas.CloudEvent) bool {
	t, _ := evt.Data["test"].(bool)
	return t
}

// parseUUID parses a dashboard-supplied id string into a pgtype.UUID.
func parseUUID(s string) (pgtype.UUID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("ada: parse id %q: %w", s, err)
	}
	return pgtype.UUID{Bytes: u, Valid: true}, nil
}

// deriveFeedingSource classifies a feed from its components, identically to a fresh
// submission: breast sides take precedence, then bottle composition. Shared by
// feeding.update and feeding.log_past.
func deriveFeedingSource(leftS, rightS int, breastMilkOz, formulaOz float64) string {
	hasBreast := leftS > 0 || rightS > 0
	switch {
	case hasBreast && leftS > 0 && rightS > 0:
		return "breast"
	case hasBreast && leftS > 0:
		return "breast_left"
	case hasBreast:
		return "breast_right"
	case breastMilkOz > 0 && formulaOz > 0:
		return "mixed"
	case breastMilkOz > 0:
		return "bottle_breast"
	default:
		return "bottle_formula"
	}
}

// ── Feeding ────────────────────────────────────────────────────────────────────

func (p *Processor) handleFeedingUpdate(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaFeedingUpdateData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode feeding_update: %w", err)
	}
	id, err := parseUUID(d.ID)
	if err != nil {
		return err
	}
	startTime, err := parseRFC3339(d.StartTime)
	if err != nil {
		return fmt.Errorf("ada: parse feeding_update start_time: %w", err)
	}
	source := deriveFeedingSource(d.LeftBreastS, d.RightBreastS, d.BreastMilkOz, d.FormulaOz)

	// A feeding spans three tables; rewrite them atomically so a partial edit can
	// never leave orphaned segments or bottle detail.
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ada: begin feeding_update tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful commit
	qtx := p.q.WithTx(tx)

	if err := qtx.UpdateFeeding(ctx, &store.UpdateFeedingParams{
		ID:        id,
		Timestamp: toTimestamptz(startTime),
		Source:    source,
		LoggedBy:  d.LoggedBy,
	}); err != nil {
		return fmt.Errorf("ada: update feeding: %w", err)
	}
	if err := qtx.DeleteFeedingSegments(ctx, id); err != nil {
		return fmt.Errorf("ada: clear feeding segments: %w", err)
	}
	if err := qtx.DeleteFeedingBottleDetail(ctx, id); err != nil {
		return fmt.Errorf("ada: clear feeding bottle detail: %w", err)
	}

	segCursor := startTime
	if d.LeftBreastS > 0 {
		segEnd := segCursor.Add(time.Duration(d.LeftBreastS) * time.Second)
		if err := qtx.InsertFeedingSegment(ctx, &store.InsertFeedingSegmentParams{
			FeedingID: id, Side: "left",
			StartedAt: toTimestamptz(segCursor), EndedAt: toTimestamptz(segEnd),
			DurationS: int32(d.LeftBreastS), //nolint:gosec // G115: bounded by session duration in seconds
		}); err != nil {
			return fmt.Errorf("ada: insert left segment for update: %w", err)
		}
		segCursor = segEnd
	}
	if d.RightBreastS > 0 {
		segEnd := segCursor.Add(time.Duration(d.RightBreastS) * time.Second)
		if err := qtx.InsertFeedingSegment(ctx, &store.InsertFeedingSegmentParams{
			FeedingID: id, Side: "right",
			StartedAt: toTimestamptz(segCursor), EndedAt: toTimestamptz(segEnd),
			DurationS: int32(d.RightBreastS), //nolint:gosec // G115: bounded by session duration in seconds
		}); err != nil {
			return fmt.Errorf("ada: insert right segment for update: %w", err)
		}
	}
	if d.BreastMilkOz > 0 || d.FormulaOz > 0 {
		if err := qtx.InsertFeedingBottleDetail(ctx, &store.InsertFeedingBottleDetailParams{
			FeedingID:    id,
			AmountOz:     numericFromFloat(d.BreastMilkOz + d.FormulaOz),
			BreastMilkOz: numericFromFloat(d.BreastMilkOz),
			FormulaOz:    numericFromFloat(d.FormulaOz),
		}); err != nil {
			return fmt.Errorf("ada: insert bottle detail for update: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ada: commit feeding_update: %w", err)
	}
	p.pushFeedingSensors(ctx)
	return nil
}

func (p *Processor) handleFeedingDelete(ctx context.Context, evt schemas.CloudEvent) error {
	id, err := p.decodeDeleteID(evt, "feeding")
	if err != nil {
		return err
	}
	if err := p.q.SoftDeleteFeeding(ctx, id); err != nil {
		return fmt.Errorf("ada: soft-delete feeding: %w", err)
	}
	p.pushFeedingSensors(ctx)
	return nil
}

// ── Diaper ─────────────────────────────────────────────────────────────────────

func (p *Processor) handleDiaperUpdate(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaDiaperUpdateData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode diaper_update: %w", err)
	}
	id, err := parseUUID(d.ID)
	if err != nil {
		return err
	}
	ts, err := parseRFC3339(d.Timestamp)
	if err != nil {
		return fmt.Errorf("ada: parse diaper_update timestamp: %w", err)
	}
	if err := p.q.UpdateDiaper(ctx, &store.UpdateDiaperParams{
		ID: id, Timestamp: toTimestamptz(ts), Type: d.Type, LoggedBy: d.LoggedBy,
	}); err != nil {
		return fmt.Errorf("ada: update diaper: %w", err)
	}
	p.pushDiaperSensors(ctx)
	return nil
}

func (p *Processor) handleDiaperDelete(ctx context.Context, evt schemas.CloudEvent) error {
	id, err := p.decodeDeleteID(evt, "diaper")
	if err != nil {
		return err
	}
	if err := p.q.SoftDeleteDiaper(ctx, id); err != nil {
		return fmt.Errorf("ada: soft-delete diaper: %w", err)
	}
	p.pushDiaperSensors(ctx)
	return nil
}

// ── Sleep ──────────────────────────────────────────────────────────────────────

func (p *Processor) handleSleepUpdate(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaSleepUpdateData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode sleep_update: %w", err)
	}
	id, err := parseUUID(d.ID)
	if err != nil {
		return err
	}
	startTime, err := parseRFC3339(d.StartTime)
	if err != nil {
		return fmt.Errorf("ada: parse sleep_update start_time: %w", err)
	}
	// An empty end_time means the session is still active (end_time NULL).
	var endTz pgtype.Timestamptz
	if d.EndTime != "" {
		endTime, err := parseRFC3339(d.EndTime)
		if err != nil {
			return fmt.Errorf("ada: parse sleep_update end_time: %w", err)
		}
		endTz = toTimestamptz(endTime)
	}
	sleepType := d.SleepType
	if sleepType == "" {
		cfg := p.loadSleepConfig(ctx)
		sleepType = categorizeSleep(startTime, cfg.BedtimeHHMM, cfg.DaytimeHHMM, cfg.GraceMin)
	}
	if err := p.q.UpdateSleepSession(ctx, &store.UpdateSleepSessionParams{
		ID: id, StartTime: toTimestamptz(startTime), EndTime: endTz,
		SleepType: sleepType, LoggedBy: d.LoggedBy,
	}); err != nil {
		return fmt.Errorf("ada: update sleep session: %w", err)
	}
	p.pushSleepEndedSensors(ctx)
	return nil
}

func (p *Processor) handleSleepDelete(ctx context.Context, evt schemas.CloudEvent) error {
	id, err := p.decodeDeleteID(evt, "sleep")
	if err != nil {
		return err
	}
	if err := p.q.SoftDeleteSleepSession(ctx, id); err != nil {
		return fmt.Errorf("ada: soft-delete sleep session: %w", err)
	}
	p.pushSleepEndedSensors(ctx)
	return nil
}

// ── Tummy ──────────────────────────────────────────────────────────────────────

func (p *Processor) handleTummyUpdate(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaTummyUpdateData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode tummy_update: %w", err)
	}
	id, err := parseUUID(d.ID)
	if err != nil {
		return err
	}
	startTime, err := parseRFC3339(d.StartTime)
	if err != nil {
		return fmt.Errorf("ada: parse tummy_update start_time: %w", err)
	}
	endTime, err := parseRFC3339(d.EndTime)
	if err != nil {
		return fmt.Errorf("ada: parse tummy_update end_time: %w", err)
	}
	if err := p.q.UpdateTummySession(ctx, &store.UpdateTummySessionParams{
		ID: id, StartTime: toTimestamptz(startTime), EndTime: toTimestamptz(endTime),
		DurationS: int32(d.DurationS), //nolint:gosec // G115: bounded by session duration in seconds
		LoggedBy:  d.LoggedBy,
	}); err != nil {
		return fmt.Errorf("ada: update tummy session: %w", err)
	}
	p.pushTummySensors(ctx)
	p.pushTummyHistory(ctx)
	return nil
}

func (p *Processor) handleTummyDelete(ctx context.Context, evt schemas.CloudEvent) error {
	id, err := p.decodeDeleteID(evt, "tummy")
	if err != nil {
		return err
	}
	if err := p.q.SoftDeleteTummySession(ctx, id); err != nil {
		return fmt.Errorf("ada: soft-delete tummy session: %w", err)
	}
	p.pushTummySensors(ctx)
	p.pushTummyHistory(ctx)
	return nil
}

// ── Growth ─────────────────────────────────────────────────────────────────────

func (p *Processor) handleGrowthUpdate(ctx context.Context, evt schemas.CloudEvent) error {
	var d schemas.AdaGrowthUpdateData
	if err := remarshal(evt.Data, &d); err != nil {
		return fmt.Errorf("ada: decode growth_update: %w", err)
	}
	if d.WeightOz == nil && d.LengthIn == nil && d.HeadCircumferenceIn == nil {
		p.log.Warn("ada: growth_update has no measurements — ignoring")
		return nil
	}
	id, err := parseUUID(d.ID)
	if err != nil {
		return err
	}
	measuredAt, err := parseRFC3339(d.Timestamp)
	if err != nil {
		return fmt.Errorf("ada: parse growth_update timestamp: %w", err)
	}
	weightPct, lengthPct, headPct := p.computeGrowthPercentiles(ctx, measuredAt, d.WeightOz, d.LengthIn, d.HeadCircumferenceIn)
	if err := p.q.UpdateGrowthMeasurement(ctx, &store.UpdateGrowthMeasurementParams{
		ID:                  id,
		MeasuredAt:          toTimestamptz(measuredAt),
		WeightOz:            numericFromFloatPtr(d.WeightOz),
		LengthIn:            numericFromFloatPtr(d.LengthIn),
		HeadCircumferenceIn: numericFromFloatPtr(d.HeadCircumferenceIn),
		Source:              d.Source,
		WeightPct:           numericFromFloatPtr(weightPct),
		LengthPct:           numericFromFloatPtr(lengthPct),
		HeadPct:             numericFromFloatPtr(headPct),
		LoggedBy:            d.LoggedBy,
	}); err != nil {
		return fmt.Errorf("ada: update growth measurement: %w", err)
	}
	p.pushGrowthSensors(ctx)
	return nil
}

func (p *Processor) handleGrowthDelete(ctx context.Context, evt schemas.CloudEvent) error {
	id, err := p.decodeDeleteID(evt, "growth")
	if err != nil {
		return err
	}
	if err := p.q.SoftDeleteGrowthMeasurement(ctx, id); err != nil {
		return fmt.Errorf("ada: soft-delete growth measurement: %w", err)
	}
	p.pushGrowthSensors(ctx)
	return nil
}

// decodeDeleteID decodes the shared {id} delete payload and parses the id.
func (p *Processor) decodeDeleteID(evt schemas.CloudEvent, kind string) (pgtype.UUID, error) {
	var d schemas.AdaDeleteData
	if err := remarshal(evt.Data, &d); err != nil {
		return pgtype.UUID{}, fmt.Errorf("ada: decode %s_delete: %w", kind, err)
	}
	return parseUUID(d.ID)
}
