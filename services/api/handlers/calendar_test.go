//go:build fast

package handlers

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/primaryrutabaga/ruby-core/pkg/calendar/store"
)

func mkUUID(b byte) pgtype.UUID { return pgtype.UUID{Bytes: [16]byte{b}, Valid: true} }
func txt(s string) pgtype.Text  { return pgtype.Text{String: s, Valid: s != ""} }

// TestBuildEmailIndex covers the #133 multi-email reconciliation: aliases resolve to their
// person (case-insensitively), inactive people's aliases are ignored, and a primary email
// wins over a different person's alias on collision.
func TestBuildEmailIndex(t *testing.T) {
	p1, p2, inactive := mkUUID(1), mkUUID(2), mkUUID(9)
	people := []*store.DirectoryPerson{
		{ID: p1, Email: txt("Mom@example.com")},
		{ID: p2, Email: txt("dad@example.com")},
	}
	aliases := []*store.ListAllPersonEmailsRow{
		{PersonID: p1, Email: "Mom.Alias@Gmail.com"},  // alias → p1 (case-folded)
		{PersonID: inactive, Email: "ghost@example.com"}, // inactive (not in people) → ignored
		{PersonID: p2, Email: "MOM@example.com"},       // collides with p1's primary → must not win
	}

	idx := buildEmailIndex(people, aliases)

	if got := idx["mom@example.com"]; got != uuidStr(p1) {
		t.Errorf("primary collision: mom@example.com = %q, want p1 %q", got, uuidStr(p1))
	}
	if got := idx["mom.alias@gmail.com"]; got != uuidStr(p1) {
		t.Errorf("alias mom.alias@gmail.com = %q, want p1 %q", got, uuidStr(p1))
	}
	if _, ok := idx["ghost@example.com"]; ok {
		t.Error("an inactive person's alias must not be indexed")
	}
	if len(idx) != 3 { // mom primary, dad primary, mom alias
		t.Errorf("index size = %d, want 3 (%v)", len(idx), idx)
	}
}
