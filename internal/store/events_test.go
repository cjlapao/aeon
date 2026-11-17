package store

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// T8's tests. The first one is the promise S2 deferred to this slice.

func TestSeqSurvivesACloseAndReopenAndContinues(t *testing.T) {
	// D9, and the one test S2 could not write.
	//
	// S2's seq was an in-memory counter that reset on restart; its doc comment
	// and its demo both said so, naming this slice. The store CLOSES and
	// REOPENS here, because that is the whole promise — an in-memory counter
	// passes every test that does not.
	dir := t.TempDir()

	first := openAt(t, dir)
	var last int64
	for _, kind := range []string{"daemon.started", "client.connected", "client.disconnected"} {
		ev, err := first.AppendEvent(NewEvent{Kind: kind})
		if err != nil {
			t.Fatal(err)
		}
		if ev.Seq <= last {
			t.Fatalf("seq went backwards within one run: %d after %d", ev.Seq, last)
		}
		last = ev.Seq
	}
	head, err := first.HeadSeq()
	if err != nil {
		t.Fatal(err)
	}
	if head != last {
		t.Errorf("HeadSeq is %d, the last allocated seq was %d", head, last)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := openAt(t, dir)
	ev, err := second.AppendEvent(NewEvent{Kind: "daemon.started"})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Seq != last+1 {
		t.Errorf("after a restart seq is %d; it must continue from %d. This is the gap S2 admitted, and "+
			"a cursor that silently restarts redelivers events a client already saw", ev.Seq, last)
	}

	// And a cursor from before the restart still works across it.
	after, err := second.ReadEvents(last, EventFilter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].Seq != last+1 {
		t.Errorf("a cursor held across the restart returned %+v", after)
	}
}

func TestSeqIsNeverReusedEvenAfterRowsAreDeleted(t *testing.T) {
	// AUTOINCREMENT, not plain rowid. With a plain INTEGER PRIMARY KEY, SQLite
	// reuses the highest id after a delete — so a cursor at 3 would be handed a
	// NEW event numbered 3 and would skip it forever.
	//
	// Measured: after a close, a reopen AND a delete of every row, the next id
	// was 4 where three had been used.
	dir := t.TempDir()
	s := openAt(t, dir)
	for i := 0; i < 3; i++ {
		if _, err := s.AppendEvent(NewEvent{Kind: "test.event"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.w.Exec(`DELETE FROM event`); err != nil {
		t.Fatal(err)
	}
	ev, err := s.AppendEvent(NewEvent{Kind: "test.event"})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Seq != 4 {
		t.Errorf("after deleting every row the next seq is %d, want 4 — a reused seq is a cursor that "+
			"silently skips an event", ev.Seq)
	}
}

func TestSeqIsMonotonicWithNoDuplicatesOverARun(t *testing.T) {
	s := openTemp(t)
	seen := map[int64]bool{}
	var last int64
	for i := 0; i < 200; i++ {
		ev, err := s.AppendEvent(NewEvent{Kind: "test.event"})
		if err != nil {
			t.Fatal(err)
		}
		if seen[ev.Seq] {
			t.Fatalf("seq %d was allocated twice", ev.Seq)
		}
		seen[ev.Seq] = true
		if ev.Seq <= last {
			t.Fatalf("seq %d followed %d", ev.Seq, last)
		}
		last = ev.Seq
	}
	if last != 200 {
		t.Errorf("200 appends ended at seq %d; within one table the sequence has no gaps", last)
	}
}

func TestAFilteredReadReturnsMatchingEventsWithGapsAndTheCursorStillWorks(t *testing.T) {
	// Gaps are CORRECT. The cursor's contract is "everything after N that
	// matches my filter", and a subscription filtered to one component receives
	// ids with holes in them — the holes are other projects' events and carry
	// no information the client needs.
	s := openTemp(t)

	// Interleaved, so the matching ids cannot be contiguous.
	for i := 0; i < 4; i++ {
		if _, err := s.AppendEvent(NewEvent{ComponentRoot: "/repo-a", Kind: "loop.started"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AppendEvent(NewEvent{ComponentRoot: "/repo-b", Kind: "loop.started"}); err != nil {
			t.Fatal(err)
		}
	}

	mine, err := s.ReadEvents(0, EventFilter{ComponentRoot: "/repo-a"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 4 {
		t.Fatalf("the filtered read returned %d events, want 4", len(mine))
	}
	gapped := false
	for i := 1; i < len(mine); i++ {
		if mine[i].Seq != mine[i-1].Seq+1 {
			gapped = true
		}
	}
	if !gapped {
		t.Error("the filtered read returned contiguous ids, so this test is not exercising the gap case")
	}

	// A cursor built from the last one returns exactly the next matching
	// events — not the next ids.
	if _, err := s.AppendEvent(NewEvent{ComponentRoot: "/repo-b", Kind: "loop.started"}); err != nil {
		t.Fatal(err)
	}
	next, err := s.AppendEvent(NewEvent{ComponentRoot: "/repo-a", Kind: "loop.finished"})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := s.ReadEvents(mine[len(mine)-1].Seq, EventFilter{ComponentRoot: "/repo-a"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(resumed) != 1 || resumed[0].Seq != next.Seq {
		t.Errorf("resuming from the last matching seq returned %+v, want only seq %d", resumed, next.Seq)
	}
}

func TestADaemonLifecycleEventBelongsToNoLoopAndNoProject(t *testing.T) {
	// `event.loop_slug` and `event.component_root` are NULLABLE, unlike v1's,
	// and there is no foreign key to `loop`. `daemon.started` has no loop, and
	// a NOT NULL column would make the daemon invent one.
	s := openTemp(t)
	ev, err := s.AppendEvent(NewEvent{Kind: "daemon.started", Payload: map[string]any{"endpoint": "/run/aeon.sock"}})
	if err != nil {
		t.Fatalf("a daemon event could not be written: %v", err)
	}
	if ev.LoopSlug != "" || ev.ComponentRoot != "" {
		t.Errorf("a daemon event acquired a scope: %+v", ev)
	}

	var slug, root *string
	if err := s.r.QueryRow(`SELECT loop_slug, component_root FROM event WHERE id = ?`, ev.Seq).
		Scan(&slug, &root); err != nil {
		t.Fatal(err)
	}
	if slug != nil || root != nil {
		t.Errorf("stored as %v / %v rather than NULL", slug, root)
	}

	// And an unscoped read still sees it, which is what `aeon watch` does.
	all, err := s.ReadEvents(0, EventFilter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("the unscoped read returned %d events", len(all))
	}
}

func TestAMissingPayloadIsStoredAsAnEmptyObjectRatherThanNull(t *testing.T) {
	// Every reader then parses ONE shape, and a client written against
	// "payload is an object" never meets a null.
	s := openTemp(t)
	ev, err := s.AppendEvent(NewEvent{Kind: "ping"})
	if err != nil {
		t.Fatal(err)
	}
	if string(ev.Payload) != "{}" {
		t.Errorf("the payload is %q, want {}", ev.Payload)
	}
	back, err := s.ReadEvents(ev.Seq-1, EventFilter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var into map[string]any
	if err := json.Unmarshal(back[0].Payload, &into); err != nil {
		t.Errorf("the stored payload does not parse as an object: %v", err)
	}
}

func TestReadEventsHonoursKindsAndLimit(t *testing.T) {
	s := openTemp(t)
	for _, kind := range []string{"loop.started", "loop.finished", "ask.delta", "ask.done", "daemon.started"} {
		if _, err := s.AppendEvent(NewEvent{Kind: kind}); err != nil {
			t.Fatal(err)
		}
	}

	exact, err := s.ReadEvents(0, EventFilter{Kinds: []string{"ask.done"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) != 1 || exact[0].Kind != "ask.done" {
		t.Errorf("an exact kind filter returned %+v", exact)
	}

	// The protocol's wildcard shape.
	wild, err := s.ReadEvents(0, EventFilter{Kinds: []string{"loop.*"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(wild) != 2 {
		t.Errorf("loop.* returned %d events, want 2", len(wild))
	}

	several, err := s.ReadEvents(0, EventFilter{Kinds: []string{"ask.*", "daemon.started"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(several) != 3 {
		t.Errorf("two filters returned %d events, want 3", len(several))
	}

	limited, err := s.ReadEvents(0, EventFilter{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 {
		t.Errorf("limit 2 returned %d events", len(limited))
	}
}

func TestAKindContainingAWildcardCharacterCannotBecomeAWildcard(t *testing.T) {
	// The injection-shaped version of this mistake: a kind holding `%` would
	// otherwise match everything once it reached a LIKE.
	s := openTemp(t)
	for _, kind := range []string{"loop.started", "100%.weird"} {
		if _, err := s.AppendEvent(NewEvent{Kind: kind}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ReadEvents(0, EventFilter{Kinds: []string{"100%.weird"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "100%.weird" {
		t.Errorf("a kind containing %% matched %d events: %+v", len(got), got)
	}
}

func TestOnlyTheStoreWritesEventID(t *testing.T) {
	// DB20 / I5, asserted by grep across `internal/` and `cmd/`. Two
	// allocators produce duplicates or regressions, and a cursor handed a
	// duplicate silently delivers the same event twice.
	hits := grepRepo(t, regexp.MustCompile(`INSERT\s+INTO\s+event\b`))
	for _, hit := range hits {
		if !strings.HasPrefix(hit.file, filepath.Join("internal", "store")) {
			t.Errorf("%s writes to the event table:\n    %s\n  Only the store allocates seq (DB20)",
				hit.file, hit.line)
		}
	}
	if len(hits) == 0 {
		t.Error("the grep found no INSERT INTO event at all, so it is not checking anything")
	}
}

func TestEventsAreAppendOnly(t *testing.T) {
	// No update path and no delete path. An event is what happened; a record of
	// what happened that can be edited is not a record.
	//
	// The test's own DELETE in TestSeqIsNeverReused goes through s.w directly,
	// which is why this greps the non-test sources.
	bad := regexp.MustCompile(`(UPDATE\s+event|DELETE\s+FROM\s+event)\b`)
	for name, src := range packageFiles(t) {
		if m := bad.FindString(src); m != "" {
			t.Errorf("%s has an event mutation path (%s); events are append-only", name, m)
		}
	}
}

func TestMemoryAppendAndReadRoundTrip(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "memory", "/repo")

	if _, err := s.AppendMemory(sc, "qa-expert", "T4", "the scope test needs the same slug in two roots"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendMemory(sc, "", "", "a note with no agent and no task"); err != nil {
		t.Fatal(err)
	}

	notes, err := s.ReadMemory(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("got %d notes", len(notes))
	}
	if notes[0].Agent != "qa-expert" || notes[0].TaskID != "T4" {
		t.Errorf("the first note came back as %+v", notes[0])
	}
	if notes[1].Agent != "" || notes[1].TaskID != "" {
		t.Errorf("the second note invented an agent or a task: %+v", notes[1])
	}
	if notes[0].At == "" {
		t.Error("a note has no timestamp")
	}
}

func TestMemoryIsScopedToItsLoop(t *testing.T) {
	s := openTemp(t)
	a := seedLoop(t, s, "refactor", "/repo-a")
	b := seedLoop(t, s, "refactor", "/repo-b")
	if _, err := s.AppendMemory(a, "", "", "only in A"); err != nil {
		t.Fatal(err)
	}
	notes, err := s.ReadMemory(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Errorf("/repo-b sees /repo-a's memory: %+v", notes)
	}
}

func TestAMemoryNeedsANote(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "blank", "/repo")
	if _, err := s.AppendMemory(sc, "", "", "  "); err == nil {
		t.Fatal("a blank memory was accepted")
	}
}
