package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// S17 T6 — verify's three outcomes (L33, D8, D9, O4, Q8). The protocol's
// wording IS the specification: "lint is unverified, which is not the same
// as clean. Do not report verification=pass on an unverified gate." This is
// P31's FIFTH instance — the one where the collapse looks like a green
// check.
//
// The three route as: pass → ok, fail → reject, unverified → reject with a
// DISTINCT reason (not a fifth handoff outcome — L4's four stand). The two
// redos differ: fail says FIX the finding, unverified says MAKE THE CHECK
// RUN. A tool that will not run is not the handler's failure (O4): the
// task's retries go untouched; per Q8 the ONLY counter charged is the loop
// ceiling's total_failed_gates — which is also what bounds it, so an
// environment that never recovers cannot loop forever.
//
// The engine consumes bash's oomKilled fact through the handoff's
// structured verification field, rendered to the canonical "unverified: "
// reason by the handler layer (handoff.go). The engine never parses prose:
// a reject whose text merely MENTIONS 137/OOM is accounted as a fail.

const (
	unverifiedOOM     = "unverified: golangci-lint was KILLED (the bash fact, not a diagnosis) — the check did not run; make the check run"
	unverifiedTimeout = "unverified: go vet TIMED OUT after 120s — the check did not run; make the check run"
	unverifiedAbsent  = "unverified: golangci-lint is not installed — the check did not run; make the check run"
	failFinding       = "field 'generated_at' missing from the json output"
)

func verifyRunner(failsWith string, times int) ModelRunner {
	n := 0
	return func(_ context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		if step.ID == "verify" {
			n++
			if n <= times {
				return ModelResult{Kind: "completed", Outcome: "reject", Reason: failsWith}, nil
			}
			return ModelResult{Kind: "completed", Outcome: "ok"}, nil
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
}

// TestUnverifiedIsNeverStoredAsPass — asserted on the STORED value, not on
// a log line: the record is what a person reads afterwards, and a test on
// a log line passes while the record says pass (D8, C6, I5).
func TestUnverifiedIsNeverStoredAsPass(t *testing.T) {
	s := openLoopStore(t)
	path, root := redoBlueprint(t)
	id := startRedoLoop(t, s, path, root, "uv-store", []string{"T1"})
	var stored string
	calls := 0
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		if step.ID == "implement" {
			calls++
			if calls == 2 {
				// Mid-run, from the handler's seat: the record the FIRST
				// unverified verify left behind.
				st, err := Pinned(context.Background(), s, id)
				if err != nil {
					t.Fatal(err)
				}
				stored = st.StepStates["build/T1/verify"].Outcome
			}
		}
		if step.ID == "verify" && calls < 2 {
			return ModelResult{Kind: "completed", Outcome: "reject", Reason: unverifiedOOM}, nil
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	if stored == "" {
		t.Fatal("the spy never saw the second dispatch — the verify reject did not route back")
	}
	if stored != "unverified" {
		t.Fatalf("the verify step stored outcome %q — unverified must be STORED as unverified, and %q would be the collapse this task exists to refuse", stored, stored)
	}
}

// TestUnverifiedAndFailDifferInTheirRedo — the two send-backs are
// different work (L33, D9): fix the finding versus make the check run.
// Collapsing them wastes a round, so the REDO INPUTS are asserted unequal.
func TestUnverifiedAndFailDifferInTheirRedo(t *testing.T) {
	runRework := func(t *testing.T, reason string) string {
		t.Helper()
		s := openLoopStore(t)
		path, root := redoBlueprint(t)
		slug := "uv-redo" + strings.NewReplacer(" ", "-", ":", "-", "'", "").Replace(reason[:12])
		id := startRedoLoop(t, s, path, root, slug, []string{"T1"})
		var reworkInput string
		impl := 0
		dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, step blueprint.Step, prompt string, _ string) (ModelResult, error) {
			if step.ID == "implement" {
				impl++
				if impl == 2 {
					reworkInput = prompt
				}
			}
			if step.ID == "verify" && impl <= 1 {
				return ModelResult{Kind: "completed", Outcome: "reject", Reason: reason}, nil
			}
			return ModelResult{Kind: "completed", Outcome: "ok"}, nil
		}}
		if err := Run(context.Background(), dep, id); err != nil {
			t.Fatal(err)
		}
		if reworkInput == "" {
			t.Fatal("no rework dispatch was captured — the reject did not route back")
		}
		return reworkInput
	}
	failInput := runRework(t, failFinding)
	unvInput := runRework(t, unverifiedOOM)
	if failInput == unvInput {
		t.Fatal("the two redos carry the SAME input — fix-the-finding and make-the-check-run collapsed into one round (D9)")
	}
	if !strings.Contains(failInput, "field 'generated_at' missing") {
		t.Fatalf("the fail redo lost the finding: %q", failInput)
	}
	if !strings.Contains(unvInput, "unverified") || !strings.Contains(unvInput, "make the check run") {
		t.Fatalf("the unverified redo lost its directive: %q", unvInput)
	}
	if strings.Contains(unvInput, "retry 1 of 2") {
		t.Fatal("the unverified redo claims a retry of the task budget it never charged (O4)")
	}
}

// TestUnverifiedDoesNotChargeRetries — per Q8, the task's retries are
// UNMOVED; the only counter touched is total_failed_gates.
func TestUnverifiedDoesNotChargeRetries(t *testing.T) {
	s := openLoopStore(t)
	path, root := redoBlueprint(t)
	id := startRedoLoop(t, s, path, root, "uv-budget", []string{"T1"})
	dep := Deps{Store: s, Model: verifyRunner(unverifiedOOM, 2)}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: "uv-budget", ComponentRoot: root}
	rows, err := NewCounters(s).TaskCounters(sc, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if rows["retries"] != 0 {
		t.Fatalf("retries = %d after two unverified rejects — a tool that will not run is not the handler's failure (O4)", rows["retries"])
	}
	loop, err := NewCounters(s).LoopCounters(sc)
	if err != nil {
		t.Fatal(err)
	}
	if loop["total_failed_gates"] != 2 {
		t.Fatalf("total_failed_gates = %d, want 2 — Q8: unverified charges ONLY this counter", loop["total_failed_gates"])
	}
}

// TestUnverifiedRoutesAsReject — work cannot proceed on an unknown: the
// lane routes back exactly as a fail does (and NOT as a fifth outcome).
func TestUnverifiedRoutesAsReject(t *testing.T) {
	s := openLoopStore(t)
	path, root := redoBlueprint(t)
	id := startRedoLoop(t, s, path, root, "uv-route", []string{"T1"})
	implements := 0
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		if step.ID == "implement" {
			implements++
		}
		if step.ID == "verify" && implements == 1 {
			return ModelResult{Kind: "completed", Outcome: "reject", Reason: unverifiedOOM}, nil
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	if implements < 2 {
		t.Fatalf("the unverified reject did not route back: implement ran %d times — an unknown must not read as ok", implements)
	}
}

// TestTimeoutAndAbsentToolAreAlsoUnverified — three causes, one outcome:
// the OOM fact, a timeout, a missing tool. The distinction lives in the
// reason's cause; the ROUTING and the ACCOUNTING are one.
func TestTimeoutAndAbsentToolAreAlsoUnverified(t *testing.T) {
	for _, cause := range []string{unverifiedOOM, unverifiedTimeout, unverifiedAbsent} {
		s := openLoopStore(t)
		path, root := redoBlueprint(t)
		slug := "uv-cause"
		id := startRedoLoop(t, s, path, root, slug, []string{"T1"})
		dep := Deps{Store: s, Model: verifyRunner(cause, 1)}
		if err := Run(context.Background(), dep, id); err != nil {
			t.Fatal(err)
		}
		rows, err := NewCounters(s).TaskCounters(store.Scope{LoopSlug: slug, ComponentRoot: root}, "T1")
		if err != nil {
			t.Fatal(err)
		}
		if rows["retries"] != 0 {
			t.Fatalf("cause %q charged retries (%d) — the cause varies, the accounting does not", cause, rows["retries"])
		}
	}
}

// TestOOMIsDetectedFromTheToolNotTheProse — the outcome arrives through
// the canonical channel (the handoff's structured verification field,
// rendered by the handler layer as the "unverified: " prefix). A reject
// that merely WRITES about 137 and OOM in its prose is an ordinary fail:
// it charges retries. Prose is not a fact (W5 consumed, not re-derived).
func TestOOMIsDetectedFromTheToolNotTheProse(t *testing.T) {
	s := openLoopStore(t)
	path, root := redoBlueprint(t)
	id := startRedoLoop(t, s, path, root, "uv-prose", []string{"T1"})
	dep := Deps{Store: s, Model: verifyRunner("exit 137 signal: killed — oom killed the linter, so the output is suspect", 1)}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	rows, err := NewCounters(s).TaskCounters(store.Scope{LoopSlug: "uv-prose", ComponentRoot: root}, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if rows["retries"] != 1 {
		t.Fatalf("retries = %d: the engine PARSED the prose — unverified is a structured verdict (the handoff field), never a keyword scan", rows["retries"])
	}
}

// TestLoopCeilingBoundsRepeatedUnverified — uncharged does not mean
// unbounded: an environment that never recovers hits the loop ceiling and
// BLOCKS, with the ceiling's own sentence (Q8's second half, Q11).
func TestLoopCeilingBoundsRepeatedUnverified(t *testing.T) {
	s := openLoopStore(t)
	path, root := redoBlueprint(t)
	id := startRedoLoop(t, s, path, root, "uv-ceiling", []string{"T1"})
	dep := Deps{Store: s, Model: verifyRunner(unverifiedOOM, 100)}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	st, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "blocked" {
		t.Fatalf("status %q after 100 impossible checks — the ceiling is what bounds the uncharged (Q8)", st.Status)
	}
	if !strings.Contains(st.LastReason, "total_failed_gates 10/10") {
		t.Fatalf("blocked reason %q does not name the ceiling it hit", st.LastReason)
	}
	rows, err := NewCounters(s).TaskCounters(store.Scope{LoopSlug: "uv-ceiling", ComponentRoot: root}, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if rows["retries"] != 0 {
		t.Fatalf("retries = %d — the ceiling blocked it, the budget was never touched", rows["retries"])
	}
}
