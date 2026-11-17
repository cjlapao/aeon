package seed

import (
	"context"
	"fmt"
	"testing"
)

// countingSet is a set whose source counts how many times it is read. A
// domain's source is expensive in direct proportion to its record count —
// the agents' source parses every embedded body and canonicalises every
// record (measured: 69 ms for 33 records) — so "how many times did an
// operation read the source" is the same question as "how long did the
// whole-set status door take". S72 measured the answer before this guard
// existed: 6.4 s cold for seed.status over the 15 sets, because a per-record
// key lookup re-invoked the source per record.
func countingSet(n int) (Set, *int, *fakeApplier) {
	calls := 0
	s := Set{
		ID:      "agents.firstparty",
		Class:   ClassInert,
		Version: 1,
		Records: func() []Record {
			calls++
			recs := make([]Record, 0, n)
			for i := 1; i <= n; i++ {
				recs = append(recs, Record{
					Key:     fmt.Sprintf("body-%02d", i),
					Version: 1,
					Content: []byte(`{"slug":"body"}`),
				})
			}
			return recs
		},
	}
	return s, &calls, newFakeApplier("agents.firstparty", false)
}

// TestEngineReadsTheSetSourceOncePerOperation is the S72 guard: every
// engine operation reads a set's source exactly once, however many records
// it walks. A regression here is invisible to the correctness tests — the
// records are the same on every read — and shows up only as seconds on the
// status door.
func TestEngineReadsTheSetSourceOncePerOperation(t *testing.T) {
	const n = 12
	ctx := context.Background()

	// Status over one set, and over every set (the whole-set door the UI calls).
	s, calls, a := countingSet(n)
	e, _ := newEngine(t, s, a)
	*calls = 0
	st, err := e.Status(ctx, s.ID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if *calls != 1 {
		t.Errorf("Status read the source %d times for a %d-record set, want 1", *calls, n)
	}
	if len(st) != 1 || len(st[0].Records) != n {
		t.Fatalf("status reported %d sets / %d records, want 1 / %d", len(st), len(st[0].Records), n)
	}
	for _, r := range st[0].Records {
		if r.AvailableVersion != 1 {
			t.Fatalf("record %q reports available version %d, want 1 — the index missed it", r.Key, r.AvailableVersion)
		}
	}
	*calls = 0
	if _, err := e.Status(ctx, ""); err != nil {
		t.Fatalf("status (all sets): %v", err)
	}
	if *calls != 1 {
		t.Errorf("Status over every set read the source %d times, want 1", *calls)
	}

	// Plan.
	s, calls, a = countingSet(n)
	e, _ = newEngine(t, s, a)
	*calls = 0
	pl, err := e.Plan(ctx, s.ID, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if *calls != 1 {
		t.Errorf("Plan read the source %d times for a %d-record set, want 1", *calls, n)
	}
	if len(pl.Decisions) != n {
		t.Fatalf("plan made %d decisions, want %d", len(pl.Decisions), n)
	}

	// Apply — the write path resolves a record per decision, the shape that
	// was quadratic (recordFor per write).
	s, calls, a = countingSet(n)
	e, _ = newEngine(t, s, a)
	*calls = 0
	if _, err := e.Apply(ctx, s.ID, nil, Actor{Kind: "user", ID: "u1"}, PathExplicit); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if *calls != 1 {
		t.Errorf("Apply read the source %d times for a %d-record set, want 1", *calls, n)
	}
	if a.upserts != n {
		t.Fatalf("apply wrote %d rows, want %d", a.upserts, n)
	}

	// A re-apply where every record is an up-to-date skip still reads once.
	*calls = 0
	if _, err := e.Apply(ctx, s.ID, nil, Actor{Kind: "user", ID: "u1"}, PathExplicit); err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if *calls != 1 {
		t.Errorf("a no-change re-apply read the source %d times, want 1", *calls)
	}
}

// TestRestoreReadsTheSetSourceOnce covers the single-record door, which
// resolves one key: one read, not one per lookup (the record, the plan and
// the ledger hash each used to ask the source).
func TestRestoreReadsTheSetSourceOnce(t *testing.T) {
	const n = 6
	ctx := context.Background()
	s, calls, a := countingSet(n)
	e, repo := newEngine(t, s, a)
	by := Actor{Kind: "user", ID: "u1"}
	if _, err := e.Apply(ctx, s.ID, nil, by, PathExplicit); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// Put one record into a removable state so the restore has something to do.
	k := keyOf(s.ID, "body-03")
	row, ok := repo.ledger[k]
	if !ok {
		t.Fatal("the applied ledger row is missing")
	}
	row.State = string(StateDiverged)
	repo.ledger[k] = row
	*calls = 0
	res, err := e.Restore(ctx, s.ID, "body-03", by)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if *calls != 1 {
		t.Errorf("Restore read the source %d times for a %d-record set, want 1", *calls, n)
	}
	if len(res.Records) != 1 || res.Records[0].Op != OpRestore {
		t.Fatalf("restore reported %+v, want one restore", res.Records)
	}
}

// TestSnapshotMatchesTheLinearLookups pins the index against the behaviour it
// replaced: a missing key reports version 0 and a key-only Record, the first
// of two duplicate keys wins, and a present key hands back the same Record.
func TestSnapshotMatchesTheLinearLookups(t *testing.T) {
	s := Set{
		ID: "x.y", Class: ClassInert, Version: 2,
		Records: func() []Record {
			return []Record{
				{Key: "a", Version: 3, Content: []byte("A")},
				{Key: "a", Version: 9, Content: []byte("dup")},
				{Key: "b", Version: 1, Content: []byte("B")},
			}
		},
	}
	sn := newSetSnapshot(s)
	if got := sn.version("a"); got != 3 {
		t.Errorf("version(a) = %d, want 3 (the first record wins, as the linear scan did)", got)
	}
	if got := sn.version("missing"); got != 0 {
		t.Errorf("version(missing) = %d, want 0", got)
	}
	if r := sn.record("missing"); r.Key != "missing" || len(r.Content) != 0 || r.Version != 0 {
		t.Errorf("record(missing) = %+v, want a key-only Record", r)
	}
	if r, ok := sn.lookup("b"); !ok || r.Version != 1 || string(r.Content) != "B" {
		t.Errorf("lookup(b) = %+v ok=%v, want the record", r, ok)
	}
	if sn.id() != "x.y" {
		t.Errorf("id() = %q, want the set's id", sn.id())
	}
}
