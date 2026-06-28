//go:build fast

package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	calendarv3 "google.golang.org/api/calendar/v3"

	"github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/calendar/gcal"
)

// --- fakes ---

type fakeGCal struct {
	events            map[string]*calendarv3.Event
	inserts, updates  int
	deletes, gets     int
	nextID            int
	failNextUpdate412 bool
	listResults       []*gcal.ListResult
	listIdx           int
}

func newFakeGCal() *fakeGCal { return &fakeGCal{events: map[string]*calendarv3.Event{}} }

func (f *fakeGCal) Get(_ context.Context, _, id string) (*calendarv3.Event, error) {
	f.gets++
	ev, ok := f.events[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return ev, nil
}

func (f *fakeGCal) Insert(_ context.Context, _ string, ev *calendarv3.Event) (*calendarv3.Event, error) {
	f.inserts++
	f.nextID++
	out := *ev
	out.Id = fmt.Sprintf("g%d", f.nextID)
	out.Etag = `"etag-` + out.Id + `"`
	f.events[out.Id] = &out
	return &out, nil
}

func (f *fakeGCal) Update(_ context.Context, _, id, _ string, ev *calendarv3.Event) (*calendarv3.Event, error) {
	if f.failNextUpdate412 {
		f.failNextUpdate412 = false
		return nil, gcal.ErrConflict
	}
	f.updates++
	out := *ev
	out.Id = id
	out.Etag = `"etag-upd"`
	f.events[id] = &out
	return &out, nil
}

func (f *fakeGCal) Delete(_ context.Context, _, id string) error {
	f.deletes++
	delete(f.events, id)
	return nil
}

func (f *fakeGCal) List(_ context.Context, _, _, _ string) (*gcal.ListResult, error) {
	if f.listIdx >= len(f.listResults) {
		return &gcal.ListResult{NextSyncToken: "tok-final"}, nil
	}
	r := f.listResults[f.listIdx]
	f.listIdx++
	return r, nil
}

type fakeStore struct {
	events      map[string]*store.CalendarEvent
	sync        *store.SyncState
	upserts     int
	fullResyncs int
}

func newFakeStore() *fakeStore {
	return &fakeStore{events: map[string]*store.CalendarEvent{}}
}

func (s *fakeStore) UpsertEvent(_ context.Context, arg *store.UpsertEventParams) error {
	s.upserts++
	s.events[arg.GoogleEventID] = &store.CalendarEvent{
		GoogleEventID: arg.GoogleEventID, Etag: arg.Etag, Status: arg.Status,
	}
	return nil
}

func (s *fakeStore) GetEvent(_ context.Context, id string) (*store.CalendarEvent, error) {
	ev, ok := s.events[id]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return ev, nil
}

func (s *fakeStore) DeleteEvent(_ context.Context, id string) error {
	delete(s.events, id)
	return nil
}

func (s *fakeStore) GetSyncState(_ context.Context, _ string) (*store.SyncState, error) {
	if s.sync == nil {
		return nil, pgx.ErrNoRows
	}
	return s.sync, nil
}

func (s *fakeStore) UpsertSyncToken(_ context.Context, arg *store.UpsertSyncTokenParams) error {
	s.sync = &store.SyncState{CalendarID: arg.CalendarID, SyncToken: arg.SyncToken}
	return nil
}

func (s *fakeStore) MarkFullResync(_ context.Context, calID string) error {
	s.fullResyncs++
	s.sync = &store.SyncState{CalendarID: calID}
	return nil
}

type fakeIDStore struct{ seen map[string]bool }

func (f *fakeIDStore) Seen(id string) (bool, error) { return f.seen[id], nil }
func (f *fakeIDStore) Mark(id string) error         { f.seen[id] = true; return nil }
func (f *fakeIDStore) Close() error                 { return nil }

func newTestProcessor() (*Processor, *fakeGCal, *fakeStore) {
	g := newFakeGCal()
	st := newFakeStore()
	p := &Processor{
		log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		q:           st,
		gcal:        g,
		calendarID:  "cal",
		syncEnabled: true,
		idStore:     &fakeIDStore{seen: map[string]bool{}},
	}
	return p, g, st
}

func timedEvent(t *testing.T) (schemas.CalendarEventDate, schemas.CalendarEventDate) {
	t.Helper()
	return schemas.CalendarEventDate{DateTime: "2026-06-26T09:00:00-04:00", TimeZone: "America/New_York"},
		schemas.CalendarEventDate{DateTime: "2026-06-26T10:00:00-04:00", TimeZone: "America/New_York"}
}

func cloudEvent(t *testing.T, payload any) *schemas.CloudEvent {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	return &schemas.CloudEvent{Data: m}
}

// --- tests ---

func TestHandleUpsert_CreateDedupesOnIdempotencyKey(t *testing.T) {
	p, g, st := newTestProcessor()
	start, end := timedEvent(t)
	evt := cloudEvent(t, schemas.CalendarUpsertData{
		Summary: "Dentist", Start: start, End: end,
		IdempotencyKey: "k1", LoggedBy: "michael",
	})

	if err := p.handleUpsert(context.Background(), evt); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if g.inserts != 1 {
		t.Fatalf("inserts = %d, want 1", g.inserts)
	}
	if len(st.events) != 1 {
		t.Fatalf("mirror has %d events, want 1", len(st.events))
	}

	// Redelivered create with the same idempotency key: no second Google insert.
	if err := p.handleUpsert(context.Background(), evt); err != nil {
		t.Fatalf("duplicate create: %v", err)
	}
	if g.inserts != 1 {
		t.Errorf("duplicate create caused %d inserts, want 1", g.inserts)
	}
}

func TestHandleUpsert_Update412ResyncsAndRetries(t *testing.T) {
	p, g, st := newTestProcessor()
	start, end := timedEvent(t)
	// Seed the existing Google event + mirror row.
	g.events["g1"] = &calendarv3.Event{
		Id: "g1", Etag: `"fresh"`,
		Start: &calendarv3.EventDateTime{DateTime: "2026-06-26T09:00:00-04:00"},
		End:   &calendarv3.EventDateTime{DateTime: "2026-06-26T10:00:00-04:00"},
	}
	st.events["g1"] = &store.CalendarEvent{GoogleEventID: "g1", Etag: "stale"}
	g.failNextUpdate412 = true

	evt := cloudEvent(t, schemas.CalendarUpsertData{
		GoogleEventID: "g1", Summary: "Moved", Start: start, End: end, Etag: "stale",
	})
	if err := p.handleUpsert(context.Background(), evt); err != nil {
		t.Fatalf("update with 412: %v", err)
	}
	if g.gets != 1 {
		t.Errorf("expected 1 resync Get after 412, got %d", g.gets)
	}
	if g.updates != 1 {
		t.Errorf("expected 1 successful update after retry, got %d", g.updates)
	}
}

func TestHandleDelete_RemovesGoogleAndMirror(t *testing.T) {
	p, g, st := newTestProcessor()
	g.events["g1"] = &calendarv3.Event{Id: "g1"}
	st.events["g1"] = &store.CalendarEvent{GoogleEventID: "g1"}

	evt := cloudEvent(t, schemas.CalendarDeleteData{GoogleEventID: "g1", LoggedBy: "katie"})
	if err := p.handleDelete(context.Background(), evt); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if g.deletes != 1 {
		t.Errorf("google deletes = %d, want 1", g.deletes)
	}
	if _, ok := st.events["g1"]; ok {
		t.Error("mirror row should have been deleted")
	}
}

func TestSyncOnce_UpsertsAndPersistsToken(t *testing.T) {
	p, g, st := newTestProcessor()
	g.listResults = []*gcal.ListResult{{
		Events:        []*calendarv3.Event{googleTimed("g1", `"e1"`)},
		NextSyncToken: "tok2",
	}}
	p.syncOnce(context.Background())

	if _, ok := st.events["g1"]; !ok {
		t.Error("synced event not mirrored")
	}
	if st.sync == nil || st.sync.SyncToken.String != "tok2" {
		t.Errorf("sync token not persisted: %+v", st.sync)
	}
}

func TestSyncOnce_EchoSkipsSameEtag(t *testing.T) {
	p, g, st := newTestProcessor()
	st.events["g1"] = &store.CalendarEvent{GoogleEventID: "g1", Etag: "e1"} // already mirrored
	g.listResults = []*gcal.ListResult{{
		Events:        []*calendarv3.Event{googleTimed("g1", `"e1"`)},
		NextSyncToken: "tok2",
	}}
	p.syncOnce(context.Background())

	if st.upserts != 0 {
		t.Errorf("echo of same etag should not upsert, got %d upserts", st.upserts)
	}
}

func TestSyncOnce_410TriggersFullResync(t *testing.T) {
	p, g, st := newTestProcessor()
	st.sync = &store.SyncState{CalendarID: "cal", SyncToken: pgtype.Text{String: "expired", Valid: true}}
	g.listResults = []*gcal.ListResult{
		{Expired: true},
		{Events: []*calendarv3.Event{googleTimed("g1", `"e1"`)}, NextSyncToken: "tok3"},
	}
	p.syncOnce(context.Background())

	if st.fullResyncs != 1 {
		t.Errorf("full resyncs = %d, want 1", st.fullResyncs)
	}
	if _, ok := st.events["g1"]; !ok {
		t.Error("event after resync not mirrored")
	}
	if st.sync == nil || st.sync.SyncToken.String != "tok3" {
		t.Errorf("fresh token not persisted after resync: %+v", st.sync)
	}
}

func googleTimed(id, etag string) *calendarv3.Event {
	return &calendarv3.Event{
		Id: id, Etag: etag, Status: "confirmed",
		Start: &calendarv3.EventDateTime{DateTime: "2026-06-26T09:00:00-04:00"},
		End:   &calendarv3.EventDateTime{DateTime: "2026-06-26T10:00:00-04:00"},
	}
}
