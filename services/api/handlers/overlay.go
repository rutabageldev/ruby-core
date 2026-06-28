package handlers

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	rubycal "github.com/primaryrutabaga/ruby-core/pkg/calendar"
	"github.com/primaryrutabaga/ruby-core/pkg/calendar/expand"
	"github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
	"github.com/primaryrutabaga/ruby-core/services/api/oas"
)

// ListDirectoryPeople returns the active household directory.
func (s *Service) ListDirectoryPeople(ctx context.Context) (oas.ListDirectoryPeopleRes, error) {
	rows, err := store.New(s.pool).ListActivePeople(ctx)
	if err != nil {
		return nil, err
	}
	out := make(oas.ListDirectoryPeopleOKApplicationJSON, 0, len(rows))
	for _, r := range rows {
		p := oas.Person{ID: uuidStr(r.ID), DisplayName: r.DisplayName, Kind: r.Kind, Active: r.Active}
		if r.HaPersonEntityID.Valid {
			p.HaPersonEntityID = oas.NewOptString(r.HaPersonEntityID.String)
		}
		if r.Email.Valid {
			p.Email = oas.NewOptString(r.Email.String)
		}
		if r.Family.Valid {
			p.Family = oas.NewOptString(r.Family.String)
		}
		if r.Color.Valid {
			p.Color = oas.NewOptString(r.Color.String)
		}
		out = append(out, p)
	}
	return &out, nil
}

// ListChildcareProviders returns the active (non-archived) provider roster.
func (s *Service) ListChildcareProviders(ctx context.Context) (oas.ListChildcareProvidersRes, error) {
	rows, err := store.New(s.pool).ListActiveProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := make(oas.ListChildcareProvidersOKApplicationJSON, 0, len(rows))
	for _, r := range rows {
		p := oas.Provider{ID: uuidStr(r.ID), DisplayName: r.DisplayName, Archived: r.Archived}
		if r.PersonID.Valid {
			p.PersonID = oas.NewOptString(uuidStr(r.PersonID))
		}
		if r.Relationship.Valid {
			p.Relationship = oas.NewOptString(r.Relationship.String)
		}
		out = append(out, p)
	}
	return &out, nil
}

// ListChildcareProviderSuggestions ranks non-archived providers by recency-weighted
// per-occurrence usage, computed from associations + expansion (nothing stored).
func (s *Service) ListChildcareProviderSuggestions(ctx context.Context) (oas.ListChildcareProviderSuggestionsRes, error) {
	q := store.New(s.pool)
	providers, err := q.ListActiveProviders(ctx)
	if err != nil {
		return nil, err
	}
	assoc, err := q.ListProviderEvents(ctx)
	if err != nil {
		return nil, err
	}

	providerEvents := make(map[string][]expand.Event)
	for _, r := range assoc {
		pid := uuidStr(r.ProviderID)
		providerEvents[pid] = append(providerEvents[pid], providerRowToEvent(r))
	}
	scores := rubycal.RankProviderUsage(providerEvents, time.Now().UTC(), rubycal.DefaultSuggestionWindow)

	out := make(oas.ListChildcareProviderSuggestionsOKApplicationJSON, 0, len(providers))
	for _, p := range providers {
		id := uuidStr(p.ID)
		out = append(out, oas.ProviderSuggestion{ID: id, DisplayName: p.DisplayName, Score: scores[id]})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return &out, nil
}

func providerRowToEvent(r *store.ListProviderEventsRow) expand.Event {
	loc := time.UTC
	if r.StartTimezone.Valid && r.StartTimezone.String != "" {
		if l, err := time.LoadLocation(r.StartTimezone.String); err == nil {
			loc = l
		}
	}
	return expand.Event{
		GoogleEventID: r.GoogleEventID,
		Start:         r.StartUtc.Time,
		End:           r.EndUtc.Time,
		Location:      loc,
		AllDay:        r.AllDay,
		Recurrence:    r.Recurrence,
	}
}

// uuidStr formats a pgtype.UUID as the canonical 8-4-4-4-12 string (empty if null).
func uuidStr(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
