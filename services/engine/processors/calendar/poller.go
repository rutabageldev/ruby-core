package calendar

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	calendarv3 "google.golang.org/api/calendar/v3"

	"github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
)

// runPoller drives incremental sync on a timer until ctx is cancelled. A future
// Google watch/push can replace the timer by calling syncOnce on the same path
// (ADR-0042).
func (p *Processor) runPoller(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	p.syncOnce(ctx) // sync immediately on startup
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.syncOnce(ctx)
		}
	}
}

// syncOnce runs one incremental sync pass: page through changes since the stored
// sync token, upsert each into the mirror, and persist the new token. A 410 (token
// expired) triggers exactly one full resync from scratch.
func (p *Processor) syncOnce(ctx context.Context) {
	syncToken := p.storedSyncToken(ctx)
	pageToken := ""
	recovered410 := false

	for {
		if ctx.Err() != nil {
			return
		}
		res, err := p.gcal.List(ctx, p.calendarID, syncToken, pageToken)
		if err != nil {
			p.log.Warn("calendar: sync list failed", slog.String("error", err.Error()))
			return
		}

		if res.Expired {
			if recovered410 {
				p.log.Error("calendar: sync token still expired after resync — aborting pass")
				return
			}
			p.log.Warn("calendar: sync token expired (410) — full resync")
			if err := p.q.MarkFullResync(ctx, p.calendarID); err != nil {
				p.log.Warn("calendar: mark full resync failed", slog.String("error", err.Error()))
			}
			syncToken, pageToken, recovered410 = "", "", true
			continue
		}

		for _, ev := range res.Events {
			if err := p.upsertFromGoogle(ctx, ev); err != nil {
				p.log.Warn("calendar: mirror upsert from sync failed",
					slog.String("google_event_id", ev.Id),
					slog.String("error", err.Error()),
				)
			}
		}

		if res.NextPageToken != "" {
			pageToken = res.NextPageToken
			continue
		}
		if res.NextSyncToken != "" {
			if err := p.q.UpsertSyncToken(ctx, &store.UpsertSyncTokenParams{
				CalendarID: p.calendarID,
				SyncToken:  text(res.NextSyncToken),
			}); err != nil {
				p.log.Warn("calendar: persist sync token failed", slog.String("error", err.Error()))
			}
		}
		return
	}
}

// storedSyncToken returns the persisted sync token, or "" for a full sync.
func (p *Processor) storedSyncToken(ctx context.Context) string {
	st, err := p.q.GetSyncState(ctx, p.calendarID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			p.log.Warn("calendar: read sync state failed", slog.String("error", err.Error()))
		}
		return ""
	}
	if st.SyncToken.Valid {
		return st.SyncToken.String
	}
	return ""
}

// upsertFromGoogle mirrors a Google event, skipping the write when the mirror
// already holds the same etag — echo reconciliation that prevents a self-write
// observed by the poller from churning the mirror (ADR-0042).
func (p *Processor) upsertFromGoogle(ctx context.Context, ev *calendarv3.Event) error {
	existing, err := p.q.GetEvent(ctx, ev.Id)
	if err == nil && existing.Etag == trimEtag(ev.Etag) {
		return nil // already mirrored at this etag
	}
	params, err := googleToParams(ev, p.calendarID)
	if err != nil {
		return err
	}
	return p.q.UpsertEvent(ctx, params)
}
