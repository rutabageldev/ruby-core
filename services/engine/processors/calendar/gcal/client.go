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
	// Insert creates an event and returns it with Google's assigned id and etag.
	Insert(ctx context.Context, calendarID string, ev *calendarv3.Event) (*calendarv3.Event, error)
	// Update modifies an event with If-Match optimistic concurrency. A Google 412
	// (etag mismatch) is surfaced as ErrConflict.
	Update(ctx context.Context, calendarID, eventID, etag string, ev *calendarv3.Event) (*calendarv3.Event, error)
	// Delete removes an event (series-level for MVP).
	Delete(ctx context.Context, calendarID, eventID string) error
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

func (a *apiService) Insert(ctx context.Context, calendarID string, ev *calendarv3.Event) (*calendarv3.Event, error) {
	out, err := a.svc.Events.Insert(calendarID, ev).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("gcal: insert: %w", err)
	}
	return out, nil
}

func (a *apiService) Update(ctx context.Context, calendarID, eventID, etag string, ev *calendarv3.Event) (*calendarv3.Event, error) {
	call := a.svc.Events.Update(calendarID, eventID, ev)
	if etag != "" {
		call.Header().Set("If-Match", etag)
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

func (a *apiService) Delete(ctx context.Context, calendarID, eventID string) error {
	if err := a.svc.Events.Delete(calendarID, eventID).Context(ctx).Do(); err != nil {
		return fmt.Errorf("gcal: delete: %w", err)
	}
	return nil
}
