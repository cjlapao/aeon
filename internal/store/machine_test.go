package store

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// T3's tests. Enumerated FROM the tables, never listed — a hand-written list of
// legal transitions passes forever after somebody adds one.

func TestEveryPairInAllowedTransitionsIsAccepted(t *testing.T) {
	for from, targets := range AllowedTransitions() {
		for _, to := range targets {
			res := ValidateTransition(from, to, ModeStandard)
			if !res.OK {
				t.Errorf("%s -> %s is in the table and was refused: %s", from, to, res.Reason)
			}
		}
	}
}

func TestEveryPairAbsentFromAllowedTransitionsIsRefused(t *testing.T) {
	// The cartesian product, minus the table and minus self-transitions. This
	// is the half that catches a machine that says yes to everything.
	table := AllowedTransitions()
	checked := 0
	for _, from := range TaskStatuses() {
		allowed := map[TaskStatus]bool{}
		for _, to := range table[from] {
			allowed[to] = true
		}
		for _, to := range TaskStatuses() {
			if from == to || allowed[to] {
				continue
			}
			checked++
			res := ValidateTransition(from, to, ModeStandard)
			if res.OK {
				t.Errorf("%s -> %s is NOT in the table and was accepted", from, to)
			}
		}
	}
	if checked < 100 {
		// 14 statuses: 196 pairs, minus 14 self-transitions, minus the ~33 in
		// the table. A count far below that means the enumeration broke and
		// the test is passing by checking nothing.
		t.Errorf("only %d forbidden pairs were checked, which is too few for %d statuses",
			checked, len(TaskStatuses()))
	}
}

func TestASelfTransitionIsAlwaysAllowed(t *testing.T) {
	// v1's behaviour, carried: a caller re-asserting the status it already has
	// is idempotent rather than wrong, which is what makes a retried write
	// safe.
	for _, s := range TaskStatuses() {
		if res := ValidateTransition(s, s, ModeStandard); !res.OK {
			t.Errorf("%s -> %s (itself) was refused: %s", s, s, res.Reason)
		}
	}
}

func TestARefusalNamesBothStates(t *testing.T) {
	res := ValidateTransition(StatusPending, StatusComplete, ModeStandard)
	if res.OK {
		t.Fatal("pending -> complete was accepted")
	}
	if res.From != StatusPending || res.To != StatusComplete {
		t.Errorf("the refusal carries From=%q To=%q", res.From, res.To)
	}
	for _, want := range []string{"pending", "complete"} {
		if !strings.Contains(res.Reason, want) {
			t.Errorf("the refusal does not name %q: %s", want, res.Reason)
		}
	}
	if len(res.Allowed) == 0 {
		t.Error("the refusal does not say what IS allowed, so a caller has a dead end rather than a choice")
	}
}

func TestARefusalNamesTheShortestLegalPathWhereOneExists(t *testing.T) {
	// This is what turns "transition refused" into "you need to go through
	// in-progress first" — the difference between an error a caller can act on
	// and one it can only log.
	res := ValidateTransition(StatusPending, StatusComplete, ModeStandard)
	if len(res.Path) == 0 {
		t.Fatal("complete is reachable from pending, but no path was offered")
	}
	if res.Path[len(res.Path)-1] != StatusComplete {
		t.Errorf("the path does not end at the requested status: %v", res.Path)
	}
	// And it must be WALKABLE: every consecutive pair legal, starting from From.
	prev := res.From
	for _, next := range res.Path {
		if !CanTransition(prev, next, ModeStandard) {
			t.Fatalf("the offered path is not legal: %s -> %s is refused (path %v)", prev, next, res.Path)
		}
		prev = next
	}
	if !strings.Contains(res.Reason, "only valid from 'merged'") {
		t.Errorf("the reason does not explain the one rung that surprises people: %s", res.Reason)
	}
}

func TestShortestStatusPathReturnsNothingRatherThanAPartialPath(t *testing.T) {
	// `complete`, `skipped` and `superseded` are terminal, so nothing is
	// reachable FROM them. A partial path would read as "walk these states" and
	// leave the caller somewhere they did not intend.
	for _, terminal := range []TaskStatus{StatusComplete, StatusSkipped, StatusSuperseded} {
		path := ShortestStatusPath(terminal, StatusInProgress, ModeStandard)
		if path != nil {
			t.Errorf("%s is terminal, but a path to in-progress was offered: %v", terminal, path)
		}
	}
	if path := ShortestStatusPath(StatusPending, StatusPending, ModeStandard); path != nil {
		t.Errorf("a self-path should be nothing, got %v", path)
	}
}

func TestTheShortestPathIsActuallyShortest(t *testing.T) {
	// pending -> complete has one shortest route through the PR ladder. A BFS
	// that returned the first route found rather than the shortest would pass
	// the walkability test above and still send a caller the long way.
	path := ShortestStatusPath(StatusPending, StatusComplete, ModeStandard)
	want := []TaskStatus{
		StatusInProgress, StatusVerified, StatusSecure,
		StatusPROpen, StatusInReview, StatusMerged, StatusComplete,
	}
	if len(path) != len(want) {
		t.Fatalf("path\n  got  %v\n  want %v", path, want)
	}
	for i := range want {
		if path[i] != want[i] {
			t.Fatalf("path\n  got  %v\n  want %v", path, want)
		}
	}
}

func TestPocTransitionOverridesBehaveAsV1Does(t *testing.T) {
	// Q5: still needed, ported. Without the overlay a POC task strands at
	// `secure`, because `complete` is otherwise reachable only from `merged`.
	if CanTransition(StatusSecure, StatusComplete, ModeStandard) {
		t.Error("secure -> complete is allowed in standard mode; complete must come only from merged")
	}
	if !CanTransition(StatusSecure, StatusComplete, ModePoc) {
		t.Error("secure -> complete is refused in poc mode, which strands every POC task at secure")
	}

	// What it does NOT relax: the PR rungs stay REACHABLE (a POC loop that does
	// open a PR is not refused), and `secure` still requires `verified` first.
	if !CanTransition(StatusSecure, StatusPROpen, ModePoc) {
		t.Error("poc mode removed the PR rungs entirely; they must stay reachable")
	}
	if CanTransition(StatusInProgress, StatusSecure, ModePoc) {
		t.Error("poc mode let a task reach secure without passing verified")
	}

	// And the overlay must not leak into the shared table.
	if CanTransition(StatusSecure, StatusComplete, ModeStandard) {
		t.Error("asking for the poc table mutated the standard one")
	}
	// The poc reason text differs, because the rung that surprises people is a
	// different rung in each mode.
	res := ValidateTransition(StatusPending, StatusComplete, ModePoc)
	if !strings.Contains(res.Reason, "poc mode") {
		t.Errorf("a poc refusal does not mention poc mode: %s", res.Reason)
	}
}

func TestEveryModeOtherThanPocGetsTheStandardTable(t *testing.T) {
	// Including the empty string, which is every caller that does not care.
	for _, mode := range []LoopMode{"", ModeSimple, ModeStandard, "something-new"} {
		if CanTransition(StatusSecure, StatusComplete, mode) {
			t.Errorf("mode %q got the poc overlay", mode)
		}
	}
}

func TestCapsMatchV1(t *testing.T) {
	// Quoted from `packages/loop-state-mcp/src/validation.ts`:
	//
	//	export const CAPS = {
	//	  retries: 2,
	//	  security_rounds: 2,
	//	  review_rounds: 2,
	//	  qa_rounds: 2,
	//	} as const;
	//
	// This is a PORT, so the test fails if a number changed by accident.
	want := map[string]int{"retries": 2, "security_rounds": 2, "review_rounds": 2, "qa_rounds": 2}
	got := Caps()
	if len(got) != len(want) {
		t.Errorf("Caps has %d entries, v1 had %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Caps[%q] = %d, v1 had %d", k, got[k], v)
		}
	}
}

func TestBumpCounterTreatsTheCapAsInclusive(t *testing.T) {
	// v1's rule: reaching the cap is fine, EXCEEDING it signals escalation.
	// An off-by-one here escalates a task that was still within its budget.
	if v, exceeded := BumpCounter(1, "retries", 1); v != 2 || exceeded {
		t.Errorf("reaching the cap reported value=%d exceeded=%v; the cap is inclusive", v, exceeded)
	}
	if v, exceeded := BumpCounter(2, "retries", 1); v != 3 || !exceeded {
		t.Errorf("exceeding the cap reported value=%d exceeded=%v", v, exceeded)
	}
	if v, exceeded := BumpCounter(9, "not-a-counter", 1); v != 10 || exceeded {
		t.Errorf("an unknown counter reported value=%d exceeded=%v; it must not escalate on a name nobody set",
			v, exceeded)
	}
}

func TestValidateTransitionReturnsAResultAndNeverAnError(t *testing.T) {
	// DB19, over invalid, empty and unknown input. The signature has no error
	// return at all, so this is really asserting that it does not PANIC and
	// that it says something useful about nonsense.
	for _, tc := range []struct{ from, to TaskStatus }{
		{"", ""},
		{"", StatusPending},
		{StatusPending, ""},
		{"not-a-status", StatusComplete},
		{StatusComplete, "not-a-status"},
	} {
		res := ValidateTransition(tc.from, tc.to, ModeStandard)
		if tc.from == tc.to {
			// A self-transition, even of nonsense, is a no-op.
			continue
		}
		if res.OK {
			t.Errorf("%q -> %q was accepted", tc.from, tc.to)
		}
		if res.Reason == "" {
			t.Errorf("%q -> %q was refused with no reason", tc.from, tc.to)
		}
	}
}

func TestMachineGoHasNoImports(t *testing.T) {
	// It is a pure table, and a pure table is the thing two packages can both
	// read without either owning the other. The test exists because the
	// temptation to import `strings` for one Join is constant.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "machine.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse machine.go: %v", err)
	}
	if len(file.Imports) != 0 {
		var names []string
		for _, imp := range file.Imports {
			names = append(names, imp.Path.Value)
		}
		t.Errorf("machine.go imports %v; it must stay pure", names)
	}
}

func TestTheStatusLadderAndTheTableAgree(t *testing.T) {
	// The type is derived from the table rather than declared beside it: every
	// status in the ladder has a row, and every row is in the ladder. A status
	// with no row can never move again; a row with no status is unreachable.
	table := AllowedTransitions()
	for _, s := range TaskStatuses() {
		if _, ok := table[s]; !ok {
			t.Errorf("%s is in the ladder and has no row in AllowedTransitions", s)
		}
	}
	for from, targets := range table {
		if !containsStatus(TaskStatuses(), from) {
			t.Errorf("AllowedTransitions has a row for %q, which is not in the ladder", from)
		}
		for _, to := range targets {
			if !containsStatus(TaskStatuses(), to) {
				t.Errorf("%s -> %q names a status that is not in the ladder", from, to)
			}
		}
	}
}

func containsStatus(in []TaskStatus, want TaskStatus) bool {
	for _, s := range in {
		if s == want {
			return true
		}
	}
	return false
}
