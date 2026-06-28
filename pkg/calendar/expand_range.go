package calendar

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/primaryrutabaga/ruby-core/pkg/calendar/expand"
	"github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
)

// RangeReader is the read surface ExpandRange needs. Both store.Queries and the
// calendar processor's store interface satisfy it.
type RangeReader interface {
	ListSingleEventsInRange(ctx context.Context, arg *store.ListSingleEventsInRangeParams) ([]*store.CalendarEvent, error)
	ListRecurringMasters(ctx context.Context) ([]*store.CalendarEvent, error)
	ListOverrides(ctx context.Context) ([]*store.CalendarEvent, error)
}

// ExpandRange loads the events overlapping [from, to) and returns the sorted,
// timezone-aware expanded instances plus a lookup of each source row by id. Shared
// by the read endpoint and the engine reminders so expansion logic lives in one place.
func ExpandRange(ctx context.Context, r RangeReader, from, to time.Time) ([]expand.Instance, map[string]*store.CalendarEvent, error) {
	singles, err := r.ListSingleEventsInRange(ctx, &store.ListSingleEventsInRangeParams{
		RangeStart: pgtype.Timestamptz{Time: from, Valid: true},
		RangeEnd:   pgtype.Timestamptz{Time: to, Valid: true},
	})
	if err != nil {
		return nil, nil, err
	}
	masters, err := r.ListRecurringMasters(ctx)
	if err != nil {
		return nil, nil, err
	}
	overrides, err := r.ListOverrides(ctx)
	if err != nil {
		return nil, nil, err
	}

	rowsByID := make(map[string]*store.CalendarEvent)
	ovByMaster := make(map[string][]expand.Override)
	for _, o := range overrides {
		rowsByID[o.GoogleEventID] = o
		if o.RecurringEventID.Valid {
			ovByMaster[o.RecurringEventID.String] = append(ovByMaster[o.RecurringEventID.String], RowToOverride(o))
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
		ins, err := expand.Expand(RowToEvent(sg), nil, from, to)
		if err != nil {
			return nil, nil, err
		}
		instances = append(instances, ins...)
	}
	for _, m := range masters {
		ins, err := expand.Expand(RowToEvent(m), ovByMaster[m.GoogleEventID], from, to)
		if err != nil {
			return nil, nil, err
		}
		instances = append(instances, ins...)
	}

	sort.Slice(instances, func(i, j int) bool { return instances[i].Start.Before(instances[j].Start) })
	return instances, rowsByID, nil
}
