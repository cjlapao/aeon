package agentseed

import (
	"context"
	"database/sql"
	"testing"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return s
}

// TestRoundTripIsLossless is SD8: every declared record, written by the
// applier and read back by the fingerprint, yields the same content hash —
// nothing a body declared is lost in the move from code to row.
func TestRoundTripIsLossless(t *testing.T) {
	s := openStore(t)
	records := Source()
	if len(records) == 0 {
		t.Fatal("the source produced no records — the agents set is empty")
	}
	ap := NewApplier(s)
	for _, rec := range records {
		if err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
			return ap.Upsert(context.Background(), tx, rec, false)
		}); err != nil {
			t.Fatalf("apply %s: %v", rec.Key, err)
		}
		var hash string
		var present bool
		if err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
			var err error
			hash, present, err = ap.Fingerprint(context.Background(), tx, rec.Key)
			return err
		}); err != nil {
			t.Fatalf("fingerprint %s: %v", rec.Key, err)
		}
		if !present {
			t.Fatalf("the row for %s was not written", rec.Key)
		}
		want := seed.Hash(rec.Content)
		if hash != want {
			t.Fatalf("round-trip lost content for %s: fingerprint %s != source hash %s", rec.Key, hash, want)
		}
	}
}

// TestAdoptionFillsASStubKeepsItsIdentity is SD9: a S43 stub (present, empty
// body, version 1) is adopted in place — the applier fills it, Adoptable
// reports yes, and the round-trip still holds.
func TestAdoptionFillsASStubKeepsItsIdentity(t *testing.T) {
	s := openStore(t)
	records := Source()
	ap := NewApplier(s)
	rec := records[0]
	c := mustDecode(rec.Content)
	// A S43-style stub: the slug, internal=1, version 1, empty content.
	if err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
		_, e := tx.ExecContext(context.Background(),
			`INSERT INTO agent (slug, name, description, prompt, tools, temperature, handoff, policy_agent, body_hash, origin, internal, metadata, version, created_at, updated_at)
			 VALUES (?, ?, ?, '', '[]', 0, '{}', '', '', 'system', 1, '{}', '1', '', '')`,
			c.Slug, c.Name, c.Description)
		return e
	}); err != nil {
		t.Fatalf("stub insert: %v", err)
	}
	var adoptable bool
	if err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
		var e error
		adoptable, e = ap.Adoptable(context.Background(), tx, c.Slug)
		return e
	}); err != nil {
		t.Fatalf("adoptable: %v", err)
	}
	if !adoptable {
		t.Fatal("a S43 stub was not reported adoptable (SD9)")
	}
	if err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
		return ap.Upsert(context.Background(), tx, rec, true)
	}); err != nil {
		t.Fatalf("adopt upsert: %v", err)
	}
	var hash string
	if err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
		var e error
		hash, _, e = ap.Fingerprint(context.Background(), tx, rec.Key)
		return e
	}); err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if hash != seed.Hash(rec.Content) {
		t.Fatalf("the adopted row lost content: %s != %s", hash, seed.Hash(rec.Content))
	}
}

// TestConflictIsNeverOverwritten is SD6: a present row that is NOT a stub
// (it has content) is not adoptable.
func TestConflictIsNeverOverwritten(t *testing.T) {
	s := openStore(t)
	records := Source()
	ap := NewApplier(s)
	rec := records[0]
	c := mustDecode(rec.Content)
	if err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
		_, e := tx.ExecContext(context.Background(),
			`INSERT INTO agent (slug, name, description, prompt, tools, temperature, handoff, policy_agent, body_hash, origin, internal, metadata, version, created_at, updated_at)
			 VALUES (?, ?, ?, 'an operator wrote this', '[]', 0, '{}', '', '', 'user', 0, '{}', '1', '', '')`,
			c.Slug, c.Name, c.Description)
		return e
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var adoptable bool
	if err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
		var e error
		adoptable, e = ap.Adoptable(context.Background(), tx, c.Slug)
		return e
	}); err != nil {
		t.Fatalf("adoptable: %v", err)
	}
	if adoptable {
		t.Fatal("a row with content was reported adoptable — it is an operator's choice (SD6)")
	}
}

func mustDecode(b []byte) content {
	c, err := decode(b)
	if err != nil {
		panic("agentseed test: " + err.Error())
	}
	return c
}
