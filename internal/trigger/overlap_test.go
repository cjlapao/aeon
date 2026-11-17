package trigger

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"
)

// The Q2 ruling, named. This test PASSES under exactly one reading: if
// somebody flips StillGoing so "suspended" no longer blocks, or so that
// "orphaned" blocks, it fails — which is the whole point of the task file's
// "a test that passes under both readings has not tested the decision".
func TestSuspendedPreviousRunBlocksTheNextFire(t *testing.T) {
	// The task file's gate name, answered on the REAL vocabulary (see
	// overlap.go): the design's "suspended" is step-speak; the loop row's
	// suspended-on-a-question state is `waiting`. This test passes under
	// exactly one reading — flip the predicate and one of these fatals
	// fires.
	if !StillGoing("running") {
		t.Fatal("a running previous run must block (TR5's core case)")
	}
	if !StillGoing("waiting") {
		t.Fatal("Q2: a previous run WAITING on a question blocks — a human owes it an answer, and a second loop would make that question stale")
	}
	if !StillGoing("blocked") {
		t.Fatal("Q2's sibling: a previous run blocked at a wall (outward/policy) blocks — a second copy duplicates the wall a human is meant to find")
	}
	if StillGoing("done") || StillGoing("aborted") {
		t.Fatal("finished states own nothing")
	}
	if StillGoing("suspended") || StillGoing("orphaned") {
		t.Fatal("step-table words on a loop row are unknowns — they must not silence a schedule forever")
	}
}

func TestStillGoingIsTheEngineRowsVocabulary(t *testing.T) {
	// The loop row's status has no CHECK — the engine (SavePinnedRuntime)
	// owns the set. This list asserts the predicate's answer for EVERY
	// status the engine writes, so the day advance.go adds one, this test
	// fails and the decision is made in review, not in a silent fall-through.
	engine := []string{"running", "waiting", "blocked", "done", "aborted"}
	blocking := map[string]bool{"running": true, "waiting": true, "blocked": true}
	for _, status := range engine {
		if StillGoing(status) != blocking[status] {
			t.Errorf("StillGoing(%q)=%v, want %v", status, StillGoing(status), blocking[status])
		}
	}
}

// Per-schedule independence, over one shared instant: two schedules whose
// previous runs are in different states must not cross-contaminate.
func TestTwoDifferentSchedulesMayFireAtTheSameInstant(t *testing.T) {
	at := london(t, "2026-10-27 03:30:00")
	a := sched("alpha", "30 3 * * *")
	b := sched("beta", "30 3 * * *")
	covered := at.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	a.CoveredUntil, b.CoveredUntil = covered, covered
	st := &fakeStore{scheds: []Schedule{a, b}, covered: map[string]string{}}
	fa := &fakeStarter{id: "loop-new"}
	sc := newSched(st, fa)
	// alpha's previous run is still going; beta's is done.
	sc.Gate.Overlap = func(s Schedule, _ time.Time) (bool, string) {
		if s.ID == "alpha" {
			return true, "loop-old-alpha"
		}
		return false, ""
	}
	if err := sc.Tick(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if fa.call != 1 {
		t.Fatalf("want beta fired once while alpha skipped, got %d starts", fa.call)
	}
	if fa.got[0].TriggerID != "beta" {
		t.Fatalf("the wrong schedule fired: %q", fa.got[0].TriggerID)
	}
	var skipped, fired int
	for _, f := range st.fires {
		switch f.Outcome {
		case OutcomeSkipped:
			skipped++
			if f.TriggerID != "alpha" || !strings.Contains(f.Reason, "loop-old-alpha") {
				t.Fatalf("TR5: the skip names the schedule's previous run: %+v", f)
			}
		case OutcomeFired:
			fired++
			if f.TriggerID != "beta" {
				t.Fatalf("fired record on the wrong schedule: %+v", f)
			}
		}
	}
	if skipped != 1 || fired != 1 {
		t.Fatalf("want one skipped + one fired, got %+v", st.fires)
	}
}

func TestSkipIsDistinctFromDidNotFire(t *testing.T) {
	if OutcomeSkipped.Render() == OutcomeDidNotFire.Render() {
		t.Fatal("the two answers must read differently — the gap's WHY is the record's purpose")
	}
	if OutcomeSkipped.Render() != "skipped" || OutcomeDidNotFire.Render() != "did not fire" {
		t.Fatalf("renderings drifted: %q vs %q", OutcomeSkipped.Render(), OutcomeDidNotFire.Render())
	}
}

// N2, asserted against the package rather than remembered: there is no
// pending-fire structure to queue into. The scan reads the package's
// identifiers — a field, method or type that speaks queue/pending/backlog
// would be queueing wearing a schedule's clothes.
func TestNothingIsQueued(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	forbidden := []string{"queue", "pending", "backlog", "deferred", "requeue", "retry"}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fs, e.Name(), nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.StructType:
				for _, fld := range x.Fields.List {
					for _, name := range fld.Names {
						for _, tok := range forbidden {
							if strings.Contains(strings.ToLower(name.Name), tok) {
								t.Errorf("%s: field %q speaks %q — N2: skipping is the decision; queueing is S27's", e.Name(), name.Name, tok)
							}
						}
					}
				}
			case *ast.FuncDecl:
				for _, tok := range forbidden {
					if strings.Contains(strings.ToLower(x.Name.Name), tok) {
						t.Errorf("%s: function %s speaks %q — no retry/queue path from the tick (design's no-retry, N2)", e.Name(), x.Name.Name, tok)
					}
				}
			}
			return true
		})
	}
}
