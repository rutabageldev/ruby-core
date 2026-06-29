package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"time"

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
	instances, rowsByID, err := rubycal.ExpandRange(ctx, q, from, to)
	if err != nil {
		return nil, err
	}

	// Resolve overlay associations by the series-level event id (the master for
	// recurring instances; the event itself otherwise).
	keys := associationKeys(instances, rowsByID)
	subjectsByEvent, err := loadSubjects(ctx, q, keys)
	if err != nil {
		return nil, err
	}
	childcareByEvent, err := loadChildcare(ctx, q, keys)
	if err != nil {
		return nil, err
	}
	emailToPerson, err := loadEmailIndex(ctx, q)
	if err != nil {
		return nil, err
	}

	out := make(oas.ListCalendarEventsOKApplicationJSON, 0, len(instances))
	for _, in := range instances {
		row := rowsByID[in.GoogleEventID]
		key := associationKey(in, row)
		ci := toAPIInstance(in, row)

		if subs := subjectsByEvent[key]; subs != nil {
			ci.Subjects = subs
		}
		if pid, ok := childcareByEvent[key]; ok {
			ci.Childcare = oas.NewOptString(pid)
		}
		ci.Attendees = resolveAttendees(row, emailToPerson)
		out = append(out, ci)
	}
	return &out, nil
}

// associationKey is the event id the overlay associations are keyed on: the series
// master for recurring/override instances, the event itself otherwise.
func associationKey(in expand.Instance, row *store.CalendarEvent) string {
	if row != nil && row.RecurringEventID.Valid && row.RecurringEventID.String != "" {
		return row.RecurringEventID.String
	}
	return in.GoogleEventID
}

func associationKeys(instances []expand.Instance, rowsByID map[string]*store.CalendarEvent) []string {
	set := make(map[string]struct{}, len(instances))
	for _, in := range instances {
		set[associationKey(in, rowsByID[in.GoogleEventID])] = struct{}{}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	return keys
}

func loadSubjects(ctx context.Context, q *store.Queries, keys []string) (map[string][]string, error) {
	m := make(map[string][]string)
	if len(keys) == 0 {
		return m, nil
	}
	rows, err := q.ListSubjectsForEvents(ctx, keys)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		m[r.GoogleEventID] = append(m[r.GoogleEventID], uuidStr(r.PersonID))
	}
	return m, nil
}

func loadChildcare(ctx context.Context, q *store.Queries, keys []string) (map[string]string, error) {
	m := make(map[string]string)
	if len(keys) == 0 {
		return m, nil
	}
	rows, err := q.ListChildcareForEvents(ctx, keys)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		m[r.GoogleEventID] = uuidStr(r.ProviderID)
	}
	return m, nil
}

func loadEmailIndex(ctx context.Context, q *store.Queries) (map[string]string, error) {
	people, err := q.ListActivePeople(ctx)
	if err != nil {
		return nil, err
	}
	aliases, err := q.ListAllPersonEmails(ctx)
	if err != nil {
		return nil, err
	}
	return buildEmailIndex(people, aliases), nil
}

// buildEmailIndex maps lower(email) -> person_id over active people's primary emails plus
// their alias / secondary addresses (#133). Aliases resolve only for active people, and a
// primary email wins over an alias on collision.
func buildEmailIndex(people []*store.DirectoryPerson, aliases []*store.ListAllPersonEmailsRow) map[string]string {
	m := make(map[string]string, len(people))
	active := make(map[string]struct{}, len(people))
	for _, r := range people {
		id := uuidStr(r.ID)
		active[id] = struct{}{}
		if r.Email.Valid && r.Email.String != "" {
			m[strings.ToLower(r.Email.String)] = id
		}
	}
	for _, a := range aliases {
		id := uuidStr(a.PersonID)
		if _, ok := active[id]; !ok {
			continue
		}
		key := strings.ToLower(a.Email)
		if _, exists := m[key]; !exists {
			m[key] = id
		}
	}
	return m
}

// rawEvent is the slice of the stored Google payload we read for attendees.
type rawEvent struct {
	Attendees []struct {
		Email          string `json:"email"`
		ResponseStatus string `json:"responseStatus"`
	} `json:"attendees"`
}

// resolveAttendees parses the stored Google payload and reconciles each attendee's
// email to a directory person id where matched.
func resolveAttendees(row *store.CalendarEvent, emailToPerson map[string]string) []oas.CalendarInstanceAttendeesItem {
	out := []oas.CalendarInstanceAttendeesItem{}
	if row == nil || len(row.Raw) == 0 {
		return out
	}
	var re rawEvent
	if err := json.Unmarshal(row.Raw, &re); err != nil {
		return out
	}
	for _, a := range re.Attendees {
		if a.Email == "" {
			continue
		}
		item := oas.CalendarInstanceAttendeesItem{Email: a.Email}
		if a.ResponseStatus != "" {
			item.ResponseStatus = oas.NewOptString(a.ResponseStatus)
		}
		if pid, ok := emailToPerson[strings.ToLower(a.Email)]; ok {
			item.PersonID = oas.NewOptString(pid)
		}
		out = append(out, item)
	}
	return out
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
		if row.Etag != "" {
			ci.Etag = oas.NewOptString(row.Etag)
		}
		// recurrence (raw RRULE/EXDATE/RDATE) lives on the series master row.
		if len(row.Recurrence) > 0 {
			ci.Recurrence = row.Recurrence
		}
		// recurring_event_id: explicit on an override row; the master's own id for the
		// occurrences it expands into (so the consumer can address the series).
		switch {
		case row.RecurringEventID.Valid && row.RecurringEventID.String != "":
			ci.RecurringEventID = oas.NewOptString(row.RecurringEventID.String)
		case len(row.Recurrence) > 0:
			ci.RecurringEventID = oas.NewOptString(row.GoogleEventID)
		}
		// original_start identifies the occurrence for per-instance writes (§2): the
		// override's pinned slot, else the occurrence's own scheduled start.
		if os, ok := originalStart(in, row); ok {
			ci.OriginalStart = oas.NewOptDateTime(os)
		}
	}
	return ci
}

// originalStart returns the occurrence's original scheduled start and whether it applies:
// an override's pinned slot (date or datetime), else the scheduled start of a recurring
// occurrence. Single events have no original start.
func originalStart(in expand.Instance, row *store.CalendarEvent) (time.Time, bool) {
	switch {
	case row.OriginalStartDatetime.Valid:
		return row.OriginalStartDatetime.Time.UTC(), true
	case row.OriginalStartDate.Valid:
		return row.OriginalStartDate.Time.UTC(), true
	case len(row.Recurrence) > 0:
		return in.Start, true
	default:
		return time.Time{}, false
	}
}
