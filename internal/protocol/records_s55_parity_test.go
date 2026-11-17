package protocol

import (
	"testing"

	"github.com/cjlapao/aeon/internal/records"
)

// S55 T10 — the loop-control / budget / watchdog families, walked in BOTH
// directions: the seven catalog specs stand as their own doors, and no OTHER
// spec twines to a plane door (a plane door is a person's, or one of the
// seven; a tool for anything else is a lever, and the catalog must never
// hand one out).

// s55SevenTwins is the T10 surface: tool name beside its socket door.
var s55SevenTwins = []struct{ name, method string }{
	{"loop_control_state", string(MethodLoopControl)},
	{"loop_request_stop", string(MethodLoopRequestStop)},
	{"budget_list", string(MethodBudgetList)},
	{"budget_show", string(MethodBudgetShow)},
	{"budget_effective", string(MethodBudgetEffective)},
	{"budget_spend", string(MethodSpendReport)},
	{"watchdog_list", string(MethodWatchdogList)},
}

// s55PlaneMethods is the whole of the three planes' doors — the set the
// catalog's seven must not exceed.
func s55PlaneMethods() map[string]bool {
	return map[string]bool{
		string(MethodLoopStop): true, string(MethodLoopResume): true,
		string(MethodLoopCancel): true, string(MethodLoopControl): true,
		string(MethodLoopRequestStop): true, string(MethodLoopControlRequests): true,
		string(MethodLoopControlDismiss): true,
		string(MethodBudgetList):         true, string(MethodBudgetShow): true,
		string(MethodBudgetEffective): true, string(MethodBudgetSet): true,
		string(MethodBudgetRemove): true, string(MethodBudgetVocabulary): true,
		string(MethodSpendReport): true, string(MethodLoopRaise): true,
		string(MethodWatchdogList): true, string(MethodWatchdogShow): true,
		string(MethodWatchdogVocabulary): true, string(MethodWatchdogSet): true,
		string(MethodWatchdogSetEnabled): true, string(MethodWatchdogRemove): true,
	}
}

func TestRecordsParityLoopControlFamilies(t *testing.T) {
	tbl := newTestTable(t)
	// The three planes are conditionally registered (V13) — the same
	// zero-value seams the exact-set test wires: the doors only inspect
	// nil-ness at registration, the walk needs the METHODS present, not the
	// answers.
	tbl.LoopControl = testLoopControlService{}
	if !tbl.RegisterLoopControl() {
		t.Fatal("RegisterLoopControl refused a wired seam")
	}
	tbl.BudgetStore = testBudgetService{}
	if !tbl.RegisterBudget() {
		t.Fatal("RegisterBudget refused a wired seam")
	}
	tbl.Watchdog = testWatchdogService{}
	if !tbl.RegisterWatchdog() {
		t.Fatal("RegisterWatchdog refused a wired seam")
	}
	registered := map[string]bool{}
	for _, m := range tbl.Methods() {
		registered[string(m)] = true
	}
	byName := map[string]records.Spec{}
	for _, sp := range records.Catalog() {
		byName[sp.Name] = sp
	}
	// Forward: each of the seven stands as its own door, under its own name.
	seven := map[string]bool{}
	for _, s7 := range s55SevenTwins {
		seven[s7.name] = true
		sp, ok := byName[s7.name]
		if !ok {
			t.Errorf("the catalog has no %q — the seven specs are the S55 T10 surface", s7.name)
			continue
		}
		if sp.Method != s7.method {
			t.Errorf("%q twines %q, want %q — the tool and its door are drifting", s7.name, sp.Method, s7.method)
		}
		if !registered[sp.Method] {
			t.Errorf("%q names %q, which the table does not register — a door nobody opened", s7.name, sp.Method)
		}
	}
	// Backward: no spec OTHER than the seven twines to a plane door.
	for _, sp := range records.Catalog() {
		if s55PlaneMethods()[sp.Method] && !seven[sp.Name] {
			t.Errorf("catalog action %q twines to the plane door %q — a plane door belongs to a person (or to the seven); a tool for it is a lever", sp.Name, sp.Method)
		}
	}
}
