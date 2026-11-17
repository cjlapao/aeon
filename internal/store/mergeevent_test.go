package store

// mergeevent_test.go — S57 T5's missing `merged` fact (VG14): the write
// is idempotent on the unique key (the re-entry "already merged" succeeds
// and writes at most one row), the record's honest fields (the minted id,
// the minted merged_at, the NULLs), and the read's scope door (a
// half-named loop is not a loop).

import (
	"strings"
	"testing"
	"time"
)

// TestMergeEventIsIdempotent — the T5 name: the engine's re-entry — the
// check-then-act that finds the merge already dispatched, and the crash
// between the dispatch and this write — records the SAME (component_root,
// pr, merge_sha) and gets the row that stands, and the table holds exactly
// one row for the merge. A different sha is a different merge, and a
// different PR is a different merge.
func TestMergeEventIsIdempotent(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "me-root")
	sc := seedLoop(t, s, "me-loop", "me-root")

	rec := MergeEventRecord{
		Scope: sc, ComponentID: "me-root", PR: 7, MergeSHA: "abc123",
		Method: "squash", BaseRef: "main", HeadSHA: "def456", RuleRevision: 3,
		Evidence: "the gate allowed it", VerifyRunID: "vr-1",
		MergedAt: time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC),
		Actor:    Actor{Kind: "user", ID: "u1"},
	}
	first, err := s.RecordMergeEvent(rec)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if first.ID == "" || !strings.HasPrefix(first.ID, "me-") {
		t.Fatalf("the id is minted: %q", first.ID)
	}
	if first.MergedAt != "2026-11-02T05:00:00.000Z" {
		t.Errorf("the given merged_at stands: %q", first.MergedAt)
	}

	// The re-entry: the same key, a different minted id. It is NOT an
	// error — the already-merged answer — and it returns the row that
	// stands.
	rec.ID = "me-something-else"
	again, err := s.RecordMergeEvent(rec)
	if err != nil {
		t.Fatalf("the re-entry is the already-merged answer, not an error: %v", err)
	}
	if again.ID != first.ID {
		t.Fatalf("the re-entry returned a different row: %q vs %q", again.ID, first.ID)
	}
	if n := countTable(t, g, "merge_event"); n != 1 {
		t.Fatalf("the idempotency wrote %d rows, want 1", n)
	}

	// A different sha is a different merge (a re-merge after a force-push
	// is a new fact). It carries a LATER merged_at so the read's
	// "newest first" is a real ordering, not an id tie-break.
	rec.ID = ""
	rec.MergeSHA = "ghi789"
	rec.MergedAt = time.Date(2026, 11, 2, 6, 0, 0, 0, time.UTC)
	second, err := s.RecordMergeEvent(rec)
	if err != nil || second.ID == first.ID {
		t.Fatalf("a new sha is a new row: %v %q", err, second.ID)
	}
	if n := countTable(t, g, "merge_event"); n != 2 {
		t.Fatalf("rows after the new sha = %d, want 2", n)
	}

	// The read: the loop's facts, newest first.
	evs, err := s.ListMergeEvents(MergeEventFilter{Scope: sc})
	if err != nil || len(evs) != 2 {
		t.Fatalf("list: %d events err %v, want 2", len(evs), err)
	}
	if evs[0].MergeSHA != "ghi789" || evs[1].MergeSHA != "abc123" {
		t.Errorf("newest first: %q then %q", evs[0].MergeSHA, evs[1].MergeSHA)
	}
	// The scope door: a half-named loop is not a loop.
	if _, err := s.ListMergeEvents(MergeEventFilter{Scope: Scope{LoopSlug: "me-loop"}}); err == nil {
		t.Errorf("a slug alone listed the events")
	}
	if _, err := s.ListMergeEvents(MergeEventFilter{Scope: Scope{ComponentRoot: "me-root"}}); err == nil {
		t.Errorf("a root alone listed the events")
	}
	// Another loop's fact is invisible to this loop's read.
	other, _ := s.RecordMergeEvent(MergeEventRecord{
		Scope: Scope{LoopSlug: "other", ComponentRoot: "me-root"}, PR: 8,
		MergeSHA: "zzz", Evidence: "someone else's merge",
	})
	_ = other
	if evs, _ = s.ListMergeEvents(MergeEventFilter{Scope: sc}); len(evs) != 2 {
		t.Errorf("the read bled across loops: %d events, want 2", len(evs))
	}
}

// TestMergeEventFieldsRoundTrip — the record's honest fields: an empty
// id is minted, a zero merged_at is minted to now, and the optional halves
// (the component, the method, the base/head, the revision, the run, the
// actor) stand NULL when empty and carry their value when set.
func TestMergeEventFieldsRoundTrip(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "me2-root")
	sc := seedLoop(t, s, "me2-loop", "me2-root")

	// Everything optional empty: the NULLs.
	spars, err := s.RecordMergeEvent(MergeEventRecord{
		Scope: sc, PR: 1, MergeSHA: "s1", Evidence: "the fact",
	})
	if err != nil {
		t.Fatalf("record the sparse: %v", err)
	}
	if spars.MergedAt == "" {
		t.Errorf("a zero merged_at is minted to now")
	}
	if spars.ComponentID != nil || spars.Method != nil || spars.BaseRef != nil ||
		spars.HeadSHA != nil || spars.RuleRevision != nil || spars.VerifyRunID != nil ||
		spars.ActorKind != nil || spars.ActorID != nil {
		t.Errorf("the empty halves stand NULL: %+v", spars)
	}

	// Everything set: the values round-trip.
	full, err := s.RecordMergeEvent(MergeEventRecord{
		Scope: sc, ComponentID: "me2-root", PR: 2, MergeSHA: "s2",
		Method: "rebase", BaseRef: "main", HeadSHA: "h2", RuleRevision: 11,
		Evidence: "the fact", VerifyRunID: "vr-77",
		Actor: Actor{Kind: "user", ID: "u9"},
	})
	if err != nil {
		t.Fatalf("record the full: %v", err)
	}
	if full.ComponentID == nil || *full.ComponentID != "me2-root" || full.Method == nil || *full.Method != "rebase" ||
		full.BaseRef == nil || *full.BaseRef != "main" || full.HeadSHA == nil || *full.HeadSHA != "h2" ||
		full.RuleRevision == nil || *full.RuleRevision != 11 || full.VerifyRunID == nil || *full.VerifyRunID != "vr-77" ||
		full.ActorKind == nil || *full.ActorKind != "user" || full.ActorID == nil || *full.ActorID != "u9" {
		t.Errorf("the set halves round-trip: %+v", full)
	}
	if n := countTable(t, g, "merge_event"); n != 2 {
		t.Errorf("rows = %d, want 2", n)
	}
}

// TestMergeEventDoors — the record's own refusals (the engine's caller
// bugs, answered as errors — this is an engine path, not a human door,
// and it has no actor parameter by design): no loop, no PR, no sha, no
// evidence.
func TestMergeEventDoors(t *testing.T) {
	_, s := testStore(t)
	base := MergeEventRecord{
		Scope: Scope{LoopSlug: "x", ComponentRoot: "r"}, PR: 1,
		MergeSHA: "s", Evidence: "e",
	}
	cases := []struct {
		name   string
		mutate func(*MergeEventRecord)
	}{
		{"no loop", func(r *MergeEventRecord) { r.Scope = Scope{LoopSlug: "x"} }},
		{"no PR", func(r *MergeEventRecord) { r.PR = 0 }},
		{"no sha", func(r *MergeEventRecord) { r.MergeSHA = "" }},
		{"no evidence", func(r *MergeEventRecord) { r.Evidence = "" }},
	}
	for _, c := range cases {
		r := base
		c.mutate(&r)
		if _, err := s.RecordMergeEvent(r); err == nil {
			t.Errorf("%s: the record landed — the door is missing", c.name)
		}
	}
}
