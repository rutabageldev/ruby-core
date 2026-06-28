package handlers

import (
	"context"

	rubycal "github.com/primaryrutabaga/ruby-core/pkg/calendar"
	"github.com/primaryrutabaga/ruby-core/pkg/calendar/expand"
	"github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
	"github.com/primaryrutabaga/ruby-core/services/api/oas"
)

// ListCalendarEvents returns the flat, sorted, timezone-aware expansion of calendar
// events overlapping [start, end). Single events are returned directly; recurring
// series are expanded with EXDATEs and cancelled occurrences subtracted and modified
// occurrences applied. The window is guarded (ADR-0042).
func (s *Service) ListCalendarEvents(ctx context.Context, params oas.ListCalendarEventsParams) (oas.ListCalendarEventsRes, error) {
	from := params.Start.UTC()
	to := params.End.UTC()
	if err := expand.CheckWindow(from, to); err != nil {
		return nil, badRequest(err.Error())
	}

	instances, rowsByID, err := rubycal.ExpandRange(ctx, store.New(s.pool), from, to)
	if err != nil {
		return nil, err
	}

	out := make(oas.ListCalendarEventsOKApplicationJSON, 0, len(instances))
	for _, in := range instances {
		out = append(out, toAPIInstance(in, rowsByID[in.GoogleEventID]))
	}
	return &out, nil
}

// toAPIInstance builds the API shape from an expanded instance plus its source row.
// subjects/childcare are populated by the household-overlay slice; here subjects is
// an empty list and childcare is omitted.
func toAPIInstance(in expand.Instance, row *store.CalendarEvent) oas.CalendarInstance {
	ci := oas.CalendarInstance{
		GoogleEventID: in.GoogleEventID,
		Start:         in.Start,
		End:           in.End,
		AllDay:        in.AllDay,
		Status:        "confirmed",
		Subjects:      []string{},
	}
	if row != nil {
		ci.Status = row.Status
		if row.Summary.Valid {
			ci.Summary = oas.NewOptString(row.Summary.String)
		}
		if row.Location.Valid {
			ci.Location = oas.NewOptString(row.Location.String)
		}
		if row.Description.Valid {
			ci.Description = oas.NewOptString(row.Description.String)
		}
	}
	return ci
}
