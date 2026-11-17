package loop

// allowoutward_test.go — S19 T6's loop-side gates. The distinction that
// makes this task ("That distinction is the point of the task"): a test
// that READS the effect class proves the class is set; these tests DRIVE
// the loop until it reaches an outward action and assert what happens —
// the composition is what the claim rests on, and the version that reads
// the policy passes even if the trigger forgot to mark the loop
// unattended.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/store"
)

// startTriggered starts a loop the way the narrow trigger leaves it at
// the daemon: attended pinned false (the trigger's one transform), the
// opt-in as given, and nothing else claimed.
func startTriggered(t *testing.T, s *store.Store, slug, blueprintPath string, allowOutward bool) LoopState {
	t.Helper()
	return startTriggeredWith(t, s, slug, blueprintPath, allowOutward, nil)
}

// startTriggeredWith adds the S16 T12 single-task `with` bridge: an
// OUTWARD action that runs needs its task identity declared at start —
// the run-side gates carry it so the drive reaches the forge fake, and
// the deny-side gates deliberately do not: a denial that fires before
// argument validation is the stronger property (the loop does not even
// find out whether it could have run).
func startTriggeredWith(t *testing.T, s *store.Store, slug, blueprintPath string, allowOutward bool, with map[string]string) LoopState {
	t.Helper()
	st, err := Start(context.Background(), s, store.NewLoop{
		Slug: slug, ComponentRoot: t.TempDir(), Root: t.TempDir(), Mode: "standard",
		AllowOutward: allowOutward,
	}, blueprintPath, boolPtr(false), with)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// taskIdentity is the honest single-task shape the S16 walk used: the
// person starting the run declares which task this outward action serves.
func taskIdentity() map[string]string {
	return map[string]string{
		"task":       "4a17",
		"task_title": "Add version endpoint",
		"pr_summary": "Serves the per-loop opt-in drive; the base is code's fact, not the model's.",
	}
}

func outwardBlueprint(t *testing.T, actionName string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "outward.yaml")
	writeBlueprint(t, path, "  - id: act\n    step: {kind: code-action, action: "+actionName+"}\n")
	return path
}

// TestTriggeredLoopIsUnattended — asserted on the loop's PINNED
// attendedness (G14): the value the trigger forces is the value the
// decision site reads, stored as data, and never re-derived. The
// never-declared third state stays a third state beside it — false is a
// declaration, NULL is an absence, and collapsing them is exactly what
// would let a defaulted true slide in unnoticed (P31).
func TestTriggeredLoopIsUnattended(t *testing.T) {
	s := openLoopStore(t)
	st := startTriggered(t, s, "trig", outwardBlueprint(t, action.ForgeOpenPR), false)
	att, err := s.LoopAttended(context.Background(), st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if att == nil || *att {
		t.Fatalf("the triggered loop's pinned attendedness is %v; want *false — the trigger's transform must land as data, not be re-guessed downstream", att)
	}
	// The contrast that makes "pinned false" mean something: a client
	// that sends nothing is NEVER-DECLARED, not attended and not
	// unattended-collapsed.
	quiet, err := Start(context.Background(), s, store.NewLoop{Slug: "quiet", ComponentRoot: t.TempDir(), Root: t.TempDir(), Mode: "standard"}, outwardBlueprint(t, action.ForgeOpenPR), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.LoopAttended(context.Background(), quiet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if q != nil {
		t.Error("a client that declared nothing read back as a declaration — the three states must stay three")
	}
}

// TestOutwardDeniesEndToEnd — DRIVEN (D9, C8): the loop runs until it
// reaches forge.openPR, and the denial arrives at the loop, naming the
// class and that the loop is unattended. The forge fake is the witness:
// zero calls means the door actually stayed shut — a policy table can
// say deny while the handler runs anyway, and only the drive sees that.
func TestOutwardDeniesEndToEnd(t *testing.T) {
	s := openLoopStore(t)
	f := forge.NewFake()
	st := startTriggered(t, s, "deny", outwardBlueprint(t, action.ForgeOpenPR), false)
	dep := Deps{Store: s, Actions: func(string) *action.Services { return svcWith(f) }}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" {
		t.Fatalf("status %q; want blocked — an unattended outward action without the opt-in must stop the loop at the action", saved.Status)
	}
	if !strings.Contains(saved.LastReason, "outward") || !strings.Contains(saved.LastReason, "unattended") {
		t.Errorf("the block must name the class AND that the loop is unattended, got %q", saved.LastReason)
	}
	for _, c := range f.Calls() {
		if strings.Contains(c.Method, "OpenPR") || strings.Contains(c.Method, "CreatePR") {
			t.Fatalf("the fake saw the outward action run (%v) while the loop says blocked — the composition failed between policy and dispatch", c)
		}
	}
}

// TestOutwardRunsWhenOptedIn — the opt-in's positive half, driven too:
// the SAME loop shape with allowOutward declared at start runs the action
// (the fake is the witness in this direction as well), and the block
// reason nowhere claims a denial. This is why "per loop, at start" is the
// right shape: the decision is in the row the runtime reads, made by the
// person who started the run.
func TestOutwardRunsWhenOptedIn(t *testing.T) {
	s := openLoopStore(t)
	f := forge.NewFake()
	st := startTriggeredWith(t, s, "opt", outwardBlueprint(t, action.ForgeOpenPR), true, taskIdentity())
	dep := Deps{Store: s, Actions: func(string) *action.Services { return svcWith(f) }}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	sawOpen := false
	for _, c := range f.Calls() {
		if strings.Contains(c.Method, "OpenPR") {
			sawOpen = true
		}
	}
	if !sawOpen {
		saved, _ := Pinned(context.Background(), s, st.ID)
		t.Fatalf("allowOutward did not lift the outward action: fake calls %v, status %q reason %q", f.Calls(), saved.Status, saved.LastReason)
	}
}

// TestIrreversibleDeniesToo — and the opt-in does not change it. Driven:
// the loop reaches forge.merge with allowOutward declared, and the
// denial arrives ANYWAY — naming both the class and the fact that the
// flag did not lift it (the operator who set it must learn which door
// said no from the block reason, not from reading the source).
func TestIrreversibleDeniesToo(t *testing.T) {
	s := openLoopStore(t)
	f := forge.NewFake()
	st := startTriggered(t, s, "irrev", outwardBlueprint(t, action.ForgeMerge), true)
	dep := Deps{Store: s, Actions: func(string) *action.Services { return svcWith(f) }}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" {
		t.Fatalf("irreversible ran (status %q) with allowOutward declared — the opt-in was supposed to stop at outward", saved.Status)
	}
	if !strings.Contains(saved.LastReason, "irreversible") {
		t.Errorf("the block must name the irreversible class, got %q", saved.LastReason)
	}
	if !strings.Contains(saved.LastReason, "allowOutward") {
		t.Errorf("the block must say the declared opt-in did not lift it, got %q", saved.LastReason)
	}
	for _, c := range f.Calls() {
		if strings.Contains(c.Method, "Merge") {
			t.Fatalf("forge.merge executed despite the deny (%v)", c)
		}
	}
}

// TestAllowOutwardIsPerLoop — two loops from one "key" (one store, one
// daemon wiring, one caller — everything about them identical except the
// field in the start body): one runs the outward action, one blocks. If
// the grant lived on anything standing — a key, a config, a cache — this
// pair would behave the same, and the test would fail.
func TestAllowOutwardIsPerLoop(t *testing.T) {
	s := openLoopStore(t)
	// One fake per loop's Deps, built AT CONSTRUCTION and shared by the
	// closure — the witness is the fake the dispatch actually used, not
	// a fresh one built after the fact.
	fakeG, fakeD := forge.NewFake(), forge.NewFake()
	depG := Deps{Store: s, Actions: func(string) *action.Services { return svcWith(fakeG) }}
	depD := Deps{Store: s, Actions: func(string) *action.Services { return svcWith(fakeD) }}
	granted := startTriggeredWith(t, s, "granted", outwardBlueprint(t, action.ForgeOpenPR), true, taskIdentity())
	denied := startTriggered(t, s, "denied", outwardBlueprint(t, action.ForgeOpenPR), false)

	if err := Run(context.Background(), depG, granted.ID); err != nil {
		t.Fatal(err)
	}
	sawOpen := false
	for _, c := range fakeG.Calls() {
		if strings.Contains(c.Method, "OpenPR") {
			sawOpen = true
		}
	}
	if err := Run(context.Background(), depD, denied.ID); err != nil {
		t.Fatal(err)
	}
	blocked, err := Pinned(context.Background(), s, denied.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !sawOpen {
		t.Error("the opted-in loop did not run its outward action — per-loop grant is half-true if the grant never lifts anything")
	}
	if blocked.Status != "blocked" {
		t.Errorf("the non-opted-in loop did not block (%s) — a grant that leaks across loops from one key is the O2 failure this gate exists for", blocked.Status)
	}
	// And the rows say what they say: one column each, not a shared
	// flag in some shared place.
	ga, err := s.LoopAllowOutward(context.Background(), granted.ID)
	da, err2 := s.LoopAllowOutward(context.Background(), denied.ID)
	if err != nil || err2 != nil {
		t.Fatal(err, err2)
	}
	if !ga || da {
		t.Errorf("the pinned rows diverge from behaviour: granted=%v denied=%v", ga, da)
	}
}

// svcForgeCalls reaches the forge fake inside a Deps built by this file.
// (The Actions closure builds services on demand; each call site in this
// file made exactly one, so the last-built fake's log is complete.)
func svcForgeCalls(d Deps) []forge.Call {
	f := lastFake
	svc := d.Actions("")
	_ = svc
	return f.Calls()
}

// The Actions closures above hand out fresh services per call; the tests
// need the fake the DISPATCH actually used. Simplest honest answer: one
// fake per Deps, built at construction.
var lastFake *forge.Fake

// TestQ4Recorded — how attendedness arrives (Q4): the ONLY way a caller
// sets it is the start body (one field on loop.start's params); there is
// no later door. Asserted mechanically: `Attended` as a caller-settable
// param appears in exactly one Params struct in the protocol package.
func TestQ4Recorded(t *testing.T) {
	files, err := filepath.Glob("../protocol/*.go")
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]int{}
	for _, fp := range files {
		if strings.HasSuffix(fp, "_test.go") {
			continue
		}
		src, err := os.ReadFile(fp)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if strings.Contains(trimmed, "Attended") && strings.Contains(trimmed, "json:\"attended") {
				owners[fp]++
			}
		}
	}
	// S24 T7 (dated amendment, 2026-10-29): the queue's append params are
	// the SAME caller declaration, made earlier — design.md D5/D6 make
	// attendedness a per-item DECLARATION (never inferred, declared-or-
	// NULL), and CG3 says an item IS a loop.start payload held in a queue;
	// materialization carries the row's declaration into the pin. That is
	// still G14's law — the value is declared by the caller at the moment
	// of intent and never SET after the pin — so the door stays one act
	// with two moments. The guard now pins BOTH files by name and by the
	// params type they own, which is stricter than counting.
	//
	// S26 T4 (dated amendment, 2026-10-29): the roadmap's dispatch params
	// are the SAME act at the same moment — S26's R4 makes an item a
	// loop.start payload (identical to a queue row's), and the ideate/
	// materialize seams carry the declaration THROUGH the loop.start door
	// into the pin and nowhere else: no roadmap call reaches an already-
	// pinned loop's attendedness. Same law, so the same by-name pin
	// extends: three files, each a params type that FEEDS the one door.
	//
	// S60 T8 (dated amendment, 2026-11-04): the merge queue's enqueue params
	// are the SAME act at the same moment — an item is enqueued with its
	// attendedness DECLARED (never inferred), the row carries the declaration,
	// and startLandingLoop hands it to the loop.start pin and nowhere else;
	// no merge-queue call reaches an already-pinned loop's attendedness. T0
	// recorded this re-walk as planned. Same law, so the same by-name pin
	// extends once more: four files, each a params type that FEEDS the one
	// door. (The view types that ANSWER attendedness are spelled Attends, so
	// they never count: see protocol/environment.go.)
	if len(owners) != 4 {
		t.Fatalf("attendedness is caller-settable in %d files (%v); want exactly four — the start params, the queue's append params, the roadmap's dispatch params and the merge queue's enqueue params (a fifth door would let attendedness be set AFTER the pin, which G14 forbids)", len(owners), owners)
	}
	for fp := range owners {
		if !strings.HasSuffix(fp, "blueprint_loop.go") && !strings.HasSuffix(fp, "queue.go") && !strings.HasSuffix(fp, "roadmap.go") && !strings.HasSuffix(fp, "mergequeue.go") {
			t.Errorf("attended-settable file %s is not one of the four declared doors", fp)
		}
	}
	// And the four files are the start of a loop, the queue's append,
	// the roadmap's dispatch and the merge queue's enqueue.
	var seen [4]bool
	for fp := range owners {
		src, _ := os.ReadFile(fp)
		if strings.Contains(string(src), "LoopStartParams") {
			seen[0] = true
		}
		if strings.Contains(string(src), "QueueAppendParams") {
			seen[1] = true
		}
		if strings.Contains(string(src), "RoadmapMaterializeParams") {
			seen[2] = true
		}
		if strings.Contains(string(src), "MergeQueueEnqueueParams") {
			seen[3] = true
		}
	}
	if !seen[0] {
		t.Error("one attended-settable file is not the loop-start params")
	}
	if !seen[1] {
		t.Error("one attended-settable file is not the queue's append params")
	}
	if !seen[2] {
		t.Error("one attended-settable file is not the roadmap's dispatch params")
	}
	if !seen[3] {
		t.Error("one attended-settable file is not the merge queue's enqueue params")
	}
}

func ownersKey(m map[string]int) string {
	for k := range m {
		return k
	}
	return ""
}

// TestAttendednessCannotBeInferredFromPolling — G14's loop-side half:
// reads are reads. A client polls the pinned attendedness a hundred
// times and the loop is no more attended than it was; and the store has
// no door that writes attendedness after creation (the UPDATE grep pins
// the absence — the daemon's liveness is not a fact the store can even
// be told).
func TestAttendednessCannotBeInferredFromPolling(t *testing.T) {
	s := openLoopStore(t)
	st := startTriggered(t, s, "poll", outwardBlueprint(t, action.ForgeOpenPR), false)
	for i := 0; i < 100; i++ {
		att, err := s.LoopAttended(context.Background(), st.ID)
		if err != nil {
			t.Fatal(err)
		}
		if att == nil || *att {
			t.Fatalf("poll %d saw attendedness change to %v — reads do not attend a loop (G14)", i, att)
		}
	}
	// No write path exists to change it anyway.
	src, err := os.ReadFile("../store/loops.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(src), "\n") {
		up := strings.ToUpper(line)
		if strings.Contains(up, "UPDATE LOOP") && strings.Contains(up, "ATTENDED") {
			t.Errorf("attendedness is written after creation: %q — the pin would be a suggestion", line)
		}
	}
}
