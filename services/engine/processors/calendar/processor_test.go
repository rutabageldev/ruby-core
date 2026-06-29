//go:build fast

package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	calendarv3 "google.golang.org/api/calendar/v3"

	"github.com/primaryrutabaga/ruby-core/pkg/calendar/expand"
	"github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
	"github.com/primaryrutabaga/ruby-core/pkg/schemas"
	"github.com/primaryrutabaga/ruby-core/services/engine/processors/calendar/gcal"
)

// --- fakes ---

type fakeGCal struct {
	events            map[string]*calendarv3.Event
	instances         map[string]string // "recurringEventID|originalStart" -> instance event id
	inserts, updates  int
	patches           int
	lastPatch         *calendarv3.Event // event arg of the most recent Patch
	deletes, gets     int
	nextID            int
	failNextUpdate412 bool
	listResults       []*gcal.ListResult
	listIdx           int
}

func newFakeGCal() *fakeGCal {
	return &fakeGCal{events: map[string]*calendarv3.Event{}, instances: map[string]string{}}
}

func (f *fakeGCal) Get(_ context.Context, _, id string) (*calendarv3.Event, error) {
	f.gets++
	ev, ok := f.events[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return ev, nil
}

func (f *fakeGCal) Insert(_ context.Context, _ string, ev *calendarv3.Event) (*calendarv3.Event, error) {
	id := ev.Id // client-assigned (deterministic) id, when set
	if id == "" {
		f.nextID++
		id = fmt.Sprintf("g%d", f.nextID)
	}
	if _, exists := f.events[id]; exists {
		// Google rejects a duplicate client-assigned id with 409 (a redelivered create).
		return nil, gcal.ErrDuplicate
	}
	f.inserts++
	out := *ev
	out.Id = id
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

func (f *fakeGCal) Patch(_ context.Context, _, id, _ string, ev *calendarv3.Event) (*calendarv3.Event, error) {
	if f.failNextUpdate412 {
		f.failNextUpdate412 = false
		return nil, gcal.ErrConflict
	}
	f.patches++
	f.lastPatch = ev
	// Patch merges: preserve fields the caller omitted (nil) from the stored event.
	out := *ev
	if cur, ok := f.events[id]; ok {
		if out.Recurrence == nil {
			out.Recurrence = cur.Recurrence
		}
		if out.Summary == "" {
			out.Summary = cur.Summary
		}
	}
	out.Id = id
	out.Etag = `"etag-patched"`
	f.events[id] = &out
	return &out, nil
}

func (f *fakeGCal) InstanceAt(_ context.Context, _, recurringEventID, originalStart string) (*calendarv3.Event, error) {
	id, ok := f.instances[recurringEventID+"|"+originalStart]
	if !ok {
		return nil, gcal.ErrAlreadyGone
	}
	ev, ok := f.events[id]
	if !ok {
		return nil, gcal.ErrAlreadyGone
	}
	return ev, nil
}

func (f *fakeGCal) Delete(_ context.Context, _, id string) error {
	f.deletes++
	if _, ok := f.events[id]; !ok {
		return gcal.ErrAlreadyGone
	}
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

	providers   []*store.UpsertProviderParams
	archived    []pgtype.UUID
	people      []*store.UpsertPersonParams
	deactivated []pgtype.UUID
	subjects    map[string][]pgtype.UUID
	childcare   map[string]pgtype.UUID
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		events:    map[string]*store.CalendarEvent{},
		subjects:  map[string][]pgtype.UUID{},
		childcare: map[string]pgtype.UUID{},
	}
}

func (s *fakeStore) UpsertProvider(_ context.Context, arg *store.UpsertProviderParams) error {
	s.providers = append(s.providers, arg)
	return nil
}
func (s *fakeStore) ArchiveProvider(_ context.Context, id pgtype.UUID) error {
	s.archived = append(s.archived, id)
	return nil
}
func (s *fakeStore) UpsertPerson(_ context.Context, arg *store.UpsertPersonParams) error {
	s.people = append(s.people, arg)
	return nil
}
func (s *fakeStore) DeactivatePerson(_ context.Context, id pgtype.UUID) error {
	s.deactivated = append(s.deactivated, id)
	return nil
}
func (s *fakeStore) DeleteEventSubjects(_ context.Context, eid string) error {
	delete(s.subjects, eid)
	return nil
}
func (s *fakeStore) InsertEventSubject(_ context.Context, arg *store.InsertEventSubjectParams) error {
	s.subjects[arg.GoogleEventID] = append(s.subjects[arg.GoogleEventID], arg.PersonID)
	return nil
}
func (s *fakeStore) DeleteEventChildcare(_ context.Context, eid string) error {
	delete(s.childcare, eid)
	return nil
}
func (s *fakeStore) InsertEventChildcare(_ context.Context, arg *store.InsertEventChildcareParams) error {
	s.childcare[arg.GoogleEventID] = arg.ProviderID
	return nil
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

// The list methods are unused by the write-through/poller tests (reminder decision
// logic is tested directly via evaluateReminders); return empty sets.
func (s *fakeStore) ListSingleEventsInRange(_ context.Context, _ *store.ListSingleEventsInRangeParams) ([]*store.CalendarEvent, error) {
	return nil, nil
}
func (s *fakeStore) ListRecurringMasters(_ context.Context) ([]*store.CalendarEvent, error) {
	return nil, nil
}
func (s *fakeStore) ListOverrides(_ context.Context) ([]*store.CalendarEvent, error) {
	return nil, nil
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

// A redelivered create derives the same deterministic Google id, so the second Insert
// returns 409; the processor converges the mirror from the existing event instead of
// double-inserting (ADR-0042). Idempotency holds at Google, not via the KV dedup store.
func TestHandleUpsert_CreateIsIdempotentAtGoogle(t *testing.T) {
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
	wantID := deterministicEventID("k1")
	if _, ok := g.events[wantID]; !ok {
		t.Fatalf("event not stored under deterministic id %q; have %v", wantID, keys(g.events))
	}

	// Redelivered create: same deterministic id → 409 → Get + converge, no second insert.
	if err := p.handleUpsert(context.Background(), evt); err != nil {
		t.Fatalf("duplicate create: %v", err)
	}
	if g.inserts != 1 {
		t.Errorf("duplicate create caused %d inserts, want 1", g.inserts)
	}
	if g.gets != 1 {
		t.Errorf("duplicate create should Get the existing event once, got %d", g.gets)
	}
	if len(st.events) != 1 {
		t.Errorf("mirror has %d events after redelivery, want 1", len(st.events))
	}
}

// deriving the id from the seed must be stable and satisfy Google's id rules.
func TestDeterministicEventID(t *testing.T) {
	a := deterministicEventID("k1")
	b := deterministicEventID("k1")
	if a != b {
		t.Fatalf("not stable: %q vs %q", a, b)
	}
	if deterministicEventID("k2") == a {
		t.Error("different seeds produced the same id")
	}
	if len(a) != 32 {
		t.Errorf("len = %d, want 32", len(a))
	}
	for _, r := range a {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'v')) {
			t.Errorf("id %q contains out-of-range char %q (want base32hex a-v/0-9)", a, r)
		}
	}
}

// With no idempotency_key and no CloudEvent id, the create falls back to a
// Google-assigned id (and warns); it must still succeed without panicking.
func TestHandleUpsert_CreateEmptySeedFallsBack(t *testing.T) {
	p, g, st := newTestProcessor()
	start, end := timedEvent(t)
	evt := cloudEvent(t, schemas.CalendarUpsertData{Summary: "No key", Start: start, End: end})
	// evt.ID is empty (cloudEvent sets only Data).
	if err := p.handleUpsert(context.Background(), evt); err != nil {
		t.Fatalf("create with empty seed: %v", err)
	}
	if g.inserts != 1 {
		t.Errorf("inserts = %d, want 1", g.inserts)
	}
	if _, ok := st.events["g1"]; !ok {
		t.Error("expected a Google-assigned id (g1) in the mirror")
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
	if g.patches != 1 {
		t.Errorf("expected 1 successful patch after retry, got %d", g.patches)
	}
}

// TestHandleUpsert_UpdatePreservesOmittedRecurrence is the ADR-0044 §4b guarantee: editing a
// field of a recurring event while omitting recurrence must NOT strip the series. The update
// path goes through events.patch, and the patched event carries no recurrence (so Google
// preserves it) — the legacy events.update full-replace would have cleared it.
func TestHandleUpsert_UpdatePreservesOmittedRecurrence(t *testing.T) {
	p, g, st := newTestProcessor()
	start, end := timedEvent(t)
	rrule := []string{"RRULE:FREQ=WEEKLY;BYDAY=MO"}
	g.events["g1"] = &calendarv3.Event{
		Id: "g1", Etag: `"fresh"`, Summary: "Standup", Recurrence: rrule,
		Start: &calendarv3.EventDateTime{DateTime: "2026-06-22T09:00:00-04:00"},
		End:   &calendarv3.EventDateTime{DateTime: "2026-06-22T10:00:00-04:00"},
	}
	st.events["g1"] = &store.CalendarEvent{GoogleEventID: "g1", Etag: "fresh"}

	// Edit only the summary; recurrence omitted (HA's "untouched" contract).
	evt := cloudEvent(t, schemas.CalendarUpsertData{
		GoogleEventID: "g1", Summary: "Standup (moved)", Start: start, End: end, Etag: "fresh",
	})
	if err := p.handleUpsert(context.Background(), evt); err != nil {
		t.Fatalf("update: %v", err)
	}
	if g.patches != 1 || g.updates != 0 {
		t.Fatalf("expected exactly 1 patch and 0 updates, got patches=%d updates=%d", g.patches, g.updates)
	}
	if len(g.lastPatch.Recurrence) != 0 {
		t.Errorf("patch must omit recurrence so Google preserves it, got %v", g.lastPatch.Recurrence)
	}
	if got := g.events["g1"].Recurrence; len(got) != 1 || got[0] != rrule[0] {
		t.Errorf("series recurrence not preserved after edit: %v", got)
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

// A redelivered delete whose mirror row is already gone is a no-op: the delete already
// completed, so we never call Google (and the 410 never arises).
func TestHandleDelete_SkipsWhenMirrorAbsent(t *testing.T) {
	p, g, _ := newTestProcessor()
	evt := cloudEvent(t, schemas.CalendarDeleteData{GoogleEventID: "gone"})
	if err := p.handleDelete(context.Background(), evt); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if g.deletes != 0 {
		t.Errorf("google deletes = %d, want 0 (mirror absent → skip)", g.deletes)
	}
}

// A cancelled mirror tombstone (the poller may re-mirror one) also means the delete
// already applied — skip Google.
func TestHandleDelete_SkipsWhenMirrorCancelled(t *testing.T) {
	p, g, st := newTestProcessor()
	st.events["g1"] = &store.CalendarEvent{GoogleEventID: "g1", Status: "cancelled"}
	evt := cloudEvent(t, schemas.CalendarDeleteData{GoogleEventID: "g1"})
	if err := p.handleDelete(context.Background(), evt); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if g.deletes != 0 {
		t.Errorf("google deletes = %d, want 0 (cancelled tombstone → skip)", g.deletes)
	}
}

// Crash-window backstop: the mirror row is present but Google already deleted the event
// (original delete succeeded, then crashed before the mirror delete). The 410 is the
// satisfied postcondition; finish the mirror cleanup and ack, no error.
func TestHandleDelete_BackstopsOn410(t *testing.T) {
	p, g, st := newTestProcessor()
	st.events["g1"] = &store.CalendarEvent{GoogleEventID: "g1"} // present in mirror
	// g.events has no "g1" → fakeGCal.Delete returns ErrAlreadyGone.
	evt := cloudEvent(t, schemas.CalendarDeleteData{GoogleEventID: "g1"})
	if err := p.handleDelete(context.Background(), evt); err != nil {
		t.Fatalf("delete with 410 backstop: %v", err)
	}
	if g.deletes != 1 {
		t.Errorf("google deletes = %d, want 1 (attempted)", g.deletes)
	}
	if _, ok := st.events["g1"]; ok {
		t.Error("mirror row should have been cleaned up after 410 backstop")
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

func ptr(s string) *string { return &s }

func TestHandleProviderUpsert_GeneratesIDWhenAbsent(t *testing.T) {
	p, _, st := newTestProcessor()
	evt := cloudEvent(t, schemas.ChildcareProviderUpsertData{DisplayName: "Maya"})
	if err := p.handleProviderUpsert(context.Background(), evt); err != nil {
		t.Fatalf("provider upsert: %v", err)
	}
	if len(st.providers) != 1 {
		t.Fatalf("providers recorded = %d, want 1", len(st.providers))
	}
	if !st.providers[0].ID.Valid {
		t.Error("expected a generated provider id")
	}
	if st.providers[0].DisplayName != "Maya" {
		t.Errorf("display_name = %q, want Maya", st.providers[0].DisplayName)
	}
}

func TestHandleUpsert_ReconcilesAssociations(t *testing.T) {
	p, _, st := newTestProcessor()
	start, end := timedEvent(t)
	evt := cloudEvent(t, schemas.CalendarUpsertData{
		Summary: "Soccer", Start: start, End: end,
		Subjects:       []string{"11111111-1111-4111-8111-111111111111"},
		Childcare:      ptr("22222222-2222-4222-8222-222222222222"),
		IdempotencyKey: "k-assoc",
	})
	if err := p.handleUpsert(context.Background(), evt); err != nil {
		t.Fatalf("upsert with associations: %v", err)
	}
	// The created event id is derived deterministically from the idempotency_key.
	id := deterministicEventID("k-assoc")
	if got := len(st.subjects[id]); got != 1 {
		t.Errorf("subjects for %s = %d, want 1", id, got)
	}
	if !st.childcare[id].Valid {
		t.Errorf("expected a childcare association for %s", id)
	}
}

func TestEvaluateReminders(t *testing.T) {
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	mk := func(id string, off time.Duration) expand.Instance {
		return expand.Instance{GoogleEventID: id, Start: now.Add(off)}
	}
	instances := []expand.Instance{
		mk("past", -time.Hour),     // already started — not due, not next
		mk("due", 5*time.Minute),   // within 10m lead — due + next
		mk("soon", 30*time.Minute), // future, beyond lead — not due
	}

	d := evaluateReminders(instances, now, 10*time.Minute)
	if !d.active {
		t.Error("expected an active reminder")
	}
	if len(d.due) != 1 || d.due[0].GoogleEventID != "due" {
		t.Errorf("due = %+v, want [due]", d.due)
	}
	if d.next == nil || d.next.GoogleEventID != "due" {
		t.Errorf("next = %+v, want due", d.next)
	}
}

func TestStatusState(t *testing.T) {
	in := &expand.Instance{}
	cases := []struct {
		next   *expand.Instance
		active bool
		want   string
	}{
		{nil, false, "idle"},
		{in, false, "upcoming"},
		{in, true, "reminder"},
		{nil, true, "reminder"},
	}
	for _, c := range cases {
		if got := statusState(c.next, c.active); got != c.want {
			t.Errorf("statusState(next=%v, active=%v) = %q, want %q", c.next != nil, c.active, got, c.want)
		}
	}
}

func keys(m map[string]*calendarv3.Event) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func googleTimed(id, etag string) *calendarv3.Event {
	return &calendarv3.Event{
		Id: id, Etag: etag, Status: "confirmed",
		Start: &calendarv3.EventDateTime{DateTime: "2026-06-26T09:00:00-04:00"},
		End:   &calendarv3.EventDateTime{DateTime: "2026-06-26T10:00:00-04:00"},
	}
}

// TestHandlePersonUpsert_GeneratesIDAndDefaultsKind covers #155 §3 create: an absent id is
// generated, kind defaults to "person", and the full record is upserted.
func TestHandlePersonUpsert_GeneratesIDAndDefaultsKind(t *testing.T) {
	p, _, st := newTestProcessor()
	evt := cloudEvent(t, schemas.DirectoryPersonUpsertData{DisplayName: "Junior", Color: "#ff0", Email: "junior@example.com"})
	if err := p.handlePersonUpsert(context.Background(), evt); err != nil {
		t.Fatalf("person upsert: %v", err)
	}
	if len(st.people) != 1 {
		t.Fatalf("people recorded = %d, want 1", len(st.people))
	}
	got := st.people[0]
	if !got.ID.Valid {
		t.Error("expected a generated person id")
	}
	if got.DisplayName != "Junior" || got.Kind != "person" || !got.Active {
		t.Errorf("person = %+v, want display=Junior kind=person active=true", got)
	}
	if got.Color.String != "#ff0" || got.Email.String != "junior@example.com" {
		t.Errorf("color/email not mapped: %+v", got)
	}
}

// TestHandlePersonUpsert_UpdatesByID covers rename: a supplied id is preserved (update path).
func TestHandlePersonUpsert_UpdatesByID(t *testing.T) {
	p, _, st := newTestProcessor()
	const id = "11111111-1111-4111-8111-111111111111"
	evt := cloudEvent(t, schemas.DirectoryPersonUpsertData{ID: id, DisplayName: "Renamed", Kind: "group"})
	if err := p.handlePersonUpsert(context.Background(), evt); err != nil {
		t.Fatalf("person upsert: %v", err)
	}
	if len(st.people) != 1 || st.people[0].Kind != "group" {
		t.Fatalf("expected one upsert with kind=group, got %+v", st.people)
	}
	var want pgtype.UUID
	_ = want.Scan(id)
	if st.people[0].ID != want {
		t.Errorf("id = %v, want supplied %s", st.people[0].ID, id)
	}
}

// TestHandlePersonDelete_Deactivates covers soft-delete by id.
func TestHandlePersonDelete_Deactivates(t *testing.T) {
	p, _, st := newTestProcessor()
	const id = "22222222-2222-4222-8222-222222222222"
	evt := cloudEvent(t, schemas.DirectoryPersonDeleteData{ID: id})
	if err := p.handlePersonDelete(context.Background(), evt); err != nil {
		t.Fatalf("person delete: %v", err)
	}
	if len(st.deactivated) != 1 {
		t.Fatalf("deactivated = %d, want 1", len(st.deactivated))
	}
}

// TestHandleUpsert_PerInstanceEditPatchesInstance covers ADR-0044 §2: Scope=this resolves
// the occurrence and patches the instance's own id (an override), never the master.
func TestHandleUpsert_PerInstanceEditPatchesInstance(t *testing.T) {
	p, g, st := newTestProcessor()
	start, end := timedEvent(t)
	// Seed the resolvable instance.
	g.events["g1_20260622"] = &calendarv3.Event{Id: "g1_20260622", Etag: `"inst-etag"`}
	g.instances["g1|2026-06-22T13:00:00Z"] = "g1_20260622"

	evt := cloudEvent(t, schemas.CalendarUpsertData{
		Scope: schemas.ScopeThis, RecurringEventID: "g1", OriginalStart: "2026-06-22T13:00:00Z",
		Summary: "Just this one", Start: start, End: end,
	})
	if err := p.handleUpsert(context.Background(), evt); err != nil {
		t.Fatalf("per-instance edit: %v", err)
	}
	if g.patches != 1 {
		t.Fatalf("expected 1 instance patch, got %d", g.patches)
	}
	if g.lastPatch.Recurrence != nil {
		t.Errorf("an override instance must not carry recurrence, got %v", g.lastPatch.Recurrence)
	}
	if st.upserts == 0 {
		t.Error("expected the override to be mirrored")
	}
}

// TestHandleDelete_PerInstanceCancelsOccurrence covers ADR-0044 §2 delete: Scope=this
// cancels the resolved instance (status=cancelled), not the series.
func TestHandleDelete_PerInstanceCancelsOccurrence(t *testing.T) {
	p, g, _ := newTestProcessor()
	g.events["g1_20260622"] = &calendarv3.Event{Id: "g1_20260622", Etag: `"inst-etag"`}
	g.instances["g1|2026-06-22T13:00:00Z"] = "g1_20260622"

	evt := cloudEvent(t, schemas.CalendarDeleteData{
		Scope: schemas.ScopeThis, RecurringEventID: "g1", OriginalStart: "2026-06-22T13:00:00Z",
	})
	if err := p.handleDelete(context.Background(), evt); err != nil {
		t.Fatalf("per-instance delete: %v", err)
	}
	if g.patches != 1 || g.deletes != 0 {
		t.Fatalf("expected 1 cancel-patch and 0 series deletes, got patches=%d deletes=%d", g.patches, g.deletes)
	}
	if g.lastPatch.Status != "cancelled" {
		t.Errorf("instance status = %q, want cancelled", g.lastPatch.Status)
	}
}

// TestHandleUpsert_ThisAndFollowingIsIgnored covers ADR-0044 obligation 5: the deferred
// scope is a no-op (not silently downgraded), touching neither Google nor the mirror.
func TestHandleUpsert_ThisAndFollowingIsIgnored(t *testing.T) {
	p, g, st := newTestProcessor()
	start, end := timedEvent(t)
	evt := cloudEvent(t, schemas.CalendarUpsertData{
		Scope: schemas.ScopeThisAndFollowing, RecurringEventID: "g1", OriginalStart: "2026-06-22T13:00:00Z",
		Summary: "nope", Start: start, End: end,
	})
	if err := p.handleUpsert(context.Background(), evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.patches != 0 || g.inserts != 0 || g.updates != 0 || st.upserts != 0 {
		t.Errorf("this_and_following must be a no-op, got patches=%d inserts=%d updates=%d upserts=%d",
			g.patches, g.inserts, g.updates, st.upserts)
	}
}
