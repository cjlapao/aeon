package roadmap

// T2's gates, store half: the CRUD doors, the D4 moment, and the "cancel
// never deletes" ledger walk against a real store in a temp directory —
// house style, never the real one (§5).

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

func openRoadmapStore(t *testing.T, root string) (*Roadmap, *store.Store) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the test store (a failure naming fts5 means the build lacks the sqlite_fts5 tag — build through make, B6): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.CreateComponent(store.NewComponent{Name: "r", Slug: "r", Root: root}); err != nil {
		t.Fatalf("seed the component for %s: %v", root, err)
	}
	return New(s), s
}

func mustCreate(t *testing.T, r *Roadmap, root, title string, in NewItem) Item {
	t.Helper()
	if in.Provenance == "" {
		in.Provenance = ProvenanceAuthored
	}
	it, ref := r.Create(root, in)
	if ref != nil {
		t.Fatalf("create %q: %s", title, ref.Reason)
	}
	return it
}

func mustTransition(t *testing.T, r *Roadmap, id int64, to Status) Item {
	t.Helper()
	fresh, ref := r.Transition(id, to)
	if ref != nil {
		t.Fatalf("transition %d → %s: %s", id, to, ref.Reason)
	}
	return fresh
}

// walkToRunning takes an item up to `running` the only honest way: D4's
// three conditions land BEFORE the attempt (artifacts set, priority set),
// exactly as T3/T4's dispatch files will land them — the walk is a test
// of the machine, not a bypass of it.
func walkToRunning(t *testing.T, r *Roadmap, it Item) Item {
	t.Helper()
	mustTransition(t, r, it.ID, StatusIdeating)
	mustTransition(t, r, it.ID, StatusBriefAgreed)
	if _, ref := r.SetArtifacts(it.ID, "/plans/walk/brief.md", "/plans/walk/plan.md"); ref != nil {
		t.Fatalf("land the pointers: %s", ref.Reason)
	}
	mustTransition(t, r, it.ID, StatusPlanned)
	if _, ref := r.SetPriority(it.ID, PriorityKnown, "P1 (walked)"); ref != nil {
		t.Fatalf("set the priority: %s", ref.Reason)
	}
	mustTransition(t, r, it.ID, StatusReady)
	return mustTransition(t, r, it.ID, StatusRunning)
}

func TestCreateEntersAtIdeaWithTheHonestDefaults(t *testing.T) {
	r, _ := openRoadmapStore(t, "/repo/create")
	it := mustCreate(t, r, "/repo/create", "a plan of intent", NewItem{
		Title: "a plan of intent", Epic: "onboarding", Provenance: ProvenanceTriggered,
	})
	if it.Status != StatusIdea {
		t.Errorf("a created item is %q, want idea — the entry rung is the machine's, not the caller's", it.Status)
	}
	if it.Priority != PriorityUnknown {
		t.Errorf("an item nobody prioritized reads %q — D2's honest absence is the default", it.Priority)
	}
	if it.Provenance != ProvenanceTriggered {
		t.Errorf("provenance came back %q, want the answered triggered", it.Provenance)
	}
	if it.Epic != "onboarding" {
		t.Errorf("epic came back %q — R2's tag rides plain", it.Epic)
	}
	if it.Materialized() {
		t.Error("a created item is already Materialized() — loop_slug must start unset (D6)")
	}

	// The door's own vocabulary refusals (the CHECK is the backstop, the
	// door owes the sentence).
	if _, ref := r.Create("/repo/create", NewItem{Title: "t", Provenance: "webhook-created"}); ref == nil ||
		!strings.Contains(ref.Reason, "provenance") {
		t.Errorf("an invented provenance got %v, want a D5 sentence naming the three", ref)
	}
	if _, ref := r.Create("/repo/create", NewItem{Title: "t", Priority: "low", Provenance: ProvenanceAuthored}); ref == nil ||
		!strings.Contains(ref.Reason, "one of the three") {
		t.Errorf("priority as a number got %v, want D2's refusal — never collapsed", ref)
	}
	if _, ref := r.Create("/repo/create", NewItem{Title: "t", Provenance: ProvenanceAuthored,
		Estimate: &Estimate{Min: 1, Max: 3, Basis: ""}}); ref == nil ||
		!strings.Contains(ref.Reason, "basis") {
		t.Errorf("a basis-less range got %v, want D3's refusal", ref)
	}
	if _, ref := r.Create("/repo/create", NewItem{Title: "t", Provenance: ProvenanceAuthored,
		Estimate: &Estimate{Min: 5, Max: 2, Basis: "backwards"}}); ref == nil ||
		!strings.Contains(ref.Reason, "above") {
		t.Errorf("a reversed range got %v, want the arithmetic refusal", ref)
	}
	if _, ref := r.Create("/repo/create", NewItem{Title: "t", Provenance: ProvenanceAuthored,
		PriorityLabel: "P1"}); ref == nil || !strings.Contains(ref.Reason, "D2") {
		t.Errorf("a label on the default-unknown priority got %v, want D2's contradiction refusal", ref)
	}
}

func TestReadyRequiresAllThreeConditionsAtTransitionTime(t *testing.T) {
	// T2.md's gate, D4's teeth: an item whose priority is downgraded to
	// unknown AFTER planning cannot reach ready — readiness is decided at
	// the attempt, not remembered from when each condition first held.
	r, _ := openRoadmapStore(t, "/repo/d4")
	it := mustCreate(t, r, "/repo/d4", "d4", NewItem{Title: "d4", Provenance: ProvenanceAuthored,
		Priority: PriorityAssumed, PriorityLabel: "probably now"})
	mustTransition(t, r, it.ID, StatusIdeating)
	mustTransition(t, r, it.ID, StatusBriefAgreed)
	if _, ref := r.SetArtifacts(it.ID, "/plans/d4/brief.md", ""); ref != nil {
		t.Fatalf("land the brief: %s", ref.Reason)
	}
	mustTransition(t, r, it.ID, StatusPlanned)
	if _, ref := r.SetArtifacts(it.ID, "", "/plans/d4/plan.md"); ref != nil {
		t.Fatalf("land the plan: %s", ref.Reason)
	}

	// All three now — ready lands.
	ready := mustTransition(t, r, it.ID, StatusReady)
	if ready.Status != StatusReady {
		t.Fatalf("the honest path did not reach ready, got %q", ready.Status)
	}

	// Downgrade AFTER planning: a re-triaged item loses its readiness,
	// and a NEW attempt must see that. First put it back to planned in
	// the only way the machine permits (cancel the ready item — D7 — and
	// the attempt below runs on a fresh planned row).
	if _, ref := r.Cancel(ready.ID); ref != nil {
		t.Fatalf("a ready item must be cancellable (D7): %s", ref.Reason)
	}
	it2 := mustCreate(t, r, "/repo/d4", "d4-b", NewItem{Title: "d4-b", Provenance: ProvenanceTriaged,
		Priority: PriorityKnown, PriorityLabel: "P1"})
	mustTransition(t, r, it2.ID, StatusIdeating)
	mustTransition(t, r, it2.ID, StatusBriefAgreed)
	if _, ref := r.SetArtifacts(it2.ID, "/plans/d4/brief.md", "/plans/d4/plan.md"); ref != nil {
		t.Fatalf("land both pointers: %s", ref.Reason)
	}
	mustTransition(t, r, it2.ID, StatusPlanned)
	// The downgrade — the moment D4 exists for.
	if _, ref := r.SetPriority(it2.ID, PriorityUnknown, ""); ref != nil {
		t.Fatalf("downgrade the priority: %s", ref.Reason)
	}
	_, ref := r.Transition(it2.ID, StatusReady)
	if ref == nil {
		t.Fatal("planned → ready landed with priority unknown — D4 is cached, not decided at the attempt")
	}
	if !strings.Contains(ref.Reason, "unknown") {
		t.Errorf("the refusal said %q — it must name WHICH condition failed", ref.Reason)
	}
	// Re-prioritize → the SAME attempt now succeeds. Readiness is a
	// function of the row as it is now, not of history.
	if _, ref := r.SetPriority(it2.ID, PriorityKnown, "P1 after re-triage"); ref != nil {
		t.Fatalf("re-prioritize: %s", ref.Reason)
	}
	if got := mustTransition(t, r, it2.ID, StatusReady); got.Status != StatusReady {
		t.Errorf("a re-prioritized planned item still cannot reach ready: %q", got.Status)
	}

	// The two pointer conditions refuse by name too.
	bare := mustCreate(t, r, "/repo/d4", "bare", NewItem{Title: "bare", Provenance: ProvenanceAuthored,
		Priority: PriorityKnown, PriorityLabel: "P2"})
	mustTransition(t, r, bare.ID, StatusIdeating)
	mustTransition(t, r, bare.ID, StatusBriefAgreed)
	mustTransition(t, r, bare.ID, StatusPlanned)
	if _, ref := r.Transition(bare.ID, StatusReady); ref == nil ||
		!strings.Contains(ref.Reason, "brief") || !strings.Contains(ref.Reason, "plan") {
		t.Errorf("ready on a pointer-less item got %v, want a refusal naming BOTH unset pointers", ref)
	}
}

func TestCancelNeverDeletesAndClosesTheRightDoors(t *testing.T) {
	// D7 + the never-lose-a-record discipline: cancel sets the status,
	// the row survives the listing, running refuses (D11 carried), done
	// is terminal.
	r, s := openRoadmapStore(t, "/repo/cancel")
	it := mustCreate(t, r, "/repo/cancel", "gone-but-here", NewItem{Title: "gone-but-here", Provenance: ProvenanceAuthored})
	fresh, ref := r.Cancel(it.ID)
	if ref != nil {
		t.Fatalf("idea → cancelled: %s", ref.Reason)
	}
	if fresh.Status != StatusCancelled {
		t.Fatalf("cancel set %q", fresh.Status)
	}
	rows, err := r.List("/repo/cancel", ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != it.ID {
		t.Fatalf("the cancelled row vanished from the list — cancel must never delete (D7), got %d rows", len(rows))
	}
	if _, ok, err := s.RoadmapItemByID(it.ID); err != nil || !ok {
		t.Fatalf("the cancelled row vanished from the table — cancel must never delete (D7)")
	}
	if _, ref := r.Cancel(it.ID); ref != nil && ref.Moved {
		t.Errorf("cancelling a cancelled item was treated as a race, want the idempotent table answer")
	}

	// running refuses; the loop is the thing to stop, not the row.
	run := mustCreate(t, r, "/repo/cancel", "running", NewItem{Title: "running", Provenance: ProvenanceAuthored})
	walkToRunning(t, r, run)
	_, ref = r.Cancel(run.ID)
	if ref == nil || !strings.Contains(ref.Reason, "loop in flight") {
		t.Errorf("cancel on running got %v, want the D11 sentence (the loop is the record to stop)", ref)
	}

	// done is terminal both ways.
	if _, ref := r.Transition(run.ID, StatusDone); ref != nil {
		t.Fatalf("running → done: %s", ref.Reason)
	}
	if _, ref := r.Cancel(run.ID); ref == nil {
		t.Error("a done item was cancelled — done is terminal")
	}
}

func TestEstimateAndPriorityTravelWhole(t *testing.T) {
	r, _ := openRoadmapStore(t, "/repo/whole")
	it := mustCreate(t, r, "/repo/whole", "sized", NewItem{Title: "sized", Provenance: ProvenanceAuthored})
	est, ref := r.SetEstimate(it.ID, 2, 5, "three files at half a day plus review")
	if ref != nil {
		t.Fatalf("set the estimate: %s", ref.Reason)
	}
	if est.Estimate == nil || est.Estimate.Min != 2 || est.Estimate.Max != 5 || est.Estimate.Basis == "" {
		t.Fatalf("the estimate did not round-trip whole: %+v", est.Estimate)
	}
	if _, ref := r.SetEstimate(it.ID, 1, 1, ""); ref == nil || !strings.Contains(ref.Reason, "basis") {
		t.Errorf("a basis-less estimate got %v", ref)
	}
	if _, ref := r.SetPriority(it.ID, PriorityKnown, "P1"); ref != nil {
		t.Fatalf("set priority: %s", ref.Reason)
	}
	if _, ref := r.SetPriority(it.ID, PriorityUnknown, "stale P1"); ref == nil ||
		!strings.Contains(ref.Reason, "contradicts") {
		t.Errorf("the unknown-with-label pair got %v, want D2's refusal — the pair is ONE decision", ref)
	}
	if _, ref := r.SetEstimate(it.ID, 3, 1, "backwards"); ref == nil || !strings.Contains(ref.Reason, "above") {
		t.Errorf("a reversed range got %v", ref)
	}
}

func TestListOrdersTheGanttAndFiltersHonest(t *testing.T) {
	r, _ := openRoadmapStore(t, "/repo/list")
	a := mustCreate(t, r, "/repo/list", "a", NewItem{Title: "a", Epic: "now", Provenance: ProvenanceAuthored})
	mustCreate(t, r, "/repo/list", "b", NewItem{Title: "b", Epic: "later", Provenance: ProvenanceAuthored,
		Priority: PriorityAssumed, PriorityLabel: "soon-ish"})
	c := mustCreate(t, r, "/repo/list", "c", NewItem{Title: "c", Epic: "now", Provenance: ProvenanceTriaged})
	mustTransition(t, r, a.ID, StatusIdeating)
	mustTransition(t, r, c.ID, StatusCancelled)

	all, err := r.List("/repo/list", ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 rows (cancelled included — the list never deletes through the side door), got %d", len(all))
	}
	// Gantt order: the machine's rungs, idea's first among ideas' peers;
	// a=ideating, b=idea, c=cancelled.
	if all[0].Title != "b" || all[1].Title != "a" || all[2].Title != "c" {
		t.Errorf("the list came in order %s/%s/%s — the forward Gantt walks the machine, cancelled last",
			all[0].Title, all[1].Title, all[2].Title)
	}
	byEpic, _ := r.List("/repo/list", ListFilter{Epic: "now"})
	if len(byEpic) != 2 {
		t.Errorf("epic=now returned %d rows, want 2", len(byEpic))
	}
	byPriority, _ := r.List("/repo/list", ListFilter{Priority: PriorityAssumed})
	if len(byPriority) != 1 || byPriority[0].Title != "b" {
		t.Errorf("priority=assumed returned %+v, want only b", byPriority)
	}
	byStatus, _ := r.List("/repo/list", ListFilter{Status: StatusCancelled})
	if len(byStatus) != 1 || byStatus[0].Title != "c" {
		t.Errorf("status=cancelled returned %+v, want only c", byStatus)
	}
	// A filter set nobody matches says so with zero rows — not a refusal.
	none, err := r.List("/repo/list", ListFilter{Epic: "nope"})
	if err != nil || len(none) != 0 {
		t.Errorf("an unmatched filter returned %d rows / %v, want an honest empty", len(none), err)
	}
}

func TestRecordLoopLandsOnlyOnRunning(t *testing.T) {
	// R4's second half: the loop record is claimed by the rung that made
	// the loop. A real loop row backs the composite FK (a ghost slug the
	// database refuses — tested in internal/store); here the STATUS guard
	// is the claim.
	r, s := openRoadmapStore(t, "/repo/loop")
	if _, err := s.CreateLoop(store.NewLoop{Slug: "built", ComponentRoot: "/repo/loop", Goal: "the item's plan"}); err != nil {
		t.Fatalf("seed the loop: %v", err)
	}
	it := mustCreate(t, r, "/repo/loop", "materializing", NewItem{Title: "materializing", Provenance: ProvenanceAuthored})
	if _, ref := r.RecordLoop(it.ID, it.ComponentRoot, "built"); ref == nil || !strings.Contains(ref.Reason, "running") {
		t.Errorf("a loop record on an idea got %v, want the rung refusal", ref)
	}
	walkToRunning(t, r, it)
	fresh, ref := r.RecordLoop(it.ID, it.ComponentRoot, "built")
	if ref != nil {
		t.Fatalf("record the loop: %s", ref.Reason)
	}
	if !fresh.Materialized() || fresh.LoopSlug != "built" {
		t.Fatalf("Materialized()/LoopSlug did not land: %+v", fresh)
	}
	// A ghost slug even while running: the store's FK refuses it (the door
	// reports the store's refusal as a refusal, not a panic).
	if _, ref := r.RecordLoop(it.ID, it.ComponentRoot, "ghost"); ref == nil {
		t.Error("a ghost slug was recorded — the composite FK must refuse it")
	}
}

func TestCountIsD8sHonestZero(t *testing.T) {
	// D8's evidence door: a project with no roadmap items COUNTS zero —
	// the number loop.start's zero-roadmap property test will assert
	// against is this one, not a remembered guess.
	r, _ := openRoadmapStore(t, "/repo/empty")
	n, err := r.Count("/repo/empty")
	if err != nil || n != 0 {
		t.Fatalf("count on a fresh root: %d / %v, want 0 / nil", n, err)
	}
	n2, err := r.Count("/repo/never-seen")
	if err != nil || n2 != 0 {
		t.Fatalf("count on an unknown root: %d / %v, want 0 / nil — absence of items is not an error", n2, err)
	}
}

func TestRefusalIsADiscriminatedResultNotABareError(t *testing.T) {
	// DB19 at the door: a policy refusal is a *Refusal a caller branches
	// on — and it satisfies error for the socket layer that must thread
	// one, without becoming how callers decide.
	r, _ := openRoadmapStore(t, "/repo/db19")
	it := mustCreate(t, r, "/repo/db19", "x", NewItem{Title: "x", Provenance: ProvenanceAuthored})
	_, ref := r.Transition(it.ID, StatusDone) // idea → done: unlisted
	if ref == nil {
		t.Fatal("the idea → done shortcut was accepted")
	}
	var err error = ref
	if err.Error() != ref.Reason {
		t.Error("Refusal-as-error must say the Reason verbatim for the socket layer")
	}
	// Absence is honest absence — Show says false, not a refusal.
	if _, ok, err := r.Show(4242); ok || err != nil {
		t.Error("a ghost id must read absent-and-errorless, not refused")
	}
}

// TestDecodeReadsTheFourthStoredProvenance — S61 T12: the store may hold
// `intake` (the T1 CHECK widening), so the read plane must read it; the S61
// walk measured roadmap.list REFUSING to list its own intake's items.
func TestDecodeReadsTheFourthStoredProvenance(t *testing.T) {
	it, err := ItemFromRow(store.RoadmapItemRow{
		ID: 1, ComponentRoot: "/r", Title: "from the intake", Status: "idea",
		PriorityState: "unknown", Provenance: "intake",
	})
	if err != nil {
		t.Fatalf("the read plane refused a provenance the store holds: %v", err)
	}
	if it.Provenance != Provenance("intake") {
		t.Fatalf("provenance: %q", it.Provenance)
	}
}

// TestCreateDoorStillRefusesIntakeProvenance keeps the WRITE door closed to
// the fourth word: only S61's records step files it, a person never types it.
func TestCreateDoorStillRefusesIntakeProvenance(t *testing.T) {
	root := t.TempDir()
	rm, _ := openRoadmapStore(t, root)
	_, ref := rm.Create(filepath.Join(root, "proj"), NewItem{Title: "forged", Provenance: Provenance("intake")})
	if ref == nil || !strings.Contains(ref.Reason, "not one of the three") {
		t.Fatalf("the create door accepted the fourth word: %+v", ref)
	}
}
