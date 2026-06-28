package calendar

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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
		result, err = p.create(ctx, &d, gev)
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

	p.log.Info("calendar: event written",
		slog.String("google_event_id", result.Id),
		slog.String("logged_by", d.LoggedBy),
	)
	return nil
}

func (p *Processor) create(ctx context.Context, d *schemas.CalendarUpsertData, gev *calendarv3.Event) (*calendarv3.Event, error) {
	if d.IdempotencyKey != "" {
		seen, err := p.idStore.Seen(d.IdempotencyKey)
		if err != nil {
			return nil, fmt.Errorf("calendar: idempotency check: %w", err)
		}
		if seen {
			p.log.Info("calendar: duplicate create ignored", slog.String("idempotency_key", d.IdempotencyKey))
			return nil, nil
		}
	}

	out, err := p.gcal.Insert(ctx, p.calendarID, gev)
	if err != nil {
		return nil, fmt.Errorf("calendar: google insert: %w", err)
	}
	if d.IdempotencyKey != "" {
		if err := p.idStore.Mark(d.IdempotencyKey); err != nil {
			p.log.Warn("calendar: idempotency mark failed", slog.String("error", err.Error()))
		}
	}
	return out, nil
}

func (p *Processor) update(ctx context.Context, d *schemas.CalendarUpsertData, gev *calendarv3.Event) (*calendarv3.Event, error) {
	out, err := p.gcal.Update(ctx, p.calendarID, d.GoogleEventID, d.Etag, gev)
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
		out, err = p.gcal.Update(ctx, p.calendarID, d.GoogleEventID, trimEtag(fresh.Etag), gev)
	}
	if err != nil {
		return nil, fmt.Errorf("calendar: google update: %w", err)
	}
	return out, nil
}

// handleDelete applies a calendar.event.delete (series-level for MVP): delete in
// Google, then remove the mirror row. Overlay cascade is wired in Slice D.
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

	if err := p.gcal.Delete(ctx, p.calendarID, d.GoogleEventID); err != nil {
		return fmt.Errorf("calendar: google delete: %w", err)
	}
	if err := p.q.DeleteEvent(ctx, d.GoogleEventID); err != nil {
		return fmt.Errorf("calendar: mirror delete: %w", err)
	}

	p.log.Info("calendar: event deleted",
		slog.String("google_event_id", d.GoogleEventID),
		slog.String("logged_by", d.LoggedBy),
	)
	return nil
}
