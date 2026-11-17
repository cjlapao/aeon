package blueprint

import (
	"os"
	"testing"

	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/store"
)

// S17 T7 — the ladder's AUTHORSHIP, reviewed as data. S8 shipped
// dev-loop.yaml as data that validates; its nine `unverifiable`
// diagnostics were code actions, S16 retired those, and T7 makes every
// lane RUN. These are the structural half of that ruling (the executing
// half lives in internal/loop/dev-loop_test.go, where the engine and the
// fake forge are reachable). The shipped fixture pins the UNVERIFIABLE
// set at []; these tests pin the rest of the shape.

func devLoopRaw(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("blueprints/dev-loop.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func devLoopValidated(t *testing.T, mode string) (Blueprint, Report) {
	t.Helper()
	raw := devLoopRaw(t)
	report := Analyze(raw, ValidationOptions{Mode: mode, Agents: handler.Names(), Resolvers: handler.Resolvers(), Actions: s16ActionSet})
	bp, err := Load(raw)
	if err != nil {
		t.Fatalf("the shipped ladder does not load: %v", err)
	}
	return bp, report
}

// TestOneApprovalGate — the protocol's own summary: "investigates → plans
// → (one approval gate) → builds task-by-task → verifies each task →
// commits, running unattended." A ladder with four routine gates is not
// running unattended; it is a wizard. Escalations open other questions,
// but they are exceptions the machinery raises, not scheduled steps.
func TestOneApprovalGate(t *testing.T) {
	bp, _ := devLoopValidated(t, "standard")
	gates := []string{}
	for _, lane := range bp.Lanes {
		if lane.When != nil {
			continue // a conditional lane is an exception, not routine
		}
		if lane.Step != nil && lane.Step.Kind == Gate {
			gates = append(gates, lane.ID)
		}
		for _, step := range lane.Ladder {
			if step.Kind == Gate {
				gates = append(gates, lane.ID+"/"+step.ID)
			}
		}
	}
	if len(gates) != 1 || gates[0] != "plan-gate" {
		t.Fatalf("routine gates = %v, want exactly [plan-gate] — more than one routine gate is a wizard, not an unattended loop", gates)
	}
	// And the one gate answers with the closed set, abort first-class.
	for _, lane := range bp.Lanes {
		if lane.ID == "plan-gate" {
			q, ok := bp.Questions[lane.Step.Question]
			if !ok {
				t.Fatalf("plan-gate asks %q, which is not declared", lane.Step.Question)
			}
			want := map[string]bool{"approve": true, "revise": true, "abort": true}
			if len(q.Answers) != 3 {
				t.Fatalf("approve-plan answers %v, want exactly [approve, revise, abort]", q.Answers)
			}
			for _, a := range q.Answers {
				if !want[a] {
					t.Fatalf("approve-plan carries answer %q, outside the closed set", a)
				}
			}
		}
	}
}

// TestEverySetsIsLegalInEveryMode — Q7, answered with the table: a
// `sets:` legal in the default table and illegal under the POC overlay
// fails at a task's third step, in a mode nobody tested.
func TestEverySetsIsLegalInEveryMode(t *testing.T) {
	bp, _ := devLoopValidated(t, "standard")
	sets := map[string][]string{} // status → where it is declared
	for _, lane := range bp.Lanes {
		steps := lane.Ladder
		if lane.Step != nil {
			steps = append(steps, *lane.Step)
		}
		for _, step := range steps {
			if step.Sets != "" {
				sets[step.Sets] = append(sets[step.Sets], lane.ID+"/"+step.ID)
			}
		}
	}
	if len(sets) == 0 {
		t.Fatal("the ladder sets no task status anywhere — the enumeration is vacuous")
	}
	for _, mode := range []store.LoopMode{store.ModeSimple, store.ModeStandard, store.ModePoc} {
		table := store.TransitionsFor(mode)
		for status, where := range sets {
			to := store.TaskStatus(status)
			if !knownStatus(to) {
				t.Fatalf("lane %s sets %q, which is not a task status at all", where[0], status)
			}
			reachable := false
			for _, from := range store.TaskStatuses() {
				for _, allowed := range table[from] {
					if allowed == to {
						reachable = true
					}
				}
			}
			if !reachable {
				t.Fatalf("mode %s: %s sets %q, but no legal edge in that mode's table reaches it — a step would block mid-ladder in a mode nobody walked", mode, where[0], status)
			}
		}
	}
}

func knownStatus(to store.TaskStatus) bool {
	for _, s := range store.TaskStatuses() {
		if s == to {
			return true
		}
	}
	return false
}

// TestNoNewMechanismWasAdded — the enumeration that keeps authorship
// honest: every lane maps to an existing handler (or the by-tech resolver),
// an existing S16 action, or a declared gate question. A capability that
// fits none of those is NOT a lane — it is a finding (N1, D15).
func TestNoNewMechanismWasAdded(t *testing.T) {
	bp, _ := devLoopValidated(t, "standard")
	agents := map[string]bool{}
	for _, n := range handler.Names() {
		agents[n] = true
	}
	for _, r := range handler.Resolvers() {
		agents[r] = true
	}
	actions := map[string]bool{}
	for _, a := range s16ActionSet {
		actions[a] = true
	}
	for _, lane := range bp.Lanes {
		steps := lane.Ladder
		if lane.Step != nil {
			steps = append(steps, *lane.Step)
		}
		if len(steps) == 0 {
			t.Fatalf("lane %s has no step and no ladder — nothing would run", lane.ID)
		}
		for _, step := range steps {
			switch step.Kind {
			case ModelStep:
				if !agents[step.Agent] {
					t.Fatalf("lane %s step %s names agent %q: not a handler, not a resolver — S8–S16 did not build it, so this is a FINDING, not a lane", lane.ID, step.ID, step.Agent)
				}
			case CodeAction:
				if !actions[step.Action] {
					t.Fatalf("lane %s step %s names action %q: outside the closed S16 set (D14)", lane.ID, step.ID, step.Action)
				}
			case Gate:
				if _, ok := bp.Questions[step.Question]; !ok {
					t.Fatalf("lane %s asks %q, undeclared", lane.ID, step.Question)
				}
			default:
				t.Fatalf("lane %s step %s has kind %q — the format knows three", lane.ID, step.ID, step.Kind)
			}
		}
	}
}

// TestValidatesStrict — the ladder passes `--strict` (invalid AND
// unverifiable are failures there), in EVERY mode, now that every lane
// runs.
func TestValidatesStrict(t *testing.T) {
	for _, mode := range []string{"simple", "standard", "poc"} {
		_, report := devLoopValidated(t, mode)
		if !report.OK(true) {
			for _, d := range report.Diagnostics {
				t.Errorf("mode %s: %s %s (%s): %s", mode, d.State, d.Path, d.Kind, d.Msg)
			}
			t.Fatalf("the shipped ladder is not --strict clean in mode %s", mode)
		}
	}
}

// TestLadderMatchesTheProtocolShape — the diagram, as data: the top-level
// ladder is the protocol's phase rows in order, and build is the one
// iterating lane.
func TestLadderMatchesTheProtocolShape(t *testing.T) {
	bp, _ := devLoopValidated(t, "standard")
	want := []string{"clarify", "discovery", "legal", "plan", "plan-gate", "git-feature-start", "build", "qa-specs", "qa", "memory", "git-abort"}
	got := []string{}
	for _, lane := range bp.Lanes {
		got = append(got, lane.ID)
	}
	if len(got) != len(want) {
		t.Fatalf("lanes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("lane %d is %q, want %q", i, got[i], want[i])
		}
	}
	iterating := 0
	for _, lane := range bp.Lanes {
		if lane.Iterate != "" {
			iterating++
			if lane.ID != "build" {
				t.Fatalf("lane %s iterates; build is the one iterating lane (L27)", lane.ID)
			}
			if lane.Rework != "implement" {
				t.Fatalf("build rework = %q, want implement", lane.Rework)
			}
		}
	}
	if iterating != 1 {
		t.Fatalf("%d iterating lanes, want exactly one", iterating)
	}
}
