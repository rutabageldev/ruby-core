package calendar

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	calendarv3 "google.golang.org/api/calendar/v3"

	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/calendar/gcal"
)

// handleUpsert applies a calendar.event.upsert: create (no google_event_id) or
// update (with etag If-Match). Creates dedupe on idempotency_key; updates resync
// and retry once on a 412. The mirror is upserted from Google's returned event in
// the same operation (ADR-0042).
func (p *Processor) handleUpsert(ctx context.Context, evt *schemas.CloudEvent) error {
	var d schemas.CalendarUpsertData
	if err := decodeData(evt.Data, &d); err != nil {
		p.log.Warn("calendar: bad upsert payload", slog.String("error", err.Error()))
		return nil // ack malformed
	}

	gev := payloadToGoogle(&d)

	var result *calendarv3.Event
	var err error
	if d.GoogleEventID == "" {
		result, err = p.create(ctx, evt.ID, &d, gev)
	} else {
		result, err = p.update(ctx, &d, gev)
	}
	if err != nil {
		return err // transient → NAK/redeliver
	}
	if result == nil {
		return nil // deduped create — nothing to mirror
	}

	params, err := googleToParams(result, p.calendarID)
	if err != nil {
		return fmt.Errorf("calendar: map result: %w", err)
	}
	if err := p.q.UpsertEvent(ctx, params); err != nil {
		return fmt.Errorf("calendar: mirror upsert: %w", err)
	}

	// Overlay associations ride inside the upsert payload (Slice D).
	if err := p.reconcileAssociations(ctx, result.Id, &d); err != nil {
		return fmt.Errorf("calendar: reconcile associations: %w", err)
	}

	p.log.Info("calendar: event written",
		slog.String("google_event_id", result.Id),
		slog.String("logged_by", d.LoggedBy),
	)
	return nil
}

// create writes a new event through to Google. Idempotency is enforced AT GOOGLE: the
// event id is derived deterministically from the idempotency_key (or the CloudEvent id
// as fallback), so a redelivered create hits the same id and Google returns 409. We
// then fetch the existing event and converge the mirror — never a second insert
// (ADR-0042). This is robust to the redelivery/concurrency window that an application
// dedup store cannot close (ADR-0025).
func (p *Processor) create(ctx context.Context, eventID string, d *schemas.CalendarUpsertData, gev *calendarv3.Event) (*calendarv3.Event, error) {
	seed := d.IdempotencyKey
	if seed == "" {
		seed = eventID
	}
	if seed != "" {
		gev.Id = deterministicEventID(seed)
	} else {
		// No stable seed: fall back to a Google-assigned id. A redelivery here can
		// double-insert — the gateway MUST populate idempotency_key (ADR-0042).
		p.log.Warn("calendar: create without idempotency_key or event id — id is non-deterministic, redelivery may duplicate")
	}

	out, err := p.gcal.Insert(ctx, p.calendarID, gev)
	if errors.Is(err, gcal.ErrDuplicate) {
		// Redelivered create: the event already exists under the deterministic id.
		// Fetch it so the caller converges the mirror + associations (no second event).
		p.log.Info("calendar: duplicate create — converging mirror from existing event", slog.String("google_event_id", gev.Id))
		existing, gerr := p.gcal.Get(ctx, p.calendarID, gev.Id)
		if gerr != nil {
			return nil, fmt.Errorf("calendar: get after 409: %w", gerr)
		}
		return existing, nil
	}
	if err != nil {
		return nil, fmt.Errorf("calendar: google insert: %w", err)
	}
	return out, nil
}

// update uses Google events.patch (not update/replace) so fields the caller omits — notably
// recurrence, which the HA surface drops when untouched — are preserved rather than stripped
// (ADR-0044). Optimistic concurrency and the resync-and-retry-once-on-412 flow are unchanged.
func (p *Processor) update(ctx context.Context, d *schemas.CalendarUpsertData, gev *calendarv3.Event) (*calendarv3.Event, error) {
	out, err := p.gcal.Patch(ctx, p.calendarID, d.GoogleEventID, d.Etag, gev)
	if errors.Is(err, gcal.ErrConflict) {
		// Stale etag: fetch the current event, refresh the mirror, retry once with
		// the fresh etag rather than clobbering a concurrent edit.
		p.log.Warn("calendar: etag conflict (412) — resyncing and retrying", slog.String("google_event_id", d.GoogleEventID))
		fresh, rerr := p.gcal.Get(ctx, p.calendarID, d.GoogleEventID)
		if rerr != nil {
			return nil, fmt.Errorf("calendar: resync after 412: %w", rerr)
		}
		if params, perr := googleToParams(fresh, p.calendarID); perr == nil {
			_ = p.q.UpsertEvent(ctx, params)
		}
		out, err = p.gcal.Patch(ctx, p.calendarID, d.GoogleEventID, trimEtag(fresh.Etag), gev)
	}
	if err != nil {
		return nil, fmt.Errorf("calendar: google update: %w", err)
	}
	return out, nil
}

// handleDelete applies a calendar.event.delete (series-level for MVP): delete in
// Google, then remove the mirror row. Overlay rows cascade via the FK.
//
// Idempotency is real, not error-swallowing (ADR-0042): the local mirror is the source
// of truth for "already deleted". A redelivered delete whose mirror row is gone (the
// hard DELETE is the last successful step) or marked cancelled (the poller may re-mirror
// a tombstone) is skipped before any Google call — so the 410 never arises. The 410/404
// backstop only covers the crash window where Google was deleted but the mirror row
// survived; there, "event absent" is the satisfied postcondition, and we finish the
// mirror cleanup.
func (p *Processor) handleDelete(ctx context.Context, evt *schemas.CloudEvent) error {
	var d schemas.CalendarDeleteData
	if err := decodeData(evt.Data, &d); err != nil {
		p.log.Warn("calendar: bad delete payload", slog.String("error", err.Error()))
		return nil
	}
	if d.GoogleEventID == "" {
		p.log.Warn("calendar: delete missing google_event_id")
		return nil
	}

	// Primary idempotency: if the mirror no longer holds the event (or holds a
	// cancelled tombstone), the delete already completed — do not reprocess.
	existing, err := p.q.GetEvent(ctx, d.GoogleEventID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && existing.Status == "cancelled") {
		p.log.Info("calendar: delete already applied, skipping",
			slog.String("google_event_id", d.GoogleEventID))
		return nil
	}
	if err != nil {
		return fmt.Errorf("calendar: mirror lookup: %w", err)
	}

	if err := p.gcal.Delete(ctx, p.calendarID, d.GoogleEventID); err != nil && !errors.Is(err, gcal.ErrAlreadyGone) {
		return fmt.Errorf("calendar: google delete: %w", err)
	}
	// On ErrAlreadyGone the event is already absent at Google (crash-window backstop);
	// fall through to finish the mirror cleanup so the postcondition holds.
	if err := p.q.DeleteEvent(ctx, d.GoogleEventID); err != nil {
		return fmt.Errorf("calendar: mirror delete: %w", err)
	}

	p.log.Info("calendar: event deleted",
		slog.String("google_event_id", d.GoogleEventID),
		slog.String("logged_by", d.LoggedBy),
	)
	return nil
}
