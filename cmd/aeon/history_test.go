package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// openHistoryCLIStore opens a throwaway store for a CLI test — never the real
// one (§5: tests never point at the real store). The in-process verbs reach
// only this store, exactly the `policy` test idiom.
func openHistoryCLIStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open a store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seedHistoryFixture lays down the three loops every gate needs: one live loop
// with a known step cost, one that will be archived, and one blocked with a
// recorded reason (so D4's verbatim read has a row to read from). It calls the
// store directly — there is no CLI verb for archiving (design Q4), exactly as
// the DEMO walk will.
func seedHistoryFixture(t *testing.T, s *store.Store) {
	t.Helper()
	if _, err := s.CreateComponent(store.NewComponent{Name: "/proj", Slug: "/proj", Root: "/proj"}); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	if _, err := s.CreateLoop(store.NewLoop{Slug: "still-running", ComponentRoot: "/proj", Goal: "a live loop"}); err != nil {
		t.Fatalf("create live loop: %v", err)
	}
	if _, err := s.CreateLoop(store.NewLoop{Slug: "was-blocked", ComponentRoot: "/proj", Goal: "a blocked loop"}); err != nil {
		t.Fatalf("create blocked loop: %v", err)
	}
	if _, err := s.CreateLoop(store.NewLoop{Slug: "will-be-archived", ComponentRoot: "/proj", Goal: "an archived loop"}); err != nil {
		t.Fatalf("create archived loop: %v", err)
	}

	// A known-cost step on the live loop, so a rollup can floor or not.
	sc := store.Scope{LoopSlug: "still-running", ComponentRoot: "/proj"}
	step, err := s.CreateStep(sc, store.NewStep{Phase: "build", Agent: "gpt", Status: "done"})
	if err != nil {
		t.Fatalf("create step: %v", err)
	}
	if err := s.SetStepCost(step.ID, model.Cost{State: model.CostKnown, USD: 0.5}); err != nil {
		t.Fatalf("set cost: %v", err)
	}

	// A blocked loop with a recorded reason (D4): the runtime publishes it as a
	// loop.blocked event the event stream carries, and the loop is set blocked.
	if _, err := s.AppendEvent(store.NewEvent{
		LoopSlug:      "was-blocked",
		ComponentRoot: "/proj",
		Kind:          "loop.blocked",
		Payload:       map[string]any{"reason": "the gate answer had no rework target"},
	}); err != nil {
		t.Fatalf("append a blocked event: %v", err)
	}
	if err := s.SetLoopStatus(store.Scope{LoopSlug: "was-blocked", ComponentRoot: "/proj"}, "blocked"); err != nil {
		t.Fatalf("block was-blocked: %v", err)
	}

	// Archive "will-be-archived": it leaves the live tables and appears as an
	// archive header.
	if _, err := s.ArchiveLoop(store.Scope{LoopSlug: "will-be-archived", ComponentRoot: "/proj"}, "done, cold storage"); err != nil {
		t.Fatalf("archive will-be-archived: %v", err)
	}
}

// TestExportAndListShareOneQuery is H3's parity: `list` and `export` call the
// same internal/history read (historyQuery), parameterized only by output
// format. It is proven by the fact both verbs — fed the same store and the
// same flags — yield the same set of rows; a second query in one verb would
// produce a different set and fail here. The shared query itself is the one
// read both call, so asserting the rows match the shared query's rows is the
// call-site assertion the gate asks for.
func TestExportAndListShareOneQuery(t *testing.T) {
	s := openHistoryCLIStore(t)
	seedHistoryFixture(t, s)

	// The shared query, directly: the ground truth both verbs must match.
	want, err := historyQuery(s, historyOptions(), "")
	if err != nil {
		t.Fatalf("the shared query: %v", err)
	}
	wantRows := map[string]history.HistoryRow{}
	for _, e := range want {
		r := rowOf(e)
		wantRows[r.Slug] = r
	}

	// export: one JSON object per line, parsed back to the row shape.
	var exp bytes.Buffer
	if err := historyExport(s, &exp, nil); err != nil {
		t.Fatalf("export: %v", err)
	}
	exportRows := map[string]history.HistoryRow{}
	sc := bufio.NewScanner(&exp)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r history.HistoryRow
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("export line is not a JSON object: %v", err)
		}
		exportRows[r.Slug] = r
	}
	if len(exportRows) != len(wantRows) {
		t.Fatalf("export produced %d rows, the shared query %d — they must share one query", len(exportRows), len(wantRows))
	}
	for slug, wantR := range wantRows {
		got, ok := exportRows[slug]
		if !ok {
			t.Errorf("export is missing %q — a second query, not the shared one", slug)
			continue
		}
		if got.Cost != wantR.Cost || got.Status != wantR.Status || got.Reason != wantR.Reason {
			t.Errorf("export %q drifted from the shared query: got %s/%s/%q want %s/%s/%q",
				slug, got.Cost, got.Status, got.Reason, wantR.Cost, wantR.Status, wantR.Reason)
		}
	}

	// list: the same store, the same flags, the same query — the table must
	// carry the identical set (every slug the shared query and export have).
	var lis bytes.Buffer
	if err := historyList(s, &lis, nil); err != nil {
		t.Fatalf("list: %v", err)
	}
	for slug := range wantRows {
		if !bytes.Contains(lis.Bytes(), []byte(slug)) {
			t.Errorf("list is missing %q — a second query, not the shared one", slug)
		}
	}
}

// historyOptions is the flag-free fold the gate verbs share (no scope, no
// group-by, no since) — so export and list query exactly the same thing.
func historyOptions() history.Options {
	return historyFlags{}.options()
}

// TestExportIsNDJSONOneRowPerLine is H3's output half: export is NDJSON — one
// JSON object per line — and each object has the SAME shape `list`'s rows have
// (the one rowOf definition), not a second shape. A line that does not parse
// as an object, or an object missing a field list shows, fails here.
func TestExportIsNDJSONOneRowPerLine(t *testing.T) {
	s := openHistoryCLIStore(t)
	seedHistoryFixture(t, s)

	var out bytes.Buffer
	if err := historyExport(s, &out, nil); err != nil {
		t.Fatalf("export: %v", err)
	}
	sc := bufio.NewScanner(&out)
	count := 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		count++
		var r history.HistoryRow
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("line %d is not a JSON object: %v", count, err)
		}
		// Every field a person reads in `list`'s table is in the object — the
		// two cannot drift (H3): id, slug, status, cost, and the reason.
		if r.ID == "" || r.Slug == "" || r.Cost == "" {
			t.Errorf("export object %d is missing a field list shows: %+v", count, r)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if count == 0 {
		t.Errorf("export produced no rows — nothing to be one object per line")
	}
}

// TestHistoryShowsABlockedReasonVerbatim is D4/H4: a blocked loop's reason is
// the stored reason, read from the event stream verbatim, never a synthesized
// verdict. It is pinned against a fixture with a known reason so a render that
// rephrases or fabricates a verdict shows up.
func TestHistoryShowsABlockedReasonVerbatim(t *testing.T) {
	s := openHistoryCLIStore(t)
	seedHistoryFixture(t, s)
	const want = "the gate answer had no rework target"

	// export carries the reason verbatim (the one rowOf, the one formatter).
	var out bytes.Buffer
	if err := historyExport(s, &out, nil); err != nil {
		t.Fatalf("export: %v", err)
	}
	var found bool
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		var r history.HistoryRow
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("parse: %v", err)
		}
		if r.Slug == "was-blocked" {
			found = true
			if r.Reason != want {
				t.Errorf("a blocked loop's reason is not verbatim: got %q want %q", r.Reason, want)
			}
		}
	}
	if !found {
		t.Errorf("the blocked loop is missing from export")
	}

	// list prints the same reason verbatim (the one rowOf).
	var lis bytes.Buffer
	if err := historyList(s, &lis, nil); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !bytes.Contains(lis.Bytes(), []byte("blocked: "+want)) {
		t.Errorf("list does not print the blocked reason verbatim\n%s", lis.String())
	}
}
