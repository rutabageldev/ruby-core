//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
)

// startPostgres spins up a Postgres testcontainer, runs the calendar migrations against it,
// and returns a connected pool. Container + pool are cleaned up via t.Cleanup. Mirrors the
// NATS harness in pkg/natsx/consumer_integration_test.go (#127).
func startPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("ruby_core_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("startPostgres: run container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("startPostgres: terminate: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("startPostgres: connection string: %v", err)
	}
	if err := store.MigrateUp(ctx, dsn); err != nil {
		t.Fatalf("startPostgres: migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("startPostgres: pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func mkUUID(b byte) pgtype.UUID    { return pgtype.UUID{Bytes: [16]byte{b}, Valid: true} }
func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

// insertEvent inserts a minimal timed calendar_event via raw SQL (the sqlc UpsertEvent has
// 24 params; raw keeps the test readable and exercises the real CHECKs).
func insertEvent(t *testing.T, pool *pgxpool.Pool, id string, start, end time.Time, recurrence []string, recurringEventID *string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO calendar_event
		   (google_event_id, start_datetime, end_datetime, all_day, start_utc, end_utc,
		    calendar_id, etag, raw, recurrence, recurring_event_id)
		 VALUES ($1,$2,$3,false,$2,$3,'cal','e','{}'::jsonb,$4,$5)`,
		id, start, end, recurrence, recurringEventID)
	if err != nil {
		t.Fatalf("insertEvent %s: %v", id, err)
	}
}

func TestSchemaConstraints_Integration(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	q := store.New(pool)

	t.Run("start date-XOR rejects both date and datetime", func(t *testing.T) {
		_, err := pool.Exec(ctx,
			`INSERT INTO calendar_event
			   (google_event_id, start_date, start_datetime, end_datetime, all_day, start_utc, end_utc, calendar_id, etag, raw)
			 VALUES ('xor', '2026-06-30', now(), now(), false, now(), now(), 'cal', 'e', '{}'::jsonb)`)
		if err == nil {
			t.Fatal("expected the start date-XOR CHECK to reject both start_date and start_datetime")
		}
	})

	t.Run("status CHECK rejects an unknown status", func(t *testing.T) {
		_, err := pool.Exec(ctx,
			`INSERT INTO calendar_event
			   (google_event_id, start_datetime, end_datetime, all_day, start_utc, end_utc, calendar_id, status, etag, raw)
			 VALUES ('bad-status', now(), now(), false, now(), now(), 'cal', 'bogus', 'e', '{}'::jsonb)`)
		if err == nil {
			t.Fatal("expected the status CHECK to reject 'bogus'")
		}
	})

	t.Run("relationship CHECK (#134)", func(t *testing.T) {
		bad := q.UpsertProvider(ctx, &store.UpsertProviderParams{
			ID: mkUUID(1), DisplayName: "X", Relationship: pgtype.Text{String: "bogus", Valid: true},
		})
		if bad == nil {
			t.Error("expected relationship='bogus' to be rejected")
		}
		if err := q.UpsertProvider(ctx, &store.UpsertProviderParams{
			ID: mkUUID(2), DisplayName: "Nanny", Relationship: pgtype.Text{String: "nanny", Valid: true},
		}); err != nil {
			t.Errorf("relationship='nanny' should be accepted: %v", err)
		}
		if err := q.UpsertProvider(ctx, &store.UpsertProviderParams{
			ID: mkUUID(3), DisplayName: "Unknown", // relationship NULL
		}); err != nil {
			t.Errorf("relationship NULL should be accepted: %v", err)
		}
	})

	t.Run("person_email unique on lower(email) (#133)", func(t *testing.T) {
		if err := q.UpsertPerson(ctx, &store.UpsertPersonParams{
			ID: mkUUID(4), DisplayName: "Mom", Kind: "person", Active: true,
		}); err != nil {
			t.Fatalf("upsert person: %v", err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO person_email (person_id, email) VALUES ($1,$2)`,
			mkUUID(4), "alias@example.com"); err != nil {
			t.Fatalf("first alias insert: %v", err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO person_email (person_id, email) VALUES ($1,$2)`,
			mkUUID(4), "ALIAS@example.com"); err == nil {
			t.Error("expected case-insensitive unique index to reject a duplicate alias")
		}
	})
}

func TestRangeQueries_Integration(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	q := store.New(pool)

	base := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	master := "master"
	insertEvent(t, pool, "in", base, base.Add(time.Hour), nil, nil)               // single, in range
	insertEvent(t, pool, "out", base.Add(72*time.Hour), base.Add(73*time.Hour), nil, nil) // single, out of range
	insertEvent(t, pool, master, base, base.Add(time.Hour), []string{"RRULE:FREQ=WEEKLY"}, nil)
	insertEvent(t, pool, "override", base, base.Add(time.Hour), nil, &master)

	singles, err := q.ListSingleEventsInRange(ctx, &store.ListSingleEventsInRangeParams{
		RangeStart: ts(base.Add(-time.Hour)), RangeEnd: ts(base.Add(2 * time.Hour)),
	})
	if err != nil {
		t.Fatalf("ListSingleEventsInRange: %v", err)
	}
	if len(singles) != 1 || singles[0].GoogleEventID != "in" {
		t.Errorf("range singles = %v, want [in]", ids(singles))
	}

	masters, err := q.ListRecurringMasters(ctx)
	if err != nil {
		t.Fatalf("ListRecurringMasters: %v", err)
	}
	if len(masters) != 1 || masters[0].GoogleEventID != master {
		t.Errorf("masters = %v, want [master]", ids(masters))
	}

	overrides, err := q.ListOverrides(ctx)
	if err != nil {
		t.Fatalf("ListOverrides: %v", err)
	}
	if len(overrides) != 1 || overrides[0].GoogleEventID != "override" {
		t.Errorf("overrides = %v, want [override]", ids(overrides))
	}
}

func TestOverlayCascade_Integration(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	q := store.New(pool)

	base := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	insertEvent(t, pool, "g1", base, base.Add(time.Hour), nil, nil)
	if err := q.UpsertPerson(ctx, &store.UpsertPersonParams{ID: mkUUID(1), DisplayName: "Kid", Kind: "person", Active: true}); err != nil {
		t.Fatalf("upsert person: %v", err)
	}
	if err := q.UpsertProvider(ctx, &store.UpsertProviderParams{ID: mkUUID(2), DisplayName: "Nanny", Relationship: pgtype.Text{String: "nanny", Valid: true}}); err != nil {
		t.Fatalf("upsert provider: %v", err)
	}
	if err := q.InsertEventSubject(ctx, &store.InsertEventSubjectParams{GoogleEventID: "g1", PersonID: mkUUID(1)}); err != nil {
		t.Fatalf("insert subject: %v", err)
	}
	if err := q.InsertEventChildcare(ctx, &store.InsertEventChildcareParams{GoogleEventID: "g1", ProviderID: mkUUID(2)}); err != nil {
		t.Fatalf("insert childcare: %v", err)
	}

	if err := q.DeleteEvent(ctx, "g1"); err != nil {
		t.Fatalf("delete event: %v", err)
	}

	subs, _ := q.ListSubjectsForEvents(ctx, []string{"g1"})
	if len(subs) != 0 {
		t.Errorf("event_subject did not cascade: %d rows remain", len(subs))
	}
	cc, _ := q.ListChildcareForEvents(ctx, []string{"g1"})
	if len(cc) != 0 {
		t.Errorf("event_childcare did not cascade: %d rows remain", len(cc))
	}
}

func TestPersonEmailAlias_Integration(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	q := store.New(pool)

	if err := q.UpsertPerson(ctx, &store.UpsertPersonParams{
		ID: mkUUID(1), DisplayName: "Dad", Kind: "person",
		Email: pgtype.Text{String: "dad@example.com", Valid: true}, Active: true,
	}); err != nil {
		t.Fatalf("upsert person: %v", err)
	}
	if err := q.UpsertPersonEmail(ctx, &store.UpsertPersonEmailParams{PersonID: mkUUID(1), Email: "dad.alias@example.com"}); err != nil {
		t.Fatalf("upsert person email: %v", err)
	}

	aliases, err := q.ListAllPersonEmails(ctx)
	if err != nil {
		t.Fatalf("ListAllPersonEmails: %v", err)
	}
	if len(aliases) != 1 || aliases[0].Email != "dad.alias@example.com" {
		t.Errorf("aliases = %+v, want [dad.alias@example.com]", aliases)
	}
}

func ids(rows []*store.CalendarEvent) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.GoogleEventID
	}
	return out
}
