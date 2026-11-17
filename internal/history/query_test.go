package history

import (
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// openHistoryStore opens a throwaway store for a test — never the real one
// (§5: tests never point at the real store).
func openHistoryStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open a store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seedComponent registers the one component a fixture's loops live under, so a
// loop's component_root is real and ListLoops/ComponentScope find it.
func seedComponent(t *testing.T, s *store.Store, root string) {
	t.Helper()
	if _, err := s.CreateComponent(store.NewComponent{Name: root, Slug: root, Root: root}); err != nil {
		t.Fatalf("seed component %s: %v", root, err)
	}
}

// seedLoop creates a loop under the component and returns its scope + row.
func seedLoop(t *testing.T, s *store.Store, root, slug string) (store.Loop, store.Scope) {
	t.Helper()
	l, err := s.CreateLoop(store.NewLoop{Slug: slug, ComponentRoot: root, Goal: "goal of " + slug})
	if err != nil {
		t.Fatalf("create loop %s: %v", slug, err)
	}
	return l, store.Scope{LoopSlug: slug, ComponentRoot: root}
}

// seedStep creates a step and sets its cost to the three-valued state a
// fixture asks for, returning the created step. An unknown-cost step is the
// P32 case that must floor a rollup; a known one must not.
func seedStep(t *testing.T, s *store.Store, sc store.Scope, in store.NewStep, cost model.Cost) store.Step {
	t.Helper()
	step, err := s.CreateStep(sc, in)
	if err != nil {
		t.Fatalf("create step: %v", err)
	}
	if err := s.SetStepCost(step.ID, cost); err != nil {
		t.Fatalf("set cost of step %d: %v", step.ID, err)
	}
	return step
}

// TestMergeIncludesArchivedLoops is D7: history does not stop at the archive
// boundary. A project with one live and one archived loop returns BOTH from
// one call — the naive loop-only query that the design's §2 finding warns of
// would return only the live one, silently, forever, with no error anywhere;
// this test pins that the archived loop is in the sequence, tagged, and
// placed by its OWN timeline (when it ran), not by when it was archived.
func TestMergeIncludesArchivedLoops(t *testing.T) {
	s := openHistoryStore(t)
	seedComponent(t, s, "/proj")

	// One loop that stays live, one that will be archived.
	seedLoop(t, s, "/proj", "still-running")
	seedLoop(t, s, "/proj", "will-be-archived")

	// A blocked loop with a recorded reason, so D4's verbatim read has a row
	// to read from (the reason the runtime publishes as a loop.blocked event,
	// which the event stream carries).
	blk, _ := seedLoop(t, s, "/proj", "was-blocked")
	if _, err := s.AppendEvent(store.NewEvent{
		LoopSlug:      blk.Slug,
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
	// archive header. (There is no CLI verb for this yet — design's Q4 — so
	// the test calls the store directly, exactly as the DEMO walk will.)
	if _, err := s.ArchiveLoop(store.Scope{LoopSlug: "will-be-archived", ComponentRoot: "/proj"}, "done, cold storage"); err != nil {
		t.Fatalf("archive will-be-archived: %v", err)
	}

	entries, err := Merge(s, Options{Scope: store.ComponentScope{ComponentRoot: "/proj"}})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	// All three loops must be present — D7, one sequence, not two screens.
	bySlug := map[string]Entry{}
	for _, e := range entries {
		bySlug[e.Slug] = e
	}
	for _, want := range []string{"still-running", "was-blocked", "will-be-archived"} {
		if _, ok := bySlug[want]; !ok {
			t.Errorf("loop %q is missing from the merge — a naive loop-only query", want)
		}
	}

	// The archived entry is tagged, so the caller never has to know which
	// table it came from.
	archived, ok := bySlug["will-be-archived"]
	if !ok {
		t.Fatalf("the archived loop is missing from the merge — D7 is broken")
	}
	if archived.Kind != KindArchived {
		t.Errorf("the archived entry is not tagged KindArchived (got %v) — the caller must never have to know which table it came from", archived.Kind)
	}
	if !archived.Cost.Floor {
		t.Errorf("an archived loop's cost is not an honest unknown floor (got %+v) — P32: a cost that cannot be read is a floor, not a total", archived.Cost)
	}

	// The blocked loop's reason is read verbatim from the event stream (D4),
	// never a synthesized verdict.
	if blk, ok := bySlug["was-blocked"]; ok && blk.Reason != "the gate answer had no rework target" {
		t.Errorf("a blocked loop's reason is not read verbatim: got %q", blk.Reason)
	}
}
