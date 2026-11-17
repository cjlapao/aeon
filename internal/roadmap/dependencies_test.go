package roadmap

// dependencies_test.go — S27 T3 (CN7): the graph half of readiness. The
// rule under test is one sentence: `dependencies complete` joins the three
// readiness conditions as a CONDITION AND NOT A RANK, and every refusal
// names the blocking item by id, title and current status because a
// sentence like "dependencies are not complete" is not information. The
// pure half (IsReadyWithDeps over hand-built Dependency lists) proves the
// sentences; the store half proves the doors (Depend/Undepend) and that a
// completed dependency releases the gate ON THE NEXT LOOK with no status
// written by anyone (D4 — readiness is derived at the attempt, never
// cached, never pushed).

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// walkToPlanned is walkToRunning stopped one rung short: everything
// pre-S27 readiness needs is true — brief, plan, a priority that is not
// unknown — and the item is exactly `planned`, so anything that keeps it
// from `ready` after this point is the graph's doing alone.
func walkToPlanned(t *testing.T, r *Roadmap, it Item) Item {
	t.Helper()
	mustTransition(t, r, it.ID, StatusIdeating)
	mustTransition(t, r, it.ID, StatusBriefAgreed)
	if _, ref := r.SetArtifacts(it.ID, "/plans/deps/brief.md", "/plans/deps/plan.md"); ref != nil {
		t.Fatalf("land the pointers: %s", ref.Reason)
	}
	mustTransition(t, r, it.ID, StatusPlanned)
	if _, ref := r.SetPriority(it.ID, PriorityKnown, "P1 (deps)"); ref != nil {
		t.Fatalf("set the priority: %s", ref.Reason)
	}
	fresh, ok, err := r.Show(it.ID)
	if err != nil || !ok {
		t.Fatalf("show: %v %v", err, ok)
	}
	return fresh
}

func TestIsReadyUnchangedWithoutEdges(t *testing.T) {
	// The no-edge promise, proven the boring way: over every rung and every
	// artifact/priority shape, IsReadyWithDeps(item, nil) answers EXACTLY
	// what IsReady answered before S27 existed. T2's and S26's goldens stay
	// honest only while this holds.
	shape := func(brief, plan string, pri PriorityState) Item {
		return Item{BriefPath: brief, PlanPath: plan, Priority: pri}
	}
	for _, it := range []Item{
		shape("", "", PriorityUnknown),
		shape("/b.md", "", PriorityKnown),
		shape("", "/p.md", PriorityAssumed),
		shape("/b.md", "/p.md", PriorityUnknown),
		shape("/b.md", "/p.md", PriorityKnown),
	} {
		wantOK, wantWhy := IsReady(it)
		gotOK, gotWhy := IsReadyWithDeps(it, nil)
		if gotOK != wantOK || gotWhy != wantWhy {
			t.Errorf("edgeless IsReadyWithDeps(%+v) = (%v, %q), want the pre-S27 answer (%v, %q)",
				it, gotOK, gotWhy, wantOK, wantWhy)
		}
	}
	// An all-`done` dependency list changes nothing either: a completed
	// dependency is what readiness is made of, not a condition to carry.
	it := shape("/b.md", "/p.md", PriorityKnown)
	if ok, why := IsReadyWithDeps(it, []Dependency{{ID: 7, Status: StatusDone, Title: "finished"}}); !ok || why != "" {
		t.Errorf("a done dependency held readiness: (%v, %q)", ok, why)
	}
}

func TestOpenDependencyBlocksReady(t *testing.T) {
	it := Item{BriefPath: "/b.md", PlanPath: "/p.md", Priority: PriorityKnown}
	ok, why := IsReadyWithDeps(it, []Dependency{{ID: 21, Status: StatusRunning, Title: "collect statement templates"}})
	if ok || why == "" {
		t.Fatalf("an open dependency did not block: (%v, %q)", ok, why)
	}
	for _, frag := range []string{"not ready:", `waiting on item 21 ("collect statement templates" — running)`} {
		if !strings.Contains(why, frag) {
			t.Errorf("the sentence lacks %q: %s", frag, why)
		}
	}
	// id, title AND current status — the three things needed to act — and
	// the pre-existing three conditions keep their words when they fail too.
	noBrief := Item{PlanPath: "/p.md", Priority: PriorityKnown}
	_, why = IsReadyWithDeps(noBrief, []Dependency{{ID: 21, Status: StatusPlanned, Title: "x"}})
	if !strings.Contains(why, "no brief agreed") || !strings.Contains(why, "waiting on item 21") {
		t.Errorf("the compound sentence lost a clause: %s", why)
	}
	// Multiple open deps are all NAMED — the first answer never hides the
	// rest of the graph.
	_, why = IsReadyWithDeps(it, []Dependency{
		{ID: 21, Status: StatusPlanned, Title: "one"},
		{ID: 22, Status: StatusDone, Title: "two"},
		{ID: 23, Status: StatusRunning, Title: "three"},
	})
	if !strings.Contains(why, "item 21") || !strings.Contains(why, "item 23") || strings.Contains(why, "item 22") {
		t.Errorf("open deps not all named (and done not named): %s", why)
	}
}

func TestCancelledDependencySentence(t *testing.T) {
	it := Item{BriefPath: "/b.md", PlanPath: "/p.md", Priority: PriorityKnown}
	ok, why := IsReadyWithDeps(it, []Dependency{{ID: 21, Status: StatusCancelled, Title: "abandoned"}})
	if ok {
		t.Fatal("a cancelled dependency read as ready — waiting on a dead item is not completion")
	}
	want := `not ready: item 21 was cancelled — remove the dependency or cancel this item`
	if why != want {
		t.Errorf("cancelled sentence drifted:\n got %q\nwant %q", why, want)
	}
	// Mixed with a live wait, BOTH sentences ride — and the cancelled one is
	// its own clause, because its fix (undepend) differs from waiting's.
	_, why = IsReadyWithDeps(it, []Dependency{
		{ID: 21, Status: StatusCancelled, Title: "abandoned"},
		{ID: 22, Status: StatusPlanned, Title: "alive"},
	})
	if !strings.Contains(why, "item 21 was cancelled — remove the dependency or cancel this item") ||
		!strings.Contains(why, `waiting on item 22 ("alive" — planned)`) {
		t.Errorf("mixed cancelled/live sentence lost a clause: %s", why)
	}
}

func TestDependAndUndependDoorSentences(t *testing.T) {
	r, s := openRoadmapStore(t, "/srv/deps")
	user := Actor{Kind: "user", ID: "1000"}
	mk := func(title string) Item {
		return mustCreate(t, r, "/srv/deps", title, NewItem{Title: title})
	}
	a, b := mk("a"), mk("b")

	if ref := r.Depend(user, a.ID, a.ID); ref == nil || !strings.Contains(ref.Reason, "cannot depend on itself") {
		t.Errorf("self-edge: %+v", ref)
	}
	if ref := r.Depend(user, a.ID, 9999); ref == nil ||
		!strings.Contains(ref.Reason, "roadmap item 9999 does not exist — an edge names two real items or it names nothing") {
		t.Errorf("missing target: %+v", ref)
	}
	if ref := r.Depend(user, a.ID, b.ID); ref != nil {
		t.Fatalf("the first edge: %s", ref.Reason)
	}
	if ref := r.Depend(user, a.ID, b.ID); ref == nil ||
		ref.Reason != "roadmap item "+itoa(a.ID)+" already depends on item "+itoa(b.ID)+" — the edge is already in the graph" {
		t.Errorf("duplicate edge: %+v", ref)
	}
	// b → a is the 2-cycle: the store answers with the reverse-edge sentence
	// it shares with its trigger — the closure named is the pair itself.
	if ref := r.Depend(user, b.ID, a.ID); ref == nil ||
		ref.Reason != "roadmap_dependency: the reverse edge already exists (a 2-cycle would deadlock the graph)" {
		t.Errorf("2-cycle: %+v", ref)
	}
	// The transitive closure too: p → q → r is live, so r → p would close a
	// ring three deep — the walk names the closure that blocks, both ends
	// by id, and says WHY in one sentence.
	q, p_, rr := mk("q"), mk("p"), mk("r")
	if ref := r.Depend(user, p_.ID, q.ID); ref != nil {
		t.Fatalf("p→q: %s", ref.Reason)
	}
	if ref := r.Depend(user, q.ID, rr.ID); ref != nil {
		t.Fatalf("q→r: %s", ref.Reason)
	}
	if ref := r.Depend(user, rr.ID, p_.ID); ref == nil ||
		ref.Reason != fmt.Sprintf("roadmap: item %d cannot depend on item %d — item %d already depends on item %d; the graph would cycle and neither item could ever become ready",
			rr.ID, p_.ID, p_.ID, rr.ID) {
		t.Errorf("3-cycle closure sentence: %+v", ref)
	}
	// Cross-repository: a second component, a real second root.
	root2 := "/srv/deps2"
	if _, err := s.CreateComponent(store.NewComponent{Name: "r2", Slug: "r2", Root: root2}); err != nil {
		t.Fatalf("seed the second component: %v", err)
	}
	c := mustCreate(t, r, root2, "elsewhere", NewItem{Title: "elsewhere"})
	if ref := r.Depend(user, a.ID, c.ID); ref == nil || !strings.Contains(ref.Reason, "different repositories") {
		t.Errorf("cross-root edge: %+v", ref)
	}
	// Terminal ends: history is not rewritten — by either door.
	x := mk("cancelled")
	if _, ref := r.Cancel(x.ID); ref != nil {
		t.Fatalf("cancel: %s", ref.Reason)
	}
	if ref := r.Depend(user, x.ID, b.ID); ref == nil ||
		ref.Reason != `roadmap item `+itoa(x.ID)+` is "cancelled" — history is not rewritten; edges are authored on live items` {
		t.Errorf("depend FROM a terminal: %+v", ref)
	}
	if ref := r.Depend(user, b.ID, x.ID); ref == nil || !strings.Contains(ref.Reason, `is "cancelled"`) {
		t.Errorf("depend ONTO a terminal: %+v", ref)
	}
	// Undepend: a running item may un-wish (rule 7) — b has no edge, so the
	// refusal is the named no-such-edge, and a is live so the edge goes.
	if ref := r.Undepend(user, b.ID, a.ID); ref == nil ||
		ref.Reason != "roadmap item "+itoa(b.ID)+" does not depend on item "+itoa(a.ID)+" — there is no such edge to remove" {
		t.Errorf("no-such-edge: %+v", ref)
	}
	if ref := r.Undepend(user, x.ID, b.ID); ref == nil ||
		ref.Reason != `roadmap item `+itoa(x.ID)+` is "cancelled" — history is not rewritten; a live item un-wishes, a recorded one does not` {
		t.Errorf("undepend FROM a terminal: %+v", ref)
	}
	if ref := r.Undepend(user, a.ID, b.ID); ref != nil {
		t.Fatalf("the removal: %s", ref.Reason)
	}
	if deps, err := r.Dependencies(a.ID); err != nil || len(deps) != 0 {
		t.Errorf("the edge survived: %+v %v", deps, err)
	}
	// An edge TO a cancelled item is removable precisely because the
	// waiting item is the one being freed.
	d := mk("waits on the dead")
	if err := s.AddDependency(store.SchedulerActor{Kind: "user", ID: "1000"}, d.ID, x.ID); err != nil {
		t.Fatalf("plant the edge past the door (cancelled end, recorded before cancellation is sayable only this way): %v", err)
	}
	if ref := r.Undepend(user, d.ID, x.ID); ref != nil {
		t.Errorf("removing the cancelled-end edge: %+v", ref)
	}
}

func TestDependencyBlocksTheReadyAttempt(t *testing.T) {
	r, _ := openRoadmapStore(t, "/srv/deps")
	user := Actor{Kind: "user", ID: "1000"}
	a := walkToPlanned(t, r, mustCreate(t, r, "/srv/deps", "wants to start", NewItem{Title: "wants to start"}))
	b := mustCreate(t, r, "/srv/deps", "upstream", NewItem{Title: "upstream"})
	if ref := r.Depend(user, a.ID, b.ID); ref != nil {
		t.Fatalf("the edge: %s", ref.Reason)
	}
	// Every pre-S27 readiness condition is TRUE and the item still cannot
	// reach `ready` — and the refusal is the full sentence with its tail.
	if _, ref := r.Transition(a.ID, StatusReady); ref == nil {
		t.Fatal("a blocked item reached ready")
	} else {
		for _, frag := range []string{
			`waiting on item ` + itoa(b.ID) + ` ("upstream" — idea)`,
			"(D4 is decided at the attempt)",
		} {
			if !strings.Contains(ref.Reason, frag) {
				t.Errorf("the attempt refusal lacks %q: %s", frag, ref.Reason)
			}
		}
	}
	// The board says the same words (rule 3: never silently stuck).
	answer, ref := r.Board(BoardFilter{ComponentRoot: "/srv/deps"})
	if ref != nil {
		t.Fatal(ref.Reason)
	}
	seen := false
	for _, row := range answer.Rows {
		if row.ID == a.ID {
			seen = true
			if !strings.Contains(row.Blocker, "waiting on item "+itoa(b.ID)) {
				t.Errorf("the board hides the dependency: %+v", row)
			}
		}
	}
	if !seen {
		t.Fatal("the item is not on its own board")
	}
}

func TestDependencyCompletionReleasesOnNextPick(t *testing.T) {
	// CN7's completion half: the moment the last dependency reaches `done`,
	// the item is ready — because the NEXT look recomputes. Nobody writes
	// the row, no repair pass exists, no status moved by anyone but the
	// item's own door: a status is a rung, readiness is a conclusion.
	r, _ := openRoadmapStore(t, "/srv/deps")
	user := Actor{Kind: "user", ID: "1000"}
	a := walkToPlanned(t, r, mustCreate(t, r, "/srv/deps", "held", NewItem{Title: "held"}))
	b := walkToPlanned(t, r, mustCreate(t, r, "/srv/deps", "upstream", NewItem{Title: "upstream"}))
	if ref := r.Depend(user, a.ID, b.ID); ref != nil {
		t.Fatalf("the edge: %s", ref.Reason)
	}
	if _, ref := r.Transition(a.ID, StatusReady); ref == nil {
		t.Fatal("blocked by nothing? the dependency did not gate")
	}
	// The dependency completes through its OWN door (planned → … → done).
	mustTransition(t, r, b.ID, StatusReady)
	mustTransition(t, r, b.ID, StatusRunning)
	mustTransition(t, r, b.ID, StatusDone)

	// a moved NOTHING: still `planned`, the same row it was.
	before, ok, err := r.Show(a.ID)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if before.Status != StatusPlanned {
		t.Fatalf("the completion wrote a status — it is %q, and nothing may do that", before.Status)
	}
	// The next LOOK says ready with no refusal at all.
	deps, err := r.Dependencies(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok, why := IsReadyWithDeps(before, deps); !ok {
		t.Fatalf("the gate did not open on the next look: %s", why)
	}
	if _, ref := r.Transition(a.ID, StatusReady); ref != nil {
		t.Errorf("the attempt still refused: %s", ref.Reason)
	}
	fresh, _, _ := r.Show(a.ID)
	if fresh.Status != StatusReady {
		t.Fatalf("after a successful attempt the row is %q, not ready", fresh.Status)
	}
}
