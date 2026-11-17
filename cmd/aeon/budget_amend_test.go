package main

// budget_amend_test.go — S55 T8 (P4). The guard amendment for the budget
// plane, in the mould of loop_control_amend_test.go (T5) and the PL15/CS9
// guards already in this directory: the slice's security claim made
// mechanical rather than reviewed.
//
// The claim: budget.set, budget.remove and loop.raise are PERSON doors on
// the daemon's wide socket (LC5) and are provably absent from every
// non-human door — the narrow (agent / HTTP / MCP-stdio) surface, the
// agent-services enumeration, the tool registry, the records catalog, and by
// text in the four packages that own those doors. An agent may READ the
// budget plane (T10's four read twins); it never writes it, and the epic's
// money is a budget row, not an epic field (LC13).
//
// Positive half first, as the family's style demands: the app has its door
// (TestBudgetIsAHumanDoor), and the guards each have a canary that proves
// they can FAIL (TestBudgetGuardsHaveTeeth) — a guard that cannot fail is
// not a guard, it is a green light.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/tools"
)

// guardBudgetService is the non-nil seam a guard needs to see the eight
// budget methods REGISTERED. The door (RegisterBudget) only inspects whether
// the seam is nil — it never calls these methods — so a guard may stand in a
// seam that answers nothing (the loop-control precedent,
// loop_control_amend_test.go), without standing up a store.
type guardBudgetService struct{}

var _ protocol.BudgetStore = guardBudgetService{} // compile-time: the fake satisfies the seam

func (guardBudgetService) List(context.Context, string, string, string, *bool) ([]protocol.BudgetView, error) {
	return nil, nil
}
func (guardBudgetService) Show(context.Context, string) (protocol.BudgetShowResult, error) {
	return protocol.BudgetShowResult{}, nil
}
func (guardBudgetService) Effective(context.Context, string, string, string) (protocol.BudgetEffectiveResult, error) {
	return protocol.BudgetEffectiveResult{}, nil
}
func (guardBudgetService) SpendReport(context.Context, protocol.SpendReportParams) (protocol.SpendReportResult, error) {
	return protocol.SpendReportResult{}, nil
}
func (guardBudgetService) Set(context.Context, protocol.Actor, protocol.BudgetSetParams) (protocol.BudgetSetResult, *protocol.BudgetRefusal, error) {
	return protocol.BudgetSetResult{}, nil, nil
}
func (guardBudgetService) RemoveBudget(context.Context, protocol.Actor, string, string) (protocol.BudgetRemoveResult, *protocol.BudgetRefusal, error) {
	return protocol.BudgetRemoveResult{}, nil, nil
}
func (guardBudgetService) Raise(context.Context, protocol.Actor, protocol.LoopRaiseParams) (protocol.LoopRaiseResult, *protocol.BudgetRefusal, error) {
	return protocol.LoopRaiseResult{}, nil, nil
}
func (guardBudgetService) Vocabulary() protocol.BudgetVocabularyResult {
	return protocol.BudgetVocabularyResult{}
}

// newBudgetTable builds the table the way a daemon with a store builds it
// (daemon.go): NewTable, assign the seam, RegisterBudget. A non-nil seam
// registers the eight; the return is true, so the app has its door — the
// positive half the guards state before the negative one.
func newBudgetTable(t *testing.T) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.BudgetStore = guardBudgetService{}
	if !tbl.RegisterBudget() {
		t.Fatal("RegisterBudget refused to register over a non-nil seam — the app would have no budget door at all")
	}
	return tbl
}

// The three PERSON writes (LC5) — the set a non-human door must never carry —
// and the eight-plane names (the three writes, the five reads).
var (
	budgetWrites = []protocol.MethodName{
		protocol.MethodBudgetSet, protocol.MethodBudgetRemove,
		protocol.MethodLoopRaise,
	}
	budgetReads = []protocol.MethodName{
		protocol.MethodBudgetList, protocol.MethodBudgetShow,
		protocol.MethodBudgetEffective, protocol.MethodSpendReport,
		protocol.MethodBudgetVocabulary,
	}
	budgetAll = func() []protocol.MethodName {
		out := append([]protocol.MethodName{}, budgetWrites...)
		return append(out, budgetReads...)
	}()
)

// TestBudgetIsAHumanDoor is the positive half (P4): the exact write set is
// registered, each RequiresInitialize, none Debug; the read set is listed
// separately; and with the seam nil the eight stay ABSENT (V13 — a storeless
// daemon has no budget surface, not a refusing one), so the narrow
// projection cannot even name them.
func TestBudgetIsAHumanDoor(t *testing.T) {
	tbl := newBudgetTable(t)
	have := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		have[m] = true
	}

	// The exact write set: registered, RequiresInitialize, none Debug.
	for _, m := range budgetWrites {
		if !have[m] {
			t.Fatalf("%s must be registered — the app has one door onto the budget writes (LC5)", m)
		}
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s is not in the table — the positive half failed", m)
		}
		if !got.RequiresInitialize {
			t.Fatalf("%s does not RequireInitialize — a budget write may not precede the connection's negotiation", m)
		}
		if got.Debug {
			t.Fatalf("%s is marked Debug — a budget write is a product method on the wide socket, not a diagnostic", m)
		}
	}

	// The read set: registered, RequiresInitialize, none Debug.
	for _, m := range budgetReads {
		if !have[m] {
			t.Fatalf("%s must be registered — the read door is missing", m)
		}
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s is not in the table", m)
		}
		if !got.RequiresInitialize || got.Debug {
			t.Fatalf("%s is RequiresInitialize=%v Debug=%v, want true/false", m, got.RequiresInitialize, got.Debug)
		}
	}

	// Nil seam: the eight stay ABSENT (V13), and dispatch answers
	// method-not-found — the absence from the CALLER's side (the settings
	// guard's negative 5, carried).
	tblNil := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tblNil.RegisterBudget() // no-op over a nil seam: the eight stay absent
	for _, m := range budgetAll {
		if _, err := tblNil.Dispatch(context.Background(), "c1", m, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Fatalf("%s dispatched on a nil-seam table — the narrow surface can reach a door it must not (want ErrUnknownMethod, got %v)", m, err)
		}
	}
}

// TestBudgetNeverReachesANonHumanDoor is the negative half, one subtest per
// door: the narrow surface, the agent-services enumeration, the tool
// registry, the records catalog, the text of the four packages that own
// those doors, and the walk's non-vacuity. A budget write on any of them
// would hand a non-person the means to set another person's money authority —
// the same refusal as loop control (T5) and policy authoring (PL15), one
// plane over.
func TestBudgetNeverReachesANonHumanDoor(t *testing.T) {
	// ── Negative 1 — the narrow table: no action projects one of the three
	// writes, and NO budget-plane method reaches the surface at all — the
	// loop-shape list is T5's; here loop.raise is named explicitly because
	// it is a budget write that happens to wear a loop.* name.
	t.Run("narrow", func(t *testing.T) {
		for _, a := range narrow.Actions() {
			for _, m := range budgetWrites {
				if a.SocketMethod == m {
					t.Errorf("action %q projects the person write %q — an agent reaching a budget write sets another person's money authority (LC5)", a.Name, m)
				}
			}
			sm := string(a.SocketMethod)
			if strings.HasPrefix(sm, "budget.") || sm == "spend.report" || sm == "loop.raise" {
				t.Errorf("action %q projects budget-plane method %q — the narrow surface has no budget door, read or write (LC5)", a.Name, sm)
			}
		}
	})

	// ── Negative 2 — the agent-services enumeration: the agent-to-agent
	// task lifecycle (agent.*) has no budget door, so a budget method cannot
	// ride it either.
	t.Run("agent-services", func(t *testing.T) {
		for _, m := range protocol.AgentServiceMethods() {
			for _, b := range budgetAll {
				if m.Name == b {
					t.Fatalf("%s is an agent-service method — an agent reaching the budget plane with no person in the room (LC5)", m.Name)
				}
			}
		}
	})

	// ── Negative 3 — the tool registry: no tool name carries a budget write
	// verb except the NAMED exceptions — the pre-existing task-lifecycle
	// tool (its "set" is a task's STATE, not a budget), and T10's four read
	// twins, listed so the exception set is the record of what is allowed to
	// look budget-shaped. A tool named after a budget write would be a
	// second door onto the write the socket alone carries.
	t.Run("tools-registry", func(t *testing.T) {
		writeVerb := regexp.MustCompile(`set|remove|raise`)
		namedExceptions := map[string]bool{
			"task_set_status": true, // pre-existing: the agent task lifecycle record tool; "set" = a task's state
			// T10's read twins (registered when T10 lands):
			"budget_list":      true,
			"budget_show":      true,
			"budget_effective": true,
			"budget_spend":     true,
			// 2026-11-02 merge reconciliation: S45 T7's policy.proposeRemove
			// twin and S46 T8's mcp.mine.proposeRemove twin are OTHER
			// slices' propose tools whose names carry the "remove" substring
			// (a proposal FOR a removal, inert until a human decides — not
			// the write). The exception set stays the record of what is
			// allowed to look budget-shaped.
			"policy_propose_remove":     true, // S45 T7: the policy.remove PROPOSAL (inert row, not the write)
			"mcp_propose_remove_server": true, // S46 T8: the mcp server-removal PROPOSAL (inert row, not the write)
			// S62 T8 (2026-11-02): the export family's read twin over
			// export.sets — its "set" is a record SET (a declared export set),
			// not a budget write; a re-run re-lists, nothing to reconcile.
			"export_sets": true,
		}
		for name := range tools.Registry() {
			if namedExceptions[name] {
				continue
			}
			if writeVerb.MatchString(name) {
				t.Errorf("tool %q carries a budget write verb — a tool named after a budget write is a second door onto the write the socket alone carries (LC5)", name)
			}
		}
	})

	// ── Negative 4 — the records catalog: no spec at all twined to the
	// three writes, and NO Mutates spec twined to any budget method — the
	// read twins (T10) are the only budget-family specs an agent may file,
	// and they are not mutations (OD10).
	t.Run("records-catalog", func(t *testing.T) {
		for _, spec := range records.Catalog() {
			for _, m := range budgetWrites {
				if spec.Method == string(m) {
					t.Errorf("spec %q twines the person write %q — the records catalog is an agent's surface and a budget write is a person's act (LC5)", spec.Name, m)
				}
			}
			if spec.Mutates {
				for _, m := range budgetAll {
					if spec.Method == string(m) {
						t.Errorf("spec %q is a Mutates record twined to budget method %q — the only budget-family specs an agent may file are the reads (OD10)", spec.Name, m)
					}
				}
			}
		}
	})

	// ── Negative 5 — by text: the three writes' wire literals and consts
	// appear in NO non-test file under the four packages that own the
	// non-human doors. A projection that names budget.set, even in a comment,
	// is the projection the rule forbids.
	t.Run("text-grep", func(t *testing.T) {
		var totalWalked int
		for _, dir := range []string{"internal/narrow", "internal/agentsvc", "internal/tools", "internal/records"} {
			offenders, walked := walkBudgetLiterals(filepath.Join("..", "..", dir))
			totalWalked += walked
			for _, o := range offenders {
				t.Errorf("%s names a budget write — it must not reach a non-human door (LC5)", o)
			}
		}
		// ── Negative 6 — the walk saw enough to mean something. Measured
		// 2026-11-01 at T8: 40 non-test .go files across the four
		// directories (the spec's 50 was an estimate; the guard pins the
		// MEASURED floor, which is what anti-vacuity is for — a tree that
		// loses a guarded file fails the count).
		if totalWalked < 40 {
			t.Fatalf("the text-grep walk saw %d non-test files across the four guarded directories, want ≥ 40 — the guard would be checking almost nothing", totalWalked)
		}
	})
}

// narrowProjectingBudgetWrites returns the names of the narrow actions whose
// SocketMethod projects one of the three person writes — the guard's
// predicate, a function over action lists (not over narrow.Actions())
// precisely so the canary can feed it a poisoned table the real one never
// carries (the loop-control guard's shape).
func narrowProjectingBudgetWrites(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		for _, m := range budgetWrites {
			if a.SocketMethod == m {
				bad = append(bad, a.Name)
			}
		}
	}
	return bad
}

// recordsSpecTwinedToBudgetWrite reports whether a records spec twines one
// of the three person writes — the catalog's predicate, over specs, so the
// canary can feed it a fake spec the real catalog never carries.
func recordsSpecTwinedToBudgetWrite(spec records.Spec) bool {
	for _, m := range budgetWrites {
		if spec.Method == string(m) {
			return true
		}
	}
	return false
}

// walkBudgetLiterals walks one guarded directory's non-test .go files and
// returns the offending "path: literal" pairs plus the file count. The
// function-over-trees shape (not over a fixed dir list) is what lets the
// canary feed it a synthetic offender the real tree never carries.
func walkBudgetLiterals(root string) (offenders []string, walked int) {
	forbidden := []string{
		"budget.set", "budget.remove", "loop.raise",
		"MethodBudgetSet", "MethodBudgetRemove", "MethodLoopRaise",
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable dirs hold no Go code we own
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		walked++
		text := string(b)
		for _, f := range forbidden {
			if strings.Contains(text, f) {
				offenders = append(offenders, path+": "+f)
			}
		}
		return nil
	})
	return offenders, walked
}

// TestBudgetGuardsHaveTeeth is the canary (the family's meta-test): each
// negative predicate fed a synthetic violation must report it, and must not
// report a clean input. A guard that cannot fail is a green light, not a
// guard — the T5/PL15/CS9 tests assert the same about theirs.
func TestBudgetGuardsHaveTeeth(t *testing.T) {
	// Canary 1 — a fake narrow action projecting budget.set (exactly what a
	// slice would add "for symmetry", the one thing the Do-NOT list forbids)
	// must be flagged; the real table must not be.
	poisoned := []narrow.Action{
		{Name: "aeon_budget_set", SocketMethod: protocol.MethodBudgetSet, Kind: narrow.KindRead},
	}
	if got := narrowProjectingBudgetWrites(poisoned); len(got) == 0 {
		t.Fatal("a narrow action projecting budget.set was not flagged — the narrow guard cannot fail, so it is not a guard")
	}
	if got := narrowProjectingBudgetWrites(narrow.Actions()); len(got) != 0 {
		t.Errorf("the real narrow table was flagged: %v", got)
	}

	// Canary 2 — a fake records Spec twined to the budget.remove write must
	// be flagged; a clean budget read spec must not.
	if !recordsSpecTwinedToBudgetWrite(records.Spec{Name: "budget_remove", Method: "budget.remove", Mutates: true}) {
		t.Fatal("a records spec twined to budget.remove was not flagged — the catalog guard cannot fail")
	}
	if recordsSpecTwinedToBudgetWrite(records.Spec{Name: "budget_list", Method: "budget.list"}) {
		t.Fatal("a clean budget read spec was flagged — the predicate fires on nothing")
	}

	// Canary 3 — a temp non-test .go file under a fake tools/ directory
	// naming "budget.set" must be flagged by the text walk; a clean file must
	// not.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "poison.go"),
		[]byte("package tools\n\nvar _ = \"budget.set\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if off, _ := walkBudgetLiterals(dir); len(off) == 0 {
		t.Fatal("a non-test file naming budget.set was not flagged — the text walk cannot fail")
	}
	clean := t.TempDir()
	if err := os.WriteFile(filepath.Join(clean, "clean.go"),
		[]byte("package tools\n\nvar _ = \"budget.list\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if off, _ := walkBudgetLiterals(clean); len(off) != 0 {
		t.Errorf("a clean file was flagged: %v", off)
	}
}

// TestBudgetAmendmentLeavesThePriorGuardsByteIdentical re-pins T5's guard
// file set (plus T5's own amendment, the prior guard from T8's vantage): the
// T8 amendment adds this one test file and changes no existing guard body,
// and this makes that a fact the build checks rather than a promise the
// review remembers. T5's TestOriginalNarrowGuardsAreByteIdentical enforces
// the same set against the pre-T5 tree; this enforces it against T8.
func TestBudgetAmendmentLeavesThePriorGuardsByteIdentical(t *testing.T) {
	pinned := map[string]string{
		"narrow_degradation_test.go": "440b8fc1c6365b5b823bdebf5cbfa52796138735681db4e0ddb7d83578602ee1",
		"narrow_http_test.go":        "455be5aa2cf9d64e455fc0e6209ba83d3336298c13d670265ab0a30b835d4a75",
		"seed_guard_test.go":         "2f02c98b52b20fe0b1fadef3d2b7a85c2c062e2361a64efe4f42e69a65c436ee",
		"settings_guard_test.go":     "fc8a4e36f775db10aec884e65e854f51cc3d909c3e4941004747482408dfb10d",
		"skills_guard_test.go":       "d6a9a485537417ea5aeb39d1380ad7bb474961afad9f1ea2ed3410b557088b8d",
		"loop_control_amend_test.go": "13920fc523c793b75fc2c35ca9523917ea3a830ff11f62874c22ec67edc06b8d",// // re-pinned 2026-11-04 (S60 T7): TestOnlyTheControlServiceCallsTheCAS names the merge queue's two files as callers of the daemon's control-service door (d.CancelLoop/d.ResumeLoop), the one deliberate edit; prior re-pin: 2026-11-03 (S49 T5) — earlier: re-pinned 2026-11-03 (S49 T5): the cascade from the cited records_guard_test.go fix-forward (see that file's re-pin comment and S49's T5 Notes); the amendments this test owns remain unedited
		filepath.Join("..", "..", "internal", "narrow", "records_guard_test.go"): "34e9df0d456a3e0119d4e4b13f2a6d2f480cbd9696a555ade2b86237af5f05aa", // re-pinned 2026-11-03 (S49 T5): this pin guards the guard, not its silence — the file had been main-RED since S48 T10's catalog twin (a8759842) met the 2026-10-27 aeon_blueprint_validate action, and T5's fix-forward widened allowedOverlap on the loop.list precedent (blame and reasoning in S49's T5 Notes); the amendments this test owns remain unedited
		filepath.Join("..", "..", "internal", "narrow", "actions_test.go"):       "0f060df721d36921520cdfcbfec24944a461f03d0df239b172c1bcb1e65c47d8",
		filepath.Join("..", "..", "internal", "narrow", "questions_test.go"):     "f789cf18a11ccaa6066d4a9c4128235f7db72f7a1b9dc081cf6a4c6a17e22fa3",
	}
	for path, want := range pinned {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s changed since the T5 pins (want %s, got %s) — the T8 amendment edits no existing guard body", path, want, got)
		}
	}
}
