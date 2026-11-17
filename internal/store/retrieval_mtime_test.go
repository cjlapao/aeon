package store

import "testing"

// retrieval_mtime_test.go — S71 T1: the per-source mtime record, the cheap
// gate's memory. The gate (internal/retrieval/code.go) reads and writes
// these; this pins the store contract: upsert→read round-trips, an absent
// record reads (0, false) never a zero-mtime "match", and a second upsert
// updates rather than duplicating (the PRIMARY KEY on (corpus_id, source)).
func TestSourceMtimeUpsertAndRead(t *testing.T) {
	s := newStore(t)
	c, err := s.EnsureRetrievalCorpus("/root", "code")
	if err != nil {
		t.Fatalf("ensure corpus: %v", err)
	}
	// No record reads as (0, false), not (0, true) — an absent record is a
	// candidate (re-hash), never a "fresh" match against a zero mtime.
	if m, found, err := s.SourceMtime(c.ID, "a.go"); found || m != 0 || err != nil {
		t.Fatalf("an absent record must read (0,false,nil), got (%d,%v,%v)", m, found, err)
	}
	// upsert → read round-trip.
	if err := s.UpsertSourceMtime(c.ID, "a.go", 1000); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if m, found, err := s.SourceMtime(c.ID, "a.go"); !found || m != 1000 || err != nil {
		t.Fatalf("round-trip: got (%d,%v,%v), want (1000,true,nil)", m, found, err)
	}
	// a second upsert updates, does not duplicate (PRIMARY KEY on corpus+source).
	if err := s.UpsertSourceMtime(c.ID, "a.go", 2000); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	if m, found, err := s.SourceMtime(c.ID, "a.go"); !found || m != 2000 || err != nil {
		t.Fatalf("second upsert must update, got (%d,%v,%v)", m, found, err)
	}
	// a different source in the same corpus is independent — the record is
	// per (corpus, source), never a global (O4 held at the row level).
	if err := s.UpsertSourceMtime(c.ID, "b.go", 3000); err != nil {
		t.Fatalf("upsert b: %v", err)
	}
	if m, found, _ := s.SourceMtime(c.ID, "a.go"); !found || m != 2000 {
		t.Fatalf("a.go must be untouched by b.go's upsert, got (%d,%v)", m, found)
	}
}
