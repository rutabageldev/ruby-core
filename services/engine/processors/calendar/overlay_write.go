package calendar

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
)

// handleProviderUpsert applies ruby_home.childcare.provider.upsert: create (no id)
// or update a childcare provider. Local overlay only — never written to Google.
func (p *Processor) handleProviderUpsert(ctx context.Context, evt *schemas.CloudEvent) error {
	var d schemas.ChildcareProviderUpsertData
	if err := decodeData(evt.Data, &d); err != nil {
		p.log.Warn("calendar: bad provider upsert payload", slog.String("error", err.Error()))
		return nil
	}
	if d.DisplayName == "" {
		p.log.Warn("calendar: provider upsert missing display_name")
		return nil
	}

	id, err := uuidOrNew(d.ID)
	if err != nil {
		p.log.Warn("calendar: invalid provider id", slog.String("id", d.ID), slog.String("error", err.Error()))
		return nil
	}

	if err := p.q.UpsertProvider(ctx, &store.UpsertProviderParams{
		ID:           id,
		DisplayName:  d.DisplayName,
		PersonID:     uuidPtr(d.PersonID),
		Relationship: textPtr(d.Relationship),
		Archived:     d.Archived,
	}); err != nil {
		return fmt.Errorf("calendar: upsert provider: %w", err)
	}
	p.log.Info("calendar: provider upserted", slog.String("display_name", d.DisplayName))
	return nil
}

// handleProviderDelete archives a provider (soft-delete preserves frequency history).
func (p *Processor) handleProviderDelete(ctx context.Context, evt *schemas.CloudEvent) error {
	var d schemas.ChildcareProviderDeleteData
	if err := decodeData(evt.Data, &d); err != nil {
		p.log.Warn("calendar: bad provider delete payload", slog.String("error", err.Error()))
		return nil
	}
	id, err := parseUUID(d.ID)
	if err != nil {
		p.log.Warn("calendar: provider delete invalid id", slog.String("id", d.ID))
		return nil
	}
	if err := p.q.ArchiveProvider(ctx, id); err != nil {
		return fmt.Errorf("calendar: archive provider: %w", err)
	}
	p.log.Info("calendar: provider archived", slog.String("id", d.ID))
	return nil
}

// handlePersonUpsert applies ruby_home.directory.person.upsert: create (no id) or update
// a directory person by id (#155 §3). The payload is a full person record — HA owns the
// person model and sends the whole object — so this is a straight upsert, not a merge.
// Local overlay only; never written to Google.
func (p *Processor) handlePersonUpsert(ctx context.Context, evt *schemas.CloudEvent) error {
	var d schemas.DirectoryPersonUpsertData
	if err := decodeData(evt.Data, &d); err != nil {
		p.log.Warn("calendar: bad person upsert payload", slog.String("error", err.Error()))
		return nil
	}
	if d.DisplayName == "" {
		p.log.Warn("calendar: person upsert missing display_name")
		return nil
	}
	id, err := uuidOrNew(d.ID)
	if err != nil {
		p.log.Warn("calendar: invalid person id", slog.String("id", d.ID), slog.String("error", err.Error()))
		return nil
	}
	kind := d.Kind
	if kind == "" {
		kind = "person" // directory_person.kind CHECK default
	}
	if err := p.q.UpsertPerson(ctx, &store.UpsertPersonParams{
		ID:               id,
		DisplayName:      d.DisplayName,
		Kind:             kind,
		HaPersonEntityID: textVal(d.HAPersonEntityID),
		Email:            textVal(d.Email),
		Family:           textVal(d.Family),
		Color:            textVal(d.Color),
		Active:           true,
	}); err != nil {
		return fmt.Errorf("calendar: upsert person: %w", err)
	}
	p.log.Info("calendar: person upserted", slog.String("display_name", d.DisplayName))
	return nil
}

// handlePersonDelete deactivates a person (soft-delete; the row is retained so historical
// event associations still resolve).
func (p *Processor) handlePersonDelete(ctx context.Context, evt *schemas.CloudEvent) error {
	var d schemas.DirectoryPersonDeleteData
	if err := decodeData(evt.Data, &d); err != nil {
		p.log.Warn("calendar: bad person delete payload", slog.String("error", err.Error()))
		return nil
	}
	id, err := parseUUID(d.ID)
	if err != nil {
		p.log.Warn("calendar: person delete invalid id", slog.String("id", d.ID))
		return nil
	}
	if err := p.q.DeactivatePerson(ctx, id); err != nil {
		return fmt.Errorf("calendar: deactivate person: %w", err)
	}
	p.log.Info("calendar: person deactivated", slog.String("id", d.ID))
	return nil
}

// reconcileAssociations syncs an event's subject and childcare overlay rows to match
// the upsert payload. A nil slice/pointer means "not provided" — leave unchanged; an
// empty slice / explicit value means "set to this". Called after the mirror upsert.
func (p *Processor) reconcileAssociations(ctx context.Context, eventID string, d *schemas.CalendarUpsertData) error {
	if d.Subjects != nil {
		if err := p.q.DeleteEventSubjects(ctx, eventID); err != nil {
			return fmt.Errorf("calendar: clear subjects: %w", err)
		}
		for _, pid := range d.Subjects {
			uid, err := parseUUID(pid)
			if err != nil {
				p.log.Warn("calendar: skipping invalid subject person_id", slog.String("person_id", pid))
				continue
			}
			if err := p.q.InsertEventSubject(ctx, &store.InsertEventSubjectParams{GoogleEventID: eventID, PersonID: uid}); err != nil {
				return fmt.Errorf("calendar: insert subject: %w", err)
			}
		}
	}

	if d.Childcare != nil {
		if err := p.q.DeleteEventChildcare(ctx, eventID); err != nil {
			return fmt.Errorf("calendar: clear childcare: %w", err)
		}
		if *d.Childcare != "" {
			uid, err := parseUUID(*d.Childcare)
			if err != nil {
				p.log.Warn("calendar: invalid childcare provider_id", slog.String("provider_id", *d.Childcare))
			} else if err := p.q.InsertEventChildcare(ctx, &store.InsertEventChildcareParams{GoogleEventID: eventID, ProviderID: uid}); err != nil {
				return fmt.Errorf("calendar: insert childcare: %w", err)
			}
		}
	}
	return nil
}

// --- uuid helpers ---

func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

// uuidOrNew parses s, or generates a fresh v4 UUID when s is empty (create).
func uuidOrNew(s string) (pgtype.UUID, error) {
	if s == "" {
		return newUUID()
	}
	return parseUUID(s)
}

func newUUID() (pgtype.UUID, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return pgtype.UUID{}, err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return pgtype.UUID{Bytes: b, Valid: true}, nil
}

// uuidPtr converts an optional string id to a nullable pgtype.UUID.
func uuidPtr(s *string) pgtype.UUID {
	if s == nil || *s == "" {
		return pgtype.UUID{}
	}
	u, err := parseUUID(*s)
	if err != nil {
		return pgtype.UUID{}
	}
	return u
}

func textPtr(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

// textVal maps an optional string field to nullable text — empty becomes SQL NULL.
func textVal(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}
