package loop

// S55 T7 — budget enforcement at the loop boundary (E1), the seam's failure
// discipline, the tightest-breach naming, and the vocabulary LC1 protects.
//
// The engine under test is the real one (store-backed, openLoopStore); what
// is faked is only the budget seam (Deps.Budget) and the model (Deps.Model) —
// the same shape the production wiring has: the daemon's budget_seam.go
// implements the seam over store.ApplicableBudgets + SpendFor, and a test's
// fake verdict is the value that seam returns.

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/budget"
	"github.com/cjlapao/aeon/internal/store"
)

// capBudgetLoop pins a fresh single-lane loop the budget tests drive: one
// model step, real store, the engine's own Start.
func capBudgetLoop(t *testing.T) (*store.Store, string) {
	t.Helper()
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "cap.yaml")
	writeBlueprint(t, path, "  - id: first\n    step: {kind: model-step, agent: default}\n")
	st, err := Start(context.Background(), s, store.NewLoop{
		Slug: "cap", ComponentRoot: root, Root: root, Goal: "g", Mode: "standard",
	}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, st.ID
}

// f64 is the test's cap pointer (zero is a real cap; the pointer is the cap).
func f64(v float64) *float64 { return &v }

// blockedEvent is the loop.blocked event a test captures: the payload the
// engine emits is the block reason itself (kind + the engine's sentence).
type blockedEvent struct {
	kind   string
	detail string
}

// TestBoundaryHitBlocksWithoutStartingTheStep — LC9 E1: spend at the cap
// BEFORE a dispatch. The step is never started, the loop parks
// blocked/cap-hit, the block reason is the engine's one sentence (budget,
// scope, subject, dimension, floor, cap all named), the pin carries the hit
// (the raise reads it), and the hit audit callback fires once with it.
func TestBoundaryHitBlocksWithoutStartingTheStep(t *testing.T) {
	s, id := capBudgetLoop(t)
	started, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	hit := budget.Hit{BudgetID: "bud-1", Scope: "loop", SubjectID: id, Dimension: "usd", Window: "lifetime",
		SpendFloor: 50, Cap: 50, Reason: "over-cap", Exact: true}
	var modelCalls int
	var hits []budget.Hit
	var events []string
	var blocked blockedEvent
	dep := Deps{
		Store: s,
		Event: func(kind string, payload any) {
			events = append(events, kind)
			if kind == "loop.blocked" {
				if m, ok := payload.(map[string]any); ok {
					if r, ok := m["reason"].(*BlockedReason); ok && r != nil {
						blocked = blockedEvent{kind: r.Kind, detail: r.Detail}
					}
				}
			}
		},
		Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
			modelCalls++
			return ModelResult{Kind: "completed", Outcome: "ok"}, nil
		},
		Budget: func(context.Context, LoopState) (budget.Verdict, error) {
			return budget.Verdict{OK: false, Hits: []budget.Hit{hit}}, nil
		},
		BudgetEvent: func(h budget.Hit, loopID string) error {
			if loopID != id {
				t.Fatalf("hit recorded for loop %s, want %s", loopID, id)
			}
			hits = append(hits, h)
			return nil
		},
	}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	got, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "blocked" || got.LastReason != "cap-hit" {
		t.Fatalf("pinned = %s/%s, want blocked/cap-hit", got.Status, got.LastReason)
	}
	if modelCalls != 0 {
		t.Fatalf("the model was called %d times — a step started past the cap", modelCalls)
	}
	// Position is unchanged: nothing in flight, nothing dispatched.
	if got.Position != started.Position {
		t.Fatalf("position moved %v -> %v — a boundary hit dispatches nothing", started.Position, got.Position)
	}
	if got.CapHit == nil || got.CapHit.BudgetID != "bud-1" || got.CapHit.Dimension != "usd" {
		t.Fatalf("pin carries no cap hit (the raise would have nothing to read): %+v", got.CapHit)
	}
	// The one sentence: the engine's renderer, verbatim, in the event.
	want := "budget bud-1 on loop " + id + " is exhausted (usd 50 of 50)"
	if blocked.kind != "cap-hit" || blocked.detail != want {
		t.Fatalf("loop.blocked = {kind:%q detail:%q}, want {cap-hit %q}", blocked.kind, blocked.detail, want)
	}
	if len(hits) != 1 || hits[0] != hit {
		t.Fatalf("hit audit callback = %v, want exactly the hit", hits)
	}
	if len(events) == 0 || events[len(events)-1] != "loop.blocked" {
		t.Fatalf("events = %v, want the last to be loop.blocked", events)
	}
}

// TestBoundaryHitNamesTheTightestBreach — behaviour 5: every applicable
// budget is breached at once; the NARROWEST one (the loop's own) is the one
// named, because Evaluate orders scope narrow-to-wide and E1 records
// Hits[0]. A reader who saw the workspace budget named would raise the wrong
// cap; the others re-evaluate on the re-dispatch (they ride the same
// verdict, Hits[1:]).
func TestBoundaryHitNamesTheTightestBreach(t *testing.T) {
	s, id := capBudgetLoop(t)
	budgets := []budget.Budget{
		{ID: "bud-ws", Scope: "workspace", Subject: "workspace", Window: "lifetime", USD: f64(50)},
		{ID: "bud-ep", Scope: "epic", Subject: "ep-1", Window: "lifetime", USD: f64(50)},
		{ID: "bud-loop", Scope: "loop", Subject: id, Window: "lifetime", USD: f64(50)},
	}
	spends := map[string]budget.Spend{
		"bud-ws":   {USD: 60},
		"bud-ep":   {USD: 60},
		"bud-loop": {USD: 60},
	}
	verdict := budget.Evaluate(budgets, spends, map[string]time.Time{}, time.Now())
	if verdict.OK || len(verdict.Hits) != 3 {
		t.Fatalf("verdict = %+v, want three hits", verdict)
	}
	if verdict.Hits[0].BudgetID != "bud-loop" || verdict.Hits[1].BudgetID != "bud-ep" || verdict.Hits[2].BudgetID != "bud-ws" {
		t.Fatalf("hit order = %v, want loop, epic, workspace", verdict.Hits)
	}
	var blocked blockedEvent
	dep := Deps{
		Store: s,
		Event: func(kind string, payload any) {
			if kind == "loop.blocked" {
				if m, ok := payload.(map[string]any); ok {
					if r, ok := m["reason"].(*BlockedReason); ok && r != nil {
						blocked = blockedEvent{kind: r.Kind, detail: r.Detail}
					}
				}
			}
		},
		Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
			t.Fatal("the model must not be called on a boundary hit")
			return ModelResult{}, nil
		},
		Budget: func(context.Context, LoopState) (budget.Verdict, error) { return verdict, nil },
	}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	got, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "blocked" || got.LastReason != "cap-hit" {
		t.Fatalf("pinned = %s/%s, want blocked/cap-hit", got.Status, got.LastReason)
	}
	if got.CapHit == nil || got.CapHit.BudgetID != "bud-loop" {
		t.Fatalf("CapHit = %+v, want the LOOP's budget (the narrowest scope)", got.CapHit)
	}
	if !strings.Contains(blocked.detail, "bud-loop") || strings.Contains(blocked.detail, "bud-ws") {
		t.Fatalf("block detail = %q, want the loop-scoped budget named, not the workspace's", blocked.detail)
	}
}

// TestBudgetSeamErrorFailsClosed — behaviour 9: a budget the engine could
// not READ is a block, never a spend. The kind names the failure (a person
// can tell "the cap is reached" from "I could not see the cap"), and the
// step is not started either way.
func TestBudgetSeamErrorFailsClosed(t *testing.T) {
	s, id := capBudgetLoop(t)
	var modelCalls int
	dep := Deps{
		Store: s,
		Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
			modelCalls++
			return ModelResult{Kind: "completed", Outcome: "ok"}, nil
		},
		Budget: func(context.Context, LoopState) (budget.Verdict, error) {
			return budget.Verdict{}, errors.New("the spend row is unreadable")
		},
	}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	got, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "blocked" {
		t.Fatalf("pinned = %s/%s, want blocked/<something>", got.Status, got.LastReason)
	}
	if got.LastReason != "budget-unreadable: the spend row is unreadable" {
		t.Fatalf("lastReason = %q, want the seam's error named", got.LastReason)
	}
	if modelCalls != 0 {
		t.Fatalf("the model was called %d times — the unreadable cap let spend through", modelCalls)
	}
}

// TestVocabularyStillSixStatuses — LC1: the loop's status vocabulary is a
// CLOSED set of six. T7's cap-hit work adds a REASON (cap-hit, a lastReason)
// and a step's pause reason (cap) — it must not add a seventh status,
// because every reader (the control eligibility, the app's ladder, the
// store) branches on the six.
//
// The engine writes loop status by exactly three mechanisms, all visible in
// the AST:
//
//	st.Status = "<literal>"   the three live states: running, waiting, blocked
//	st.Status = endStatus(...) the ONE terminal decider (S39 CR1: a walk that
//	                           runs out of lanes; the DoD greps for a direct
//	                           `Status = "done"` and finds nothing)
//	st.Status = StatusIdle    the resting state (written through endStatus
//	                           today, but the const is the declared form)
//
// This test parses the engine's own sources and collects every DIRECT status
// decision: literals are pinned to the six, endStatus decisions expand to its
// three returns, and the idle const to "idle". Indirect writes (copies like
// `result.Status = parked.Status`) and foreign row statuses (agenda rows)
// are skipped — they move values the six already named. A new status literal
// anywhere in the engine, or a new decision mechanism, is a failing test.
func TestVocabularyStillSixStatuses(t *testing.T) {
	want := []string{"running", "waiting", "blocked", "idle", "done", "aborted"}
	known := map[string]bool{}
	for _, s := range want {
		known[s] = true
	}
	seen := map[string]bool{}
	endStatusDecisions := 0
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				return true
			}
			sel, ok := assign.Lhs[0].(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Status" {
				return true
			}
			switch rhs := assign.Rhs[0].(type) {
			case *ast.BasicLit:
				if rhs.Kind != token.STRING {
					return true
				}
				v := strings.Trim(rhs.Value, `"`)
				if !known[v] {
					t.Errorf("%s: the engine writes status %q — the vocabulary is the closed six %v", name, v, want)
				}
				seen[v] = true
			case *ast.CallExpr:
				fn, ok := rhs.Fun.(*ast.Ident)
				if !ok || fn.Name != "endStatus" {
					return true // a copy or a foreign row's status — not a loop decision
				}
				endStatusDecisions++
				seen["done"] = true
				seen["aborted"] = true
				seen[StatusIdle] = true
			case *ast.Ident:
				if rhs.Name == "StatusIdle" {
					seen[StatusIdle] = true
				}
				// a copy of another Status (st.Status, parked.Status) — skipped
			}
			return true
		})
	}
	if endStatusDecisions == 0 {
		t.Fatal("no endStatus decision site found — the terminal decider moved; this test no longer sees how the walk ends")
	}
	if len(seen) != len(want) {
		t.Fatalf("the engine writes %d statuses %v, want the six %v", len(seen), sortedKeys(seen), want)
	}
	for _, s := range want {
		if !seen[s] {
			t.Fatalf("status %q is no longer written by the engine: %v", s, sortedKeys(seen))
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
