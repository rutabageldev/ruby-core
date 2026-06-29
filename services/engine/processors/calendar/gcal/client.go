// Package gcal wraps the Google Calendar v3 API behind a small Service interface so
// the calendar processor can be unit-tested with a fake — CI never needs live OAuth
// (ADR-0042). The real implementation handles sync-token paging, the 410 (expired
// token) signal, and If-Match optimistic concurrency on updates.
package gcal

import (
	"context"
	"errors"
	"fmt"

	calendarv3 "google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/primaryrutabaga/ruby-core/pkg/boot"
)

// Service is the minimal Google Calendar surface the calendar processor needs.
type Service interface {
	// List returns one page of events for a sync pass. An empty syncToken starts a
	// full page-through; pageToken continues one. A 410 (expired sync token) is
	// surfaced as ListResult.Expired rather than an error so the caller can resync.
	List(ctx context.Context, calendarID, syncToken, pageToken string) (*ListResult, error)
	// Get fetches a single event (used to resync a fresh etag after a 412 conflict).
	Get(ctx context.Context, calendarID, eventID string) (*calendarv3.Event, error)
	// Insert creates an event. When ev.Id is set (a deterministic, client-assigned
	// id derived from the idempotency_key), a redelivered create hits the same id and
	// Google returns 409 — surfaced as ErrDuplicate so the caller can converge the
	// mirror from the existing event instead of double-inserting (ADR-0042).
	Insert(ctx context.Context, calendarID string, ev *calendarv3.Event) (*calendarv3.Event, error)
	// Update modifies an event with If-Match optimistic concurrency. A Google 412
	// (etag mismatch) is surfaced as ErrConflict.
	Update(ctx context.Context, calendarID, eventID, etag string, ev *calendarv3.Event) (*calendarv3.Event, error)
	// Patch partially updates an event (Google events.patch): fields absent from ev are
	// left unchanged, so a caller that omits untouched fields (e.g. recurrence) does not
	// strip them — unlike Update's full replace (ADR-0044). Same If-Match etag concurrency
	// as Update; a Google 412 is surfaced as ErrConflict.
	Patch(ctx context.Context, calendarID, eventID, etag string, ev *calendarv3.Event) (*calendarv3.Event, error)
	// Delete removes an event (series-level for MVP). A Google 410/404 (already gone)
	// is surfaced as ErrAlreadyGone so the caller can treat the absent postcondition
	// as satisfied rather than failing.
	Delete(ctx context.Context, calendarID, eventID string) error
	// InstanceAt resolves the single occurrence of recurringEventID whose original start
	// equals originalStart (RFC 3339). It returns the instance's own event id (which
	// per-instance edits/deletes then Patch), or ErrAlreadyGone when no such occurrence
	// exists (ADR-0044).
	InstanceAt(ctx context.Context, calendarID, recurringEventID, originalStart string) (*calendarv3.Event, error)
}

// ListResult is one page from a sync pass.
type ListResult struct {
	Events        []*calendarv3.Event
	NextPageToken string // non-empty => more pages this pass
	NextSyncToken string // non-empty => pass complete; persist for the next incremental sync
	Expired       bool   // true when Google returned 410 (sync token invalid)
}

// ErrConflict is returned by Update when Google rejects the If-Match etag (HTTP 412).
var ErrConflict = errors.New("gcal: etag conflict (412)")

// ErrDuplicate is returned by Insert when a client-assigned event id already exists
// (HTTP 409) — i.e. a redelivered create. The caller fetches the existing event and
// converges the mirror rather than creating a second event.
var ErrDuplicate = errors.New("gcal: duplicate event id (409)")

// ErrAlreadyGone is returned by Delete when the event is already absent (HTTP 410 or
// 404) — a redelivered or externally-applied delete. The "event absent" postcondition
// is already satisfied.
var ErrAlreadyGone = errors.New("gcal: event already absent (404/410)")

type apiService struct {
	svc *calendarv3.Service
}

// NewService builds a Google Calendar client authenticated by the Vault-stored
// offline refresh token (auto-refreshing).
func NewService(ctx context.Context, cfg *boot.GoogleConfig) (Service, error) {
	svc, err := calendarv3.NewService(ctx, option.WithTokenSource(TokenSource(ctx, cfg)))
	if err != nil {
		return nil, fmt.Errorf("gcal: new service: %w", err)
	}
	return &apiService{svc: svc}, nil
}

func (a *apiService) List(ctx context.Context, calendarID, syncToken, pageToken string) (*ListResult, error) {
	// SingleEvents(false): return recurring masters plus their override/cancelled
	// children (we store and expand them ourselves). ShowDeleted(true): include
	// cancelled events so deletions propagate to the mirror.
	call := a.svc.Events.List(calendarID).ShowDeleted(true).SingleEvents(false)
	if syncToken != "" {
		call = call.SyncToken(syncToken)
	}
	if pageToken != "" {
		call = call.PageToken(pageToken)
	}

	res, err := call.Context(ctx).Do()
	if err != nil {
		var gerr *googleapi.Error
		if errors.As(err, &gerr) && gerr.Code == 410 {
			return &ListResult{Expired: true}, nil
		}
		return nil, fmt.Errorf("gcal: list: %w", err)
	}
	return &ListResult{
		Events:        res.Items,
		NextPageToken: res.NextPageToken,
		NextSyncToken: res.NextSyncToken,
	}, nil
}

func (a *apiService) Get(ctx context.Context, calendarID, eventID string) (*calendarv3.Event, error) {
	out, err := a.svc.Events.Get(calendarID, eventID).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("gcal: get: %w", err)
	}
	return out, nil
}

func (a *apiService) Insert(ctx context.Context, calendarID string, ev *calendarv3.Event) (*calendarv3.Event, error) {
	out, err := a.svc.Events.Insert(calendarID, ev).Context(ctx).Do()
	if err != nil {
		var gerr *googleapi.Error
		if errors.As(err, &gerr) && gerr.Code == 409 {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("gcal: insert: %w", err)
	}
	return out, nil
}

// quoteEtag wraps a bare etag in the double-quotes an HTTP If-Match entity-tag requires.
// The mirror stores etags trimmed of Google's surrounding quotes (see trimEtag in the
// calendar package), but If-Match comparison needs the quoted form — a bare value always
// yields 412. Idempotent: an already-quoted value is returned unchanged.
func quoteEtag(etag string) string {
	if len(etag) >= 2 && etag[0] == '"' && etag[len(etag)-1] == '"' {
		return etag
	}
	return `"` + etag + `"`
}

func (a *apiService) Update(ctx context.Context, calendarID, eventID, etag string, ev *calendarv3.Event) (*calendarv3.Event, error) {
	call := a.svc.Events.Update(calendarID, eventID, ev)
	if etag != "" {
		// If-Match requires the quoted entity-tag form. The mirror stores etags
		// trimmed (trimEtag), so a bare value never matches → Google 412. Re-add the
		// quotes here.
		call.Header().Set("If-Match", quoteEtag(etag))
	}
	out, err := call.Context(ctx).Do()
	if err != nil {
		var gerr *googleapi.Error
		if errors.As(err, &gerr) && gerr.Code == 412 {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("gcal: update: %w", err)
	}
	return out, nil
}

func (a *apiService) Patch(ctx context.Context, calendarID, eventID, etag string, ev *calendarv3.Event) (*calendarv3.Event, error) {
	call := a.svc.Events.Patch(calendarID, eventID, ev)
	if etag != "" {
		// If-Match requires the quoted entity-tag form (see Update); the mirror stores
		// etags trimmed, so a bare value never matches → Google 412.
		call.Header().Set("If-Match", quoteEtag(etag))
	}
	out, err := call.Context(ctx).Do()
	if err != nil {
		var gerr *googleapi.Error
		if errors.As(err, &gerr) && gerr.Code == 412 {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("gcal: patch: %w", err)
	}
	return out, nil
}

func (a *apiService) InstanceAt(ctx context.Context, calendarID, recurringEventID, originalStart string) (*calendarv3.Event, error) {
	res, err := a.svc.Events.Instances(calendarID, recurringEventID).OriginalStart(originalStart).Context(ctx).Do()
	if err != nil {
		var gerr *googleapi.Error
		if errors.As(err, &gerr) && (gerr.Code == 404 || gerr.Code == 410) {
			return nil, ErrAlreadyGone
		}
		return nil, fmt.Errorf("gcal: instances: %w", err)
	}
	if len(res.Items) == 0 {
		return nil, ErrAlreadyGone
	}
	return res.Items[0], nil
}

func (a *apiService) Delete(ctx context.Context, calendarID, eventID string) error {
	if err := a.svc.Events.Delete(calendarID, eventID).Context(ctx).Do(); err != nil {
		var gerr *googleapi.Error
		if errors.As(err, &gerr) && (gerr.Code == 410 || gerr.Code == 404) {
			return ErrAlreadyGone
		}
		return fmt.Errorf("gcal: delete: %w", err)
	}
	return nil
}
