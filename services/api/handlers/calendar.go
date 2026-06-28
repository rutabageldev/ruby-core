package handlers

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"

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

	q := store.New(s.pool)

	singles, err := q.ListSingleEventsInRange(ctx, &store.ListSingleEventsInRangeParams{
		RangeStart: pgtype.Timestamptz{Time: from, Valid: true},
		RangeEnd:   pgtype.Timestamptz{Time: to, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	masters, err := q.ListRecurringMasters(ctx)
	if err != nil {
		return nil, err
	}
	overrides, err := q.ListOverrides(ctx)
	if err != nil {
		return nil, err
	}

	rowsByID := make(map[string]*store.CalendarEvent)
	ovByMaster := make(map[string][]expand.Override)
	for _, o := range overrides {
		rowsByID[o.GoogleEventID] = o
		if o.RecurringEventID.Valid {
			ovByMaster[o.RecurringEventID.String] = append(ovByMaster[o.RecurringEventID.String], rubycal.RowToOverride(o))
		}
	}
	for _, m := range masters {
		rowsByID[m.GoogleEventID] = m
	}
	for _, sg := range singles {
		rowsByID[sg.GoogleEventID] = sg
	}

	var instances []expand.Instance
	for _, sg := range singles {
		ins, err := expand.Expand(rubycal.RowToEvent(sg), nil, from, to)
		if err != nil {
			return nil, err
		}
		instances = append(instances, ins...)
	}
	for _, m := range masters {
		ins, err := expand.Expand(rubycal.RowToEvent(m), ovByMaster[m.GoogleEventID], from, to)
		if err != nil {
			return nil, err
		}
		instances = append(instances, ins...)
	}

	sort.Slice(instances, func(i, j int) bool { return instances[i].Start.Before(instances[j].Start) })

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
