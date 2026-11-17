package loop

// verify_loop_test.go — S57 T6 loop-driven gates (VG7): a test that READS
// the route proves the route is written; these DRIVE the loop. Each one
// starts a real loop (real store, real counters, real exhaustion) and runs
// verify.run through the real engine, then asserts what the engine did
// with the run's outcome.
//
// The verify service is a store-free fake — the action package is
// store-free by construction (action/verify_actions.go says so), and the
// loop tests import the loop's fake seams the way they import the forge
// fake. What the fake cannot fake is the loop: the rework, the counter,
// the ceiling and the block are the engine's, run against the real store.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
	"github.com/cjlapao/aeon/internal/verify"
)

// verifyFixturePath is the S57 ladder in the blueprint package's
// testdata (NOT an embedded blueprint — the dev-loop's verify rung is
// CR-BP1's and stays a model step until that task lands).
const verifyFixturePath = "../blueprint/testdata/verify-run-fixture.yaml"

const fakeHead = "0123456789abcdef0123456789abcdef01234567"

// --- the store-free fake seams --------------------------------------------

type fakeComponent struct{}

func (fakeComponent) ComponentForRoot(ctx context.Context, root string) (string, error) {
	return "c1", nil
}

type fakeGates struct{ gates []verify.GateSpec }

func (f fakeGates) EffectiveGates(ctx context.Context, componentID string) ([]verify.GateSpec, error) {
	if componentID != "c1" {
		return nil, fmt.Errorf("unknown component %s", componentID)
	}
	return f.gates, nil
}

type fakeDoD struct{}

func (fakeDoD) MaterializeDoD(ctx context.Context, loopSlug, componentRoot string) (int, error) {
	return 0, nil
}

// fakeLedger is the RunRecorder half: the attempt count is the ledger's
// (D-T6-8) and the runs it holds are what the action CLAIMED — the
// assertions read the claim, not the store.
type fakeLedger struct {
	runs []action.RecordRun
}

func (l *fakeLedger) CountLoopRuns(ctx context.Context, loopSlug, componentRoot string) (int, error) {
	return len(l.runs), nil
}

func (l *fakeLedger) Record(ctx context.Context, in action.RecordRun) (string, error) {
	l.runs = append(l.runs, in)
	return fmt.Sprintf("vr-%d", len(l.runs)), nil
}

// verifyHarness is the scripted verify service: one gate by default, and
// a spawn the test sets to pass, fail or never-start.
type verifyHarness struct {
	gates   []verify.GateSpec
	deny    bool // the policy seam refuses: nothing may spawn
	fail    bool // the gate completes, exit 1
	never   bool // the gate does not start (a spawn error)
	spawned int
	ledger  *fakeLedger
}

func (h *verifyHarness) spawn(ctx context.Context, command string, opts tools.SpawnOptions, tok policy.AllowToken,
	_ execenv.ChildEnv) (tools.RunResult, error) {
	h.spawned++
	switch {
	case h.never:
		return tools.RunResult{}, fmt.Errorf("spawn refused by the test")
	case h.fail:
		return tools.RunResult{ExitCode: 1, Stderr: "the gate is red"}, nil
	}
	return tools.RunResult{ExitCode: 0}, nil
}

func (h *verifyHarness) services() *action.Services {
	runner := verify.New()
	runner.Spawn = h.spawn
	if h.deny {
		runner.Verdict = func(agent, command string) (policy.Verdict, bool) { return policy.Verdict{}, true }
	} else {
		runner.Verdict = func(agent, command string) (policy.Verdict, bool) { return policy.Verdict{Allowed: true}, true }
	}
	if h.gates == nil {
		h.gates = []verify.GateSpec{{ID: "g1", Name: "build", Command: "go build ./...", Requirement: "required"}}
	}
	return &action.Services{
		Verify: &action.VerifyService{
			HeadSHA:   func(ctx context.Context, dir string) (string, error) { return fakeHead, nil },
			Component: fakeComponent{},
			Gates:     fakeGates{gates: h.gates},
			Recorder:  h.ledger,
			DoD:       fakeDoD{},
			Runner:    &runner,
			Now:       time.Now,
			Actor:     action.RunActor{Kind: "system", ID: "loop-engine"},
		},
	}
}

// okModel is the ladder's model rung: it answers, it never rejects.
func okModel(ctx context.Context, st LoopState, step blueprint.Step, words, frame string) (ModelResult, error) {
	return ModelResult{Kind: "completed", Outcome: "ok"}, nil
}

// startVerifyLoop starts a triggered loop (attended pinned false) and,
// when the blueprint iterates tasks, seeds the one task the fixture's
// ladder walks.
func startVerifyLoop(t *testing.T, s *store.Store, slug, blueprintPath string, withTask bool) LoopState {
	t.Helper()
	root := t.TempDir()
	st, err := Start(context.Background(), s, store.NewLoop{
		Slug: slug, ComponentRoot: root, Root: root, Mode: "standard",
	}, blueprintPath, boolPtr(false), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !withTask {
		return st
	}
	st.Tasks = []string{"t1"}
	sc := store.Scope{LoopSlug: slug, ComponentRoot: root}
	if _, err := s.CreateTask(sc, store.NewTask{TaskID: "t1", Title: "the verify walk"}); err != nil {
		t.Fatal(err)
	}
	raw, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePinnedRuntime(st.ID, "running", raw); err != nil {
		t.Fatal(err)
	}
	return st
}

func runLoop(t *testing.T, dep Deps, id string) {
	t.Helper()
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func pinnedLoop(t *testing.T, s *store.Store, id string) LoopState {
	t.Helper()
	saved, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

var verifyStepKey = stepKey(Position{Lane: "build", Item: "t1", Step: "verify"})

// TestVerifyRunLoopPass — the green path, driven: implement (model) then
// verify.run passes, the step completes, the ladder finishes, the loop is
// done. The evidence and the ledger row carry the run's own facts: state
// pass, the run's id, the head it measured, the engine's actor, attempt 1.
func TestVerifyRunLoopPass(t *testing.T) {
	s := openLoopStore(t)
	h := &verifyHarness{ledger: &fakeLedger{}}
	st := startVerifyLoop(t, s, "vpass", verifyFixturePath, true)
	dep := Deps{Store: s, Model: okModel, Actions: func(string) *action.Services { return h.services() }}
	runLoop(t, dep, st.ID)

	saved := pinnedLoop(t, s, st.ID)
	if saved.Status != "done" {
		t.Fatalf("status %q reason %q; want done — a passing run must finish the ladder", saved.Status, saved.LastReason)
	}
	ev := saved.Evidence[verifyStepKey]
	if ev == nil {
		t.Fatalf("no evidence for the verify step: %v", saved.Evidence)
	}
	if ev["verify_state"] != "pass" || ev["verify_run_id"] == "" || ev["verify_head_sha"] != fakeHead {
		t.Errorf("evidence %v; want state pass, a run id, the fake head", ev)
	}
	if h.spawned != 1 {
		t.Errorf("spawned %d times; want 1 — one run, one gate, one spawn", h.spawned)
	}
	if len(h.ledger.runs) != 1 {
		t.Fatalf("ledger holds %d runs; want 1", len(h.ledger.runs))
	}
	run := h.ledger.runs[0]
	if run.State != "pass" || run.Attempt != 1 || run.Trigger != "loop" || run.Actor.Kind != "system" || run.HeadSHA != fakeHead {
		t.Errorf("recorded run %+v; want pass, attempt 1, trigger loop, the engine's actor, the fake head", run)
	}
	if run.Digest == "" {
		t.Error("the run carries no digest — VG4's content identity is the rule's input")
	}
}

// TestVerifyRunLoopFail — the red path, driven to exhaustion: the gate is
// red on every try, on_reject sends the ladder back to implement, and the
// edge's counter (verify_rounds, max 2) is what finally blocks the loop —
// with then: block's exact clause, not a paraphrase.
func TestVerifyRunLoopFail(t *testing.T) {
	s := openLoopStore(t)
	h := &verifyHarness{fail: true, ledger: &fakeLedger{}}
	st := startVerifyLoop(t, s, "vfail", verifyFixturePath, true)
	dep := Deps{Store: s, Model: okModel, Actions: func(string) *action.Services { return h.services() }}
	runLoop(t, dep, st.ID)

	saved := pinnedLoop(t, s, st.ID)
	if saved.Status != "blocked" {
		t.Fatalf("status %q reason %q; want blocked — a gate that never passes cannot finish the ladder", saved.Status, saved.LastReason)
	}
	if !strings.Contains(saved.LastReason, "verify_rounds") || !strings.HasSuffix(saved.LastReason, "/ then: block") {
		t.Errorf("block reason %q; want the edge's counter clause and then: block verbatim", saved.LastReason)
	}
	if h.spawned != 3 {
		t.Errorf("spawned %d times; want 3 — two reworks, three runs, the third the one that exhausted", h.spawned)
	}
	if len(h.ledger.runs) != 3 {
		t.Fatalf("ledger holds %d runs; want 3", len(h.ledger.runs))
	}
	for i, run := range h.ledger.runs {
		if run.State != "fail" || run.Attempt != i+1 {
			t.Errorf("run %d recorded %+v; want fail, attempt %d", i, run, i+1)
		}
	}
	// The rework carried the run's own summary back to implement (L32's
	// redo: the re-entered step knows why it was sent back) — the stored
	// step state says it, and its reason is the fixed template, never
	// prose the engine invented.
	ss := saved.StepStates[verifyStepKey]
	if !strings.HasPrefix(ss.Reason, "verify: ") || !strings.Contains(ss.Reason, "1 fail") {
		t.Errorf("the verify step's stored reason %q; want the run's summary under the verify: prefix", ss.Reason)
	}
}

// TestVerifyRunLoopUnverified — the third state, driven: the gate does
// not complete (a spawn error is a non-completion), so the run is
// unverified — and an unverified reject routes by the LOOP ceiling, never
// by the step's counter (VG7's guard). The loop is blocked on the ceiling
// sentence, and every stored step state keeps the unverified: prefix, so
// the record a person reads afterwards says the check did not run.
func TestVerifyRunLoopUnverified(t *testing.T) {
	s := openLoopStore(t)
	h := &verifyHarness{never: true, ledger: &fakeLedger{}}
	st := startVerifyLoop(t, s, "vunver", verifyFixturePath, true)
	dep := Deps{Store: s, Model: okModel, Actions: func(string) *action.Services { return h.services() }}
	runLoop(t, dep, st.ID)

	saved := pinnedLoop(t, s, st.ID)
	if saved.Status != "blocked" {
		t.Fatalf("status %q reason %q; want blocked — an unverified gate never passes, so the ladder cannot finish", saved.Status, saved.LastReason)
	}
	if !strings.Contains(saved.LastReason, fmt.Sprintf("total_failed_gates %d/%d", LoopCeiling, LoopCeiling)) {
		t.Errorf("block reason %q; want the loop-ceiling clause — the unverified path routes by the ceiling, not by the step's counter", saved.LastReason)
	}
	if h.spawned != LoopCeiling {
		t.Errorf("spawned %d times; want %d — one run per rework, until the ceiling", h.spawned, LoopCeiling)
	}
	for key, ss := range saved.StepStates {
		if strings.HasSuffix(key, "verify") && !strings.HasPrefix(ss.Reason, UnverifiedPrefix) {
			t.Errorf("step %s stored reason %q without the unverified: prefix — the record must say the check did not run", key, ss.Reason)
		}
	}
}

// TestVerifyRunLoopNoOnReject — the missing-edge shape: a verify rung
// with NO on_reject (and a lane with no rework) gets a red run, and the
// loop blocks naming verify-failed and the run's own summary. This is the
// shape a hand-written blueprint has until someone adds the edge — the
// block, not a silent re-run, is what the absence of routing means.
func TestVerifyRunLoopNoOnReject(t *testing.T) {
	s := openLoopStore(t)
	h := &verifyHarness{fail: true, ledger: &fakeLedger{}}
	path := filepath.Join(t.TempDir(), "no-edge.yaml")
	writeBlueprint(t, path, "  - id: check\n    step: {kind: code-action, action: verify.run}\n")
	st := startVerifyLoop(t, s, "noedge", path, false)
	dep := Deps{Store: s, Actions: func(string) *action.Services { return h.services() }}
	runLoop(t, dep, st.ID)

	saved := pinnedLoop(t, s, st.ID)
	if saved.Status != "blocked" {
		t.Fatalf("status %q reason %q; want blocked — a red run with no edge to route on must stop the loop", saved.Status, saved.LastReason)
	}
	if !strings.Contains(saved.LastReason, "1 fail") {
		t.Errorf("block reason %q; want the run's own summary", saved.LastReason)
	}
	if len(h.ledger.runs) != 1 || h.ledger.runs[0].State != "fail" {
		t.Errorf("ledger %+v; want one recorded fail run", h.ledger.runs)
	}
}

// TestVerifyRunLoopNoGates — an honest empty set is not an error: the
// loop runs the step, the runner reports unverified with the no-gates
// reason, and the same missing-edge shape blocks on it. The distinction
// the claim rests on: a node with no gates does not pass its own ladder —
// a check that was never run proves nothing, and the loop says so.
func TestVerifyRunLoopNoGates(t *testing.T) {
	s := openLoopStore(t)
	// An explicit EMPTY set, not nil: nil is the harness's "use the
	// default gate" marker, and the honest empty set is the whole point.
	h := &verifyHarness{gates: []verify.GateSpec{}, ledger: &fakeLedger{}}
	path := filepath.Join(t.TempDir(), "no-gates.yaml")
	writeBlueprint(t, path, "  - id: check\n    step: {kind: code-action, action: verify.run}\n")
	st := startVerifyLoop(t, s, "nogates", path, false)
	dep := Deps{Store: s, Actions: func(string) *action.Services { return h.services() }}
	runLoop(t, dep, st.ID)

	saved := pinnedLoop(t, s, st.ID)
	if saved.Status != "blocked" {
		t.Fatalf("status %q reason %q; want blocked — no gates means no proof, not a pass", saved.Status, saved.LastReason)
	}
	if saved.LastReason != "0 gates: 0 pass, 0 fail (required), 0 unverified" {
		t.Errorf("block reason %q; want the run's own summary of the empty set", saved.LastReason)
	}
	if h.spawned != 0 {
		t.Errorf("spawned %d times with zero gates; want 0 — the runner aggregates, it does not run nothing", h.spawned)
	}
	if len(h.ledger.runs) != 1 || h.ledger.runs[0].State != "unverified" || h.ledger.runs[0].Reason != "unverified: no-gates-configured" {
		t.Errorf("ledger %+v; want one recorded unverified run naming the no-gates reason", h.ledger.runs)
	}
}

// TestVerifyRunLoopPolicyDenied — the policy seam's denial is a
// non-completion: nothing spawns (the witness is the spawn counter — zero
// is the claim), the run is recorded unverified with the policy reason,
// and the loop routes it as the unverified reject it is.
func TestVerifyRunLoopPolicyDenied(t *testing.T) {
	s := openLoopStore(t)
	h := &verifyHarness{deny: true, ledger: &fakeLedger{}}
	st := startVerifyLoop(t, s, "vdeny", verifyFixturePath, true)
	dep := Deps{Store: s, Model: okModel, Actions: func(string) *action.Services { return h.services() }}
	runLoop(t, dep, st.ID)

	saved := pinnedLoop(t, s, st.ID)
	if h.spawned != 0 {
		t.Fatalf("spawned %d times under a policy denial — a deny spawns nothing, and the counter is the proof", h.spawned)
	}
	if saved.Status != "blocked" {
		t.Fatalf("status %q reason %q; want blocked — a denied gate never completes", saved.Status, saved.LastReason)
	}
	if len(h.ledger.runs) != LoopCeiling {
		t.Fatalf("ledger holds %d runs; want %d — the denial routes as an unverified reject, until the ceiling", len(h.ledger.runs), LoopCeiling)
	}
	for i, run := range h.ledger.runs {
		if run.State != "unverified" || run.Reason != "unverified: policy" {
			t.Errorf("run %d recorded %+v; want unverified with the policy word", i, run)
		}
	}
	ss := saved.StepStates[verifyStepKey]
	if !strings.HasPrefix(ss.Reason, UnverifiedPrefix) {
		t.Errorf("stored reason %q; want the unverified: prefix — the record a person reads must say the check did not run", ss.Reason)
	}
}
