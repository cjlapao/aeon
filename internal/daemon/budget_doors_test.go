package daemon

// S55 T8 — the budget plane's EIGHT DOORS, spoken the way a client speaks
// them: initialize once (the fixture's c1), dispatch, read the result, and
// read the rows afterwards to prove the answer was assembled from what the
// store holds.
//
// The actor the write doors stamp comes from the CONNECTION (the daemon's
// ActorFor seam over the transport's peer credentials — t15Daemon's c1 is
// the kernel's uid 4321), never from params: a params field named `actor`
// is refused by name (-32602), and the audit shows user/4321.
//
// The confirmation is the S45 PM7.1 pattern: a set that RAISES (or disables)
// an enabled cap, and every removal, commits only against the digest of the
// canonical statement the daemon rendered — bound to what was read. The
// confirm-required refusal carries the statement and its digest so a client
// completes in one round trip; a digest that no longer matches is a
// confirm-mismatch, the row having moved between the read and the commit.
//
// The raise door is the T7 in-process op behind a socket: the dimension the
// caller named is the dimension the loop is parked on (the door checks what
// the op cannot), and a lost race is a RESULT with a refusal, never an
// error with a dead end.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/go-backend-scaffolding/framework/localrpc"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
)

// budgetPeerUID is the uid the fake transport reports for c1 — the number
// the budget audit must show as the actor, because the KERNEL put it there
// (the t15PeerUID pattern; the two consts agree on the same credential).
const budgetPeerUID = 4321

// budgetDigestHex is the test's own copy of the digest the daemon demands:
// the hex SHA-256 of the canonical statement. Recomputing it here (rather
// than echoing the daemon's) is the check — a daemon that digests anything
// other than the statement it showed would answer a confirm the statement
// does not bind.
func budgetDigestHex(t *testing.T, statement string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(statement))
	return hex.EncodeToString(sum[:])
}

// budgetErrorOf is the house assertion pattern for a refused act: the error
// is a *protocol.Error (WithWhat copies the registry entry, so errors.Is
// cannot see the sentinel) — compare the CODE and read the specifics out of
// Data (kind, what, statement, confirm).
func budgetErrorOf(t *testing.T, err error) *protocol.Error {
	t.Helper()
	if err == nil {
		t.Fatal("the act was admitted; it must have refused")
	}
	var pe *protocol.Error
	if !errors.As(err, &pe) {
		t.Fatalf("the refusal is %T (%v), want a protocol.Error", err, err)
	}
	return pe
}

// badParamsOf is the house assertion for the param-shape refusals (-32602):
// the daemon answers JSON-RPC's own bad-params — an ErrBadParams VALUE,
// whose Error() reads "invalid params: <msg>" — not a protocol.Error, so the
// specifics are read out of the string (the code is JSON-RPC's, not Aeon's).
func badParamsOf(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("the act was admitted; it must have refused the params")
	}
	var bp protocol.ErrBadParams
	if !errors.As(err, &bp) {
		t.Fatalf("the refusal is %T (%v), want the -32602 bad-params error", err, err)
	}
	return err.Error()
}

// budgetChangeCount is the number of change-kind audit rows (created,
// changed, raised) a budget has — the LC11 no-op check's "nothing was
// written" (a no-op set leaves no row, and the count is the proof).
func budgetChangeCount(t *testing.T, s *store.Store, budgetID string) int {
	t.Helper()
	events, err := s.ListBudgetEvents(budgetID, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range events {
		switch e.Kind {
		case "created", "changed", "raised":
			n++
		}
	}
	return n
}

// (1) budget.list is list-with-details (rule 3): every budget the filters
// match, with the LIVE status computed at the read (ok|warn|hit from the
// current spend and the warn line — never a stored flag), the per-dimension
// detail with P31's marks (the floor, exact when it is a total, the unknown
// count beside it), and the last change from the audit. The status filter
// filters on the computed status, so a budget that warned between two reads
// leaves the "ok" list on the second.
func TestBudgetListIsListWithDetails(t *testing.T) {
	d, _, s, root := t15Daemon(t)

	mkLoop := func(slug string) string {
		pin, err := s.CreatePinnedLoop(store.NewLoop{Slug: slug, ComponentRoot: root, Root: root, Goal: "g", Status: "running"},
			"dev-loop", 1, "id: dev-loop\n", "{}")
		if err != nil {
			t.Fatal(err)
		}
		return pin.ID
	}
	mkStep := func(loopSlug string) int64 {
		st, err := s.CreateStep(store.Scope{LoopSlug: loopSlug, ComponentRoot: root},
			store.NewStep{Phase: "build", Agent: "x", Status: store.StepRunning})
		if err != nil {
			t.Fatal(err)
		}
		return st.ID
	}
	mkBudget := func(scope, subject string, in store.NewBudgetIn) string {
		b, ref, err := s.CreateBudget(store.BudgetActor{Kind: "user", ID: "u1"}, in)
		if err != nil {
			t.Fatal(err)
		}
		if ref.Kind != "" {
			t.Fatalf("%s/%s refused: %s", scope, subject, ref)
		}
		return b.ID
	}

	// hit: a zero cap at a floor of 3 (the floor sits on the cap — a hit).
	hitLoop := mkLoop("l-hit")
	if err := s.SetStepCost(mkStep("l-hit"), model.Cost{State: model.CostKnown, USD: 3}); err != nil {
		t.Fatal(err)
	}
	hitID := mkBudget("loop", hitLoop, store.NewBudgetIn{Scope: "loop", Subject: hitLoop, Window: "lifetime", USD: capFloat(0)})

	// warn: floor 85 against cap 100 under an 80% warn line (85 >= 80, and
	// the policy is proceed, so the unknown counts but does not hit).
	warnLoop := mkLoop("l-warn")
	if err := s.SetStepCost(mkStep("l-warn"), model.Cost{State: model.CostKnown, USD: 40}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepCost(mkStep("l-warn"), model.Cost{State: model.CostKnown, USD: 45}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepCost(mkStep("l-warn"), model.Cost{State: model.CostUnknown, Reason: "no price"}); err != nil {
		t.Fatal(err)
	}
	wp := 80
	warnID := mkBudget("loop", warnLoop, store.NewBudgetIn{Scope: "loop", Subject: warnLoop, Window: "lifetime", USD: capFloat(100), WarnPct: &wp})

	// ok: floor 5 against cap 1000.
	okLoop := mkLoop("l-ok")
	if err := s.SetStepCost(mkStep("l-ok"), model.Cost{State: model.CostKnown, USD: 5}); err != nil {
		t.Fatal(err)
	}
	okID := mkBudget("loop", okLoop, store.NewBudgetIn{Scope: "loop", Subject: okLoop, Window: "lifetime", USD: capFloat(1000)})

	res, err := t15Dispatch(t, d, protocol.MethodBudgetList, protocol.BudgetListParams{})
	if err != nil {
		t.Fatal(err)
	}
	list := res.(protocol.BudgetListResult)
	if len(list.Budgets) != 3 {
		t.Fatalf("budget.list answered %d budgets, want 3: %+v", len(list.Budgets), list.Budgets)
	}
	byID := map[string]protocol.BudgetView{}
	for _, v := range list.Budgets {
		byID[v.ID] = v
	}

	checkDimension := func(view protocol.BudgetView, wantStatus string, wantFloor float64, wantExact bool, wantUnknown int) {
		if view.Status != wantStatus {
			t.Fatalf("budget %s status = %q, want %q (cap %+v)", view.ID, view.Status, wantStatus, view.Dimensions)
		}
		if len(view.Dimensions) != 1 || view.Dimensions[0].Dimension != "usd" {
			t.Fatalf("budget %s dimensions = %+v, want one usd dimension", view.ID, view.Dimensions)
		}
		dim := view.Dimensions[0]
		if dim.SpendFloor == nil || *dim.SpendFloor != wantFloor {
			t.Fatalf("budget %s spend floor = %v, want %v", view.ID, dim.SpendFloor, wantFloor)
		}
		if dim.Exact == nil || *dim.Exact != wantExact {
			t.Fatalf("budget %s exact = %v, want %v — the floor's honesty mark", view.ID, dim.Exact, wantExact)
		}
		if dim.UnknownCount != wantUnknown {
			t.Fatalf("budget %s unknown count = %d, want %d — an unknown is counted, never summed", view.ID, dim.UnknownCount, wantUnknown)
		}
		if view.LastChanged == nil || view.LastChanged.Kind != "created" || view.LastChanged.ByKind != "user" || view.LastChanged.ByID != "u1" {
			t.Fatalf("budget %s lastChanged = %+v, want the created row by user/u1", view.ID, view.LastChanged)
		}
	}
	checkDimension(byID[hitID], "hit", 3, true, 0)
	checkDimension(byID[warnID], "warn", 85, false, 1)
	checkDimension(byID[okID], "ok", 5, true, 0)

	// The status filter filters on the COMPUTED status, one value at a time.
	for status, want := range map[string]string{"hit": hitID, "warn": warnID, "ok": okID} {
		res, err := t15Dispatch(t, d, protocol.MethodBudgetList, protocol.BudgetListParams{Status: status})
		if err != nil {
			t.Fatal(err)
		}
		got := res.(protocol.BudgetListResult)
		if len(got.Budgets) != 1 || got.Budgets[0].ID != want {
			t.Fatalf("status %q answered %+v, want the one %s budget", status, got.Budgets, status)
		}
	}
}

// (2) budget.effective answers the budgets that apply to the named subject,
// TIGHTEST FIRST (LC9): the loop's own, then the epic's (loop → goal →
// epic), then the component and its root, then the workspace — the same
// tier walk the enforcement side runs, so the read and the gate cannot
// disagree about what is in force (or in what order it fires).
func TestBudgetEffectiveTightestFirst(t *testing.T) {
	d, _, s, root := t15Daemon(t)

	roots, err := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	if err != nil || len(roots) == 0 {
		t.Fatalf("no root component: %v", err)
	}
	children, err := s.ListComponents(store.ComponentFilter{ParentID: roots[0].ID})
	if err != nil || len(children) == 0 {
		t.Fatalf("no child component: %v", err)
	}
	epic, err := s.CreateEpic(store.NewEpic{ComponentID: roots[0].ID, Title: "funded"})
	if err != nil {
		t.Fatal(err)
	}
	goal, rf, err := s.CreateGoal(store.NewGoal{ComponentID: roots[0].ID, EpicID: epic.ID, Title: "works the epic",
		Type: "feature", SourceKind: "manual", SourceRef: "t8", By: store.Actor{Kind: "user", ID: "u1"}, Submit: true})
	if err != nil || rf != nil {
		t.Fatalf("goal: %v %v", rf, err)
	}
	gv := goal.Version
	pin, err := s.CreatePinnedLoop(store.NewLoop{Slug: "effective", ComponentRoot: root, Root: root, Goal: "g",
		GoalID: goal.ID, GoalVersion: &gv, Status: "running"}, "dev-loop", 1, "id: dev-loop\n", "{}")
	if err != nil {
		t.Fatal(err)
	}
	mkBudget := func(in store.NewBudgetIn) string {
		b, ref, err := s.CreateBudget(store.BudgetActor{Kind: "user", ID: "u1"}, in)
		if err != nil || ref.Kind != "" {
			t.Fatalf("budget %s/%s: %v %v", in.Scope, in.Subject, ref, err)
		}
		return b.ID
	}
	loopBudgetID := mkBudget(store.NewBudgetIn{Scope: "loop", Subject: pin.ID, Window: "lifetime", USD: capFloat(50)})
	epicBudgetID := mkBudget(store.NewBudgetIn{Scope: "epic", Subject: epic.ID, Window: "lifetime", USD: capFloat(60)})
	wsBudgetID := mkBudget(store.NewBudgetIn{Scope: "workspace", Subject: "workspace", Window: "lifetime", USD: capFloat(70)})

	// The loop arm: loop → epic → (component/root are paths here, no
	// ID-subject budget matches) → workspace.
	res, err := t15Dispatch(t, d, protocol.MethodBudgetEffective, protocol.BudgetEffectiveParams{LoopID: pin.ID})
	if err != nil {
		t.Fatal(err)
	}
	eff := res.(protocol.BudgetEffectiveResult)
	if eff.Subject != "loop "+pin.ID {
		t.Fatalf("subject = %q, want %q", eff.Subject, "loop "+pin.ID)
	}
	ids := []string{}
	for _, v := range eff.Budgets {
		ids = append(ids, v.ID)
	}
	want := []string{loopBudgetID, epicBudgetID, wsBudgetID}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("loop arm order = %v, want %v — tightest first (loop, epic, workspace)", ids, want)
	}
	// A second read is the same order (the walk is deterministic).
	res, err = t15Dispatch(t, d, protocol.MethodBudgetEffective, protocol.BudgetEffectiveParams{LoopID: pin.ID})
	if err != nil {
		t.Fatal(err)
	}
	eff2 := res.(protocol.BudgetEffectiveResult)
	ids2 := []string{}
	for _, v := range eff2.Budgets {
		ids2 = append(ids2, v.ID)
	}
	if strings.Join(ids2, ",") != strings.Join(ids, ",") {
		t.Fatalf("the effective order moved between reads: %v then %v", ids, ids2)
	}

	// The component arm: component → root → workspace.
	childBudgetID := mkBudget(store.NewBudgetIn{Scope: "component", Subject: children[0].ID, Window: "lifetime", USD: capFloat(80)})
	rootBudgetID := mkBudget(store.NewBudgetIn{Scope: "component", Subject: roots[0].ID, Window: "lifetime", USD: capFloat(90)})
	res, err = t15Dispatch(t, d, protocol.MethodBudgetEffective, protocol.BudgetEffectiveParams{ComponentID: children[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	eff = res.(protocol.BudgetEffectiveResult)
	if eff.Subject != "component "+children[0].ID {
		t.Fatalf("subject = %q, want %q", eff.Subject, "component "+children[0].ID)
	}
	ids = ids[:0]
	for _, v := range eff.Budgets {
		ids = append(ids, v.ID)
	}
	want = []string{childBudgetID, rootBudgetID, wsBudgetID}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("component arm order = %v, want %v — tightest first (component, root, workspace)", ids, want)
	}

	// Naming two subjects is a parse error that says how many.
	if _, err := t15Dispatch(t, d, protocol.MethodBudgetEffective,
		protocol.BudgetEffectiveParams{LoopID: pin.ID, ComponentID: children[0].ID}); err == nil ||
		!strings.Contains(err.Error(), "named 2") {
		t.Fatalf("two named subjects got %v — the refusal must count them", err)
	}
}

// (3) spend.report is the spend over time, grouped: day / trigger /
// component / epic at the STEP's grain (a step belongs wholly to one of each),
// and route at the TURN's grain (a step's cost is the only number that
// partitions across routes — the step rows would count it once per route it
// touched). P31 throughout: the floor is a floor, `exact` says when it is a
// total, the partial and unknown counts say what it is not, and a bucket
// with no priced tokens answers a nil token floor, never zero.
func TestSpendReportBuckets(t *testing.T) {
	d, _, s, _ := t15Daemon(t)

	roots, err := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	if err != nil || len(roots) == 0 {
		t.Fatalf("no root component: %v", err)
	}
	rootID := roots[0].ID
	rootA, rootB := t.TempDir(), t.TempDir()
	if _, err := s.CreateComponent(store.NewComponent{Name: "alpha-b", Slug: "alpha-b", Root: rootA, ParentID: rootID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComponent(store.NewComponent{Name: "beta-b", Slug: "beta-b", Root: rootB, ParentID: rootID}); err != nil {
		t.Fatal(err)
	}
	epicA, err := s.CreateEpic(store.NewEpic{ComponentID: rootID, Title: "epic A"})
	if err != nil {
		t.Fatal(err)
	}
	goalA, rf, err := s.CreateGoal(store.NewGoal{ComponentID: rootID, EpicID: epicA.ID, Title: "works A",
		Type: "feature", SourceKind: "manual", SourceRef: "t8", By: store.Actor{Kind: "user", ID: "u1"}, Submit: true})
	if err != nil || rf != nil {
		t.Fatalf("goal: %v %v", rf, err)
	}
	gv := goalA.Version
	if _, err := s.CreatePinnedLoop(store.NewLoop{Slug: "spend-a", ComponentRoot: rootA, Root: rootA, Goal: "g",
		GoalID: goalA.ID, GoalVersion: &gv, TriggerID: "trig-1", Status: "done"}, "dev-loop", 1, "id: dev-loop\n", "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePinnedLoop(store.NewLoop{Slug: "spend-b", ComponentRoot: rootB, Root: rootB, Goal: "g",
		Status: "done"}, "dev-loop", 1, "id: dev-loop\n", "{}"); err != nil {
		t.Fatal(err)
	}

	mkStep := func(slug, compRoot string) store.Step {
		st, err := s.CreateStep(store.Scope{LoopSlug: slug, ComponentRoot: compRoot},
			store.NewStep{Phase: "build", Agent: "x", Status: store.StepRunning})
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	turn := func(st store.Step, iteration int, usage model.Usage, cost model.Cost, route string) {
		if err := s.RecordStepTurn(st.ID, iteration, usage, cost, 10, route, store.TelemetryCategoryAssistantOutput); err != nil {
			t.Fatal(err)
		}
	}

	// loopA (trigger trig-1, goal works epicA): a known step of $10 across
	// two routes, and a partial step of $7 on r1.
	stA1 := mkStep("spend-a", rootA)
	if err := s.SetStepCost(stA1.ID, model.Cost{State: model.CostKnown, USD: 10}); err != nil {
		t.Fatal(err)
	}
	turn(stA1, 1, model.Usage{InputTokens: model.IntPtr(1000)}, model.Cost{State: model.CostKnown, USD: 4}, "r1")
	turn(stA1, 2, model.Usage{InputTokens: model.IntPtr(2000)}, model.Cost{State: model.CostKnown, USD: 6}, "r2")
	stA2 := mkStep("spend-a", rootA)
	if err := s.SetStepCost(stA2.ID, model.Cost{State: model.CostPartial, USDAtLeast: 7}); err != nil {
		t.Fatal(err)
	}
	turn(stA2, 1, model.Usage{InputTokens: model.IntPtr(500)}, model.Cost{State: model.CostPartial, USDAtLeast: 7}, "r1")

	// loopB (no trigger, no goal): one unknown step on r3 (no tokens at all).
	stB1 := mkStep("spend-b", rootB)
	if err := s.SetStepCost(stB1.ID, model.Cost{State: model.CostUnknown, Reason: "no price"}); err != nil {
		t.Fatal(err)
	}
	turn(stB1, 1, model.Usage{}, model.Cost{State: model.CostUnknown, Reason: "no price"}, "r3")

	// The day key is the store's own stamp (the real clock) — derive it from
	// the step row, never a literal.
	row, err := s.ReadStep(stA1.ID)
	if err != nil {
		t.Fatal(err)
	}
	day := row.StartedAt[:10]

	report := func(p protocol.SpendReportParams) protocol.SpendReportResult {
		res, err := t15Dispatch(t, d, protocol.MethodSpendReport, p)
		if err != nil {
			t.Fatalf("spend.report %s: %v", p.GroupBy, err)
		}
		return res.(protocol.SpendReportResult)
	}
	bucket := func(r protocol.SpendReportResult, key string) protocol.SpendReportBucket {
		for _, b := range r.Buckets {
			if b.Key == key {
				return b
			}
		}
		t.Fatalf("the report has no %q bucket: %+v", key, r.Buckets)
		return protocol.SpendReportBucket{}
	}
	// Every grain sums to the same spend: floor 17, exactly one unknown. The
	// step total is the grain's own count: the step-grain groupings count a
	// step once, but route counts it once PER route it touched (r1=2, r2=1,
	// r3=1 — the crossing step is counted in both of its routes).
	checkTotals := func(groupBy string, wantSteps int, p protocol.SpendReportParams) {
		r := report(p)
		if r.GroupBy != groupBy {
			t.Fatalf("groupBy echoed %q, want %q", r.GroupBy, groupBy)
		}
		var floor float64
		unknown, steps := 0, 0
		for _, b := range r.Buckets {
			floor += b.USDFloor
			unknown += b.UnknownCount
			steps += b.Steps
		}
		if floor != 17 || unknown != 1 || steps != wantSteps {
			t.Fatalf("groupBy %s sums to floor %v, unknown %d, steps %d — want 17/1/%d: %+v", groupBy, floor, unknown, steps, wantSteps, r.Buckets)
		}
	}

	// day: one bucket (all three steps started today), floor 17.
	rDay := report(protocol.SpendReportParams{GroupBy: "day"})
	if len(rDay.Buckets) != 1 {
		t.Fatalf("day report has %d buckets, want 1: %+v", len(rDay.Buckets), rDay.Buckets)
	}
	b := bucket(rDay, day)
	if b.USDFloor != 17 || b.PartialCount != 1 || b.UnknownCount != 1 || b.Steps != 3 ||
		b.Tokens == nil || *b.Tokens != 3500 || b.Exact {
		t.Fatalf("day bucket = %+v, want floor 17 partial 1 unknown 1 steps 3 tokens 3500 inexact", b)
	}
	checkTotals("day", 3, protocol.SpendReportParams{GroupBy: "day"})

	// trigger: the triggered loop's two steps, and the person-started one.
	rTrig := report(protocol.SpendReportParams{GroupBy: "trigger"})
	if b := bucket(rTrig, "trig-1"); b.USDFloor != 17 || b.PartialCount != 1 || b.UnknownCount != 0 ||
		b.Steps != 2 || b.Tokens == nil || *b.Tokens != 3500 || b.Exact {
		t.Fatalf("trig-1 bucket = %+v", b)
	}
	if b := bucket(rTrig, "none"); b.USDFloor != 0 || b.UnknownCount != 1 || b.Steps != 1 || b.Tokens != nil {
		t.Fatalf("none bucket = %+v — the unknown is counted, and no priced tokens is a nil floor", b)
	}
	checkTotals("trigger", 3, protocol.SpendReportParams{GroupBy: "trigger"})

	// component: the loop's component_root, per root.
	rComp := report(protocol.SpendReportParams{GroupBy: "component"})
	if b := bucket(rComp, rootA); b.USDFloor != 17 || b.Steps != 2 || b.UnknownCount != 0 {
		t.Fatalf("component rootA bucket = %+v", b)
	}
	if b := bucket(rComp, rootB); b.USDFloor != 0 || b.UnknownCount != 1 || b.Steps != 1 || b.Tokens != nil {
		t.Fatalf("component rootB bucket = %+v", b)
	}
	checkTotals("component", 3, protocol.SpendReportParams{GroupBy: "component"})

	// epic: the goal's epic, and 'none' for the goal-less loop.
	rEpic := report(protocol.SpendReportParams{GroupBy: "epic"})
	if b := bucket(rEpic, epicA.ID); b.USDFloor != 17 || b.Steps != 2 || b.UnknownCount != 0 {
		t.Fatalf("epic A bucket = %+v", b)
	}
	if b := bucket(rEpic, "none"); b.USDFloor != 0 || b.UnknownCount != 1 || b.Steps != 1 || b.Tokens != nil {
		t.Fatalf("epic none bucket = %+v", b)
	}
	checkTotals("epic", 3, protocol.SpendReportParams{GroupBy: "epic"})

	// route: the only turn-level grain — a step crossing routes pays each
	// route exactly its turns (r1 holds $4 from stepA1 and $7 from stepA2).
	rRoute := report(protocol.SpendReportParams{GroupBy: "route"})
	if b := bucket(rRoute, "r1"); b.USDFloor != 11 || b.PartialCount != 1 || b.UnknownCount != 0 ||
		b.Steps != 2 || b.Tokens == nil || *b.Tokens != 1500 || b.Exact {
		t.Fatalf("r1 bucket = %+v, want floor 11 partial 1 steps 2 tokens 1500 inexact", b)
	}
	if b := bucket(rRoute, "r2"); b.USDFloor != 6 || b.Steps != 1 || b.Tokens == nil || *b.Tokens != 2000 || !b.Exact {
		t.Fatalf("r2 bucket = %+v, want floor 6 steps 1 tokens 2000 exact", b)
	}
	if b := bucket(rRoute, "r3"); b.USDFloor != 0 || b.UnknownCount != 1 || b.Steps != 1 || b.Tokens != nil {
		t.Fatalf("r3 bucket = %+v — the unknown turn counts, with no token floor", b)
	}
	checkTotals("route", 4, protocol.SpendReportParams{GroupBy: "route"})

	// RootID scopes the report to one project.
	if b := bucket(report(protocol.SpendReportParams{GroupBy: "day", RootID: rootA}), day); b.USDFloor != 17 || b.Steps != 2 || b.UnknownCount != 0 {
		t.Fatalf("rootA-scoped day bucket = %+v", b)
	}
	if r := report(protocol.SpendReportParams{GroupBy: "day", RootID: rootB}); len(r.Buckets) != 1 {
		t.Fatalf("rootB-scoped report = %+v", r.Buckets)
	} else if b := r.Buckets[0]; b.USDFloor != 0 || b.Steps != 1 || b.UnknownCount != 1 {
		t.Fatalf("rootB-scoped day bucket = %+v", b)
	}

	// A bad window is a param error that says which bound moved.
	if _, err := t15Dispatch(t, d, protocol.MethodSpendReport,
		protocol.SpendReportParams{GroupBy: "day", Since: "2026-11-02T00:00:00Z", Until: "2026-11-01T00:00:00Z"}); err == nil ||
		!strings.Contains(err.Error(), "since is after until") {
		t.Fatalf("an inverted window got %v — the refusal must name the window", err)
	}
}

// (4) budget.set classifies every change: a set that LOWERS or ADDS a cap
// tightens authority and commits directly (no confirm); a set that RAISES an
// enabled cap loosens it and commits only against the digest of the
// canonical statement (PM7.1). A dryRun says what the commit would say and
// writes nothing; a no-op writes nothing at all (LC11); and the actor the
// commit stamps is the connection's peer, never a params field.
func TestBudgetSetLoweringNeedsNoConfirmRaisingDoes(t *testing.T) {
	d, _, s, _ := t15Daemon(t)

	// The key (scope, subjectId, window) with one cap at a time — the set
	// door's create-or-update discriminator.
	setP := func(p protocol.BudgetSetParams) protocol.BudgetSetParams {
		p.Scope, p.SubjectID, p.Window = "workspace", "workspace", "lifetime"
		return p
	}

	// Create: a cap that did not exist now exists — tightening, no confirm.
	res, err := t15Dispatch(t, d, protocol.MethodBudgetSet, setP(protocol.BudgetSetParams{USDCap: capFloat(100)}))
	if err != nil {
		t.Fatal(err)
	}
	set := res.(protocol.BudgetSetResult)
	if set.Effect != "created" || set.Confirmed || set.View == nil {
		t.Fatalf("create answered %+v, want created with a view and no confirm", set)
	}
	id := set.View.ID
	wantCreate := "create budget workspace/workspace/lifetime: caps usd=100; unknown policy proceed"
	if set.Statement != wantCreate {
		t.Fatalf("create statement = %q, want %q", set.Statement, wantCreate)
	}

	// Lower 100 → 50: tightening again, no confirm.
	res, err = t15Dispatch(t, d, protocol.MethodBudgetSet, setP(protocol.BudgetSetParams{USDCap: capFloat(50)}))
	if err != nil {
		t.Fatal(err)
	}
	set = res.(protocol.BudgetSetResult)
	if set.Effect != "updated" || set.Confirmed {
		t.Fatalf("lower answered %+v, want updated without confirm", set)
	}
	wantLower := "budget " + id + ": set the usd cap from 100 to 50"
	if set.Statement != wantLower {
		t.Fatalf("lower statement = %q, want %q", set.Statement, wantLower)
	}

	// Raise 50 → 200 with no digest: the daemon renders the canonical
	// statement and refuses until the digest of THAT statement commits it.
	_, err = t15Dispatch(t, d, protocol.MethodBudgetSet, setP(protocol.BudgetSetParams{USDCap: capFloat(200)}))
	pe := budgetErrorOf(t, err)
	if pe.Code != protocol.ErrBudgetRefused.Code {
		t.Fatalf("the unconfirmed raise refused with code %d, want %d", pe.Code, protocol.ErrBudgetRefused.Code)
	}
	if pe.Data.Kind != "confirm-required" {
		t.Fatalf("kind = %q, want confirm-required", pe.Data.Kind)
	}
	wantRaise := "budget " + id + ": set the usd cap from 50 to 200"
	if pe.Data.Statement != wantRaise {
		t.Fatalf("statement = %q, want %q — the client completes in one round trip from THIS", pe.Data.Statement, wantRaise)
	}
	if pe.Data.Confirm != budgetDigestHex(t, wantRaise) {
		t.Fatalf("confirm = %s, want the digest of the statement it showed", pe.Data.Confirm)
	}
	if pe.Data.What != "budget set refused: this act loosens authority (raise the usd cap from 50 to 200) — commit it with the digest of the statement as read" {
		t.Fatalf("what = %q", pe.Data.What)
	}

	// A digest that does not match the row AS IT IS NOW is a mismatch — the
	// act is refused, and the row is still 50.
	wrong := strings.Repeat("0", 64)
	_, err = t15Dispatch(t, d, protocol.MethodBudgetSet,
		setP(protocol.BudgetSetParams{USDCap: capFloat(200), Confirm: wrong}))
	pe = budgetErrorOf(t, err)
	if pe.Code != protocol.ErrBudgetRefused.Code || pe.Data.Kind != "confirm-mismatch" {
		t.Fatalf("the wrong digest got code %d kind %q, want confirm-mismatch", pe.Code, pe.Data.Kind)
	}
	if pe.Data.What != "budget set refused: the digest does not match the statement rendered from the row as it is now (it moved since you read it) — read it again and commit with the new digest" {
		t.Fatalf("what = %q", pe.Data.What)
	}
	b, err := s.GetBudget(id)
	if err != nil || b.USD == nil || *b.USD != 50 {
		t.Fatalf("the mismatched commit moved the row: %v / %v", err, b.USD)
	}

	// The digest of the statement the daemon showed commits it.
	res, err = t15Dispatch(t, d, protocol.MethodBudgetSet,
		setP(protocol.BudgetSetParams{USDCap: capFloat(200), Confirm: budgetDigestHex(t, wantRaise)}))
	if err != nil {
		t.Fatal(err)
	}
	set = res.(protocol.BudgetSetResult)
	if set.Effect != "updated" || !set.Confirmed || set.View == nil || set.View.Dimensions[0].Cap == nil || *set.View.Dimensions[0].Cap != 200 {
		t.Fatalf("the confirmed raise answered %+v, want updated confirmed at cap 200", set)
	}

	// A dryRun says what a 200 → 300 raise would commit, and writes nothing.
	res, err = t15Dispatch(t, d, protocol.MethodBudgetSet, setP(protocol.BudgetSetParams{USDCap: capFloat(300), DryRun: true}))
	if err != nil {
		t.Fatal(err)
	}
	set = res.(protocol.BudgetSetResult)
	if !set.DryRun || set.Effect != "updated" || set.View != nil {
		t.Fatalf("dryRun answered %+v, want the effect with statement and no view", set)
	}
	wantDry := "budget " + id + ": set the usd cap from 200 to 300"
	if set.Statement != wantDry || set.Digest != budgetDigestHex(t, wantDry) {
		t.Fatalf("dryRun statement/digest = %q / %s, want the 200→300 raise bound", set.Statement, set.Digest)
	}
	if b, _ := s.GetBudget(id); b.USD == nil || *b.USD != 200 {
		t.Fatalf("the dryRun wrote the row: cap %v", b.USD)
	}

	// A no-op changes nothing and writes nothing (LC11): no audit row.
	before := budgetChangeCount(t, s, id)
	res, err = t15Dispatch(t, d, protocol.MethodBudgetSet, setP(protocol.BudgetSetParams{USDCap: capFloat(200)}))
	if err != nil {
		t.Fatal(err)
	}
	set = res.(protocol.BudgetSetResult)
	if set.Effect != "noop" || set.View == nil {
		t.Fatalf("the unchanged set answered %+v, want noop with the view", set)
	}
	if after := budgetChangeCount(t, s, id); after != before {
		t.Fatalf("the no-op wrote an audit row (%d → %d) — a no-op writes nothing", before, after)
	}

	// A params field named actor is refused by name (-32602): the actor is
	// the connection, never a value a caller chooses.
	raw, _ := json.Marshal(map[string]any{"scope": "workspace", "subjectId": "workspace", "window": "lifetime", "usdCap": 200, "actor": "evil"})
	_, err = d.Table.Dispatch(context.Background(), "c1", protocol.MethodBudgetSet, raw)
	msg := badParamsOf(t, err)
	if !strings.Contains(msg, "actor is not a param — the actor is the connection (the daemon's peer credential), never a value a caller chooses") {
		t.Fatalf("the smuggled actor got %q, want the refusal naming the actor", msg)
	}

	// The actor the COMMIT stamped is the kernel's peer credential.
	res, err = t15Dispatch(t, d, protocol.MethodBudgetShow, protocol.BudgetShowParams{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	show := res.(protocol.BudgetShowResult)
	if show.View.LastChanged == nil || show.View.LastChanged.Kind != "changed" ||
		show.View.LastChanged.ByKind != "user" || show.View.LastChanged.ByID != "4321" {
		t.Fatalf("lastChanged = %+v, want changed by user/4321 — the peer credential the kernel reported", show.View.LastChanged)
	}
	events, err := s.ListBudgetEvents(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	sawChanged := false
	for _, e := range events {
		if e.Kind == "changed" && e.ActorKind == "user" && e.ActorID != "4321" {
			t.Fatalf("the changed row was stamped %s/%s — the kernel puts the peer uid there", e.ActorKind, e.ActorID)
		}
		if e.Kind == "changed" {
			sawChanged = true
		}
	}
	if !sawChanged {
		t.Fatalf("the audit has no changed row: %v", kindsOfBudgetEvents(t, s, id))
	}
}

// (5) budget.remove ALWAYS confirms: a removal loosens authority (a cap that
// is gone will never fire), so the digest of the removal statement is the
// only thing that lets it commit. A missing budget is a param that names
// nothing (-32602), not a refusal — there is nothing to remove.
func TestBudgetRemoveNeedsTheDigest(t *testing.T) {
	d, _, s, _ := t15Daemon(t)

	res, err := t15Dispatch(t, d, protocol.MethodBudgetSet,
		protocol.BudgetSetParams{Scope: "workspace", SubjectID: "workspace", Window: "lifetime", USDCap: capFloat(900)})
	if err != nil {
		t.Fatal(err)
	}
	id := res.(protocol.BudgetSetResult).View.ID

	// No digest: the daemon renders the removal's canonical statement.
	_, err = t15Dispatch(t, d, protocol.MethodBudgetRemove, protocol.BudgetRemoveParams{ID: id})
	pe := budgetErrorOf(t, err)
	if pe.Code != protocol.ErrBudgetRefused.Code || pe.Data.Kind != "confirm-required" {
		t.Fatalf("the unconfirmed remove got code %d kind %q, want confirm-required", pe.Code, pe.Data.Kind)
	}
	wantRemove := "remove budget " + id + " (workspace workspace, lifetime): in-force caps usd=900 — after this, no cap applies at this subject"
	if pe.Data.Statement != wantRemove || pe.Data.Confirm != budgetDigestHex(t, wantRemove) {
		t.Fatalf("remove statement/digest = %q / %s, want the 900 removal bound", pe.Data.Statement, pe.Data.Confirm)
	}
	if pe.Data.What != "budget remove refused: removing a budget loosens authority (its caps stop applying) — commit it with the digest of the statement as read" {
		t.Fatalf("what = %q", pe.Data.What)
	}

	// A wrong digest is a mismatch, and the row is still there.
	_, err = t15Dispatch(t, d, protocol.MethodBudgetRemove, protocol.BudgetRemoveParams{ID: id, Confirm: strings.Repeat("0", 64)})
	pe = budgetErrorOf(t, err)
	if pe.Code != protocol.ErrBudgetRefused.Code || pe.Data.Kind != "confirm-mismatch" {
		t.Fatalf("the wrong digest got code %d kind %q, want confirm-mismatch", pe.Code, pe.Data.Kind)
	}
	if pe.Data.What != "budget remove refused: the digest does not match the statement rendered from the row as it is now (it moved since you read it) — read it again and remove with the new digest" {
		t.Fatalf("what = %q", pe.Data.What)
	}
	if _, err := s.GetBudget(id); err != nil {
		t.Fatalf("the mismatched remove deleted the row: %v", err)
	}

	// The digest of the statement commits it; the audit row stays (LC11).
	res, err = t15Dispatch(t, d, protocol.MethodBudgetRemove,
		protocol.BudgetRemoveParams{ID: id, Confirm: budgetDigestHex(t, wantRemove)})
	if err != nil {
		t.Fatal(err)
	}
	rem := res.(protocol.BudgetRemoveResult)
	if rem.ID != id || !rem.Removed {
		t.Fatalf("remove answered %+v, want removed", rem)
	}
	if _, err := s.GetBudget(id); err == nil {
		t.Fatal("the row is still there after the confirmed remove")
	} else if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the read of the removed budget answered %v, want ErrNotFound", err)
	}
	events, err := s.ListBudgetEvents(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	sawRemoved := false
	for _, e := range events {
		if e.Kind == "removed" {
			sawRemoved = true
			if e.ActorKind != "user" || e.ActorID != "4321" {
				t.Fatalf("the removed row was stamped %s/%s, want user/4321", e.ActorKind, e.ActorID)
			}
		}
	}
	if !sawRemoved {
		t.Fatalf("the audit has no removed row (LC11: the history outlives the budget): %v", kindsOfBudgetEvents(t, s, id))
	}

	// A budget that is not in the store is a param that names nothing —
	// -32602 on both the set-side read and the remove door.
	_, err = t15Dispatch(t, d, protocol.MethodBudgetShow, protocol.BudgetShowParams{ID: "bud-nope"})
	msg := badParamsOf(t, err)
	if !strings.Contains(msg, `no budget "bud-nope" — the id names a budget that is not in this store`) {
		t.Fatalf("the missing budget on show got %q, want the refusal naming the id", msg)
	}
	_, err = t15Dispatch(t, d, protocol.MethodBudgetRemove, protocol.BudgetRemoveParams{ID: "bud-nope"})
	msg = badParamsOf(t, err)
	if !strings.Contains(msg, `no budget "bud-nope" — the id names a budget that is not in this store`) {
		t.Fatalf("the missing budget on remove got %q, want the refusal naming the id", msg)
	}
}

// (6) loop.raise is the person's door over the T7 in-process op. The door
// adds the two checks the op cannot make — the dimension the caller named is
// the dimension the loop is parked on, and a named budgetId names the parked
// budget — and it stamps the audit with the connection's peer. The raise
// lifts the cap FIRST (the raised row, this actor in it) and moves the loop
// in the hit's shape: an in-step suspension re-runs the parked step from its
// checkpoint (`step`), a pre-dispatch block goes back to running under a
// fresh runner (`runner`).
func TestRaiseEndToEnd(t *testing.T) {
	t.Run("in-step", func(t *testing.T) {
		enter0, release0 := make(chan struct{}), make(chan struct{})
		f, m := costedFixture(t, []costedTurn{
			{deltas: []string{"pricing the work"}, enter: enter0, release: release0,
				toolUses: []model.Block{readUse("t1")},
				usage:    model.Usage{InputTokens: model.IntPtr(1000), OutputTokens: model.IntPtr(500)}, costUSD: 12},
			{deltas: []string{okJSON}},
		})
		initConn(t, f.d, context.Background(), "c1", "raiser")
		f.w.conns = []localrpc.ConnInfo{{ID: "c1", PeerUID: budgetPeerUID, PeerPID: 99}}
		sub := subscribe(t, f.d, "c1", protocol.SubscribeParams{Filter: protocol.Filter{ComponentRoot: f.root}})
		loopID := startStreamLoop(t, f, writeStreamBlueprint(t, "read-only", "solo"))

		<-enter0
		step1, ok := stepIDFromEvents(t, f, "c1", sub.SubscriptionID, "solo", 0)
		if !ok {
			t.Fatal("no step.started for the in-flight step")
		}
		budgetID := makeLoopBudget(t, f.s, loopID, 10) // the cap the spend will cross
		close(release0)
		blockedOnCapHit(t, f, "c1", sub.SubscriptionID, loopID)

		raise := func(raw string) (any, error) {
			return f.d.Table.Dispatch(context.Background(), "c1", protocol.MethodLoopRaise, json.RawMessage(raw))
		}

		// The wrong DIMENSION: a raise lifts the cap the hit tripped — the
		// tokens param against a usd hit is not the door for it.
		_, err := raise(fmt.Sprintf(`{"id":"%s","tokensCap":5000,"reason":"tokens room"}`, loopID))
		pe := budgetErrorOf(t, err)
		if pe.Code != protocol.ErrBudgetRefused.Code || pe.Data.Kind != "wrong-dimension" {
			t.Fatalf("the wrong dimension got code %d kind %q, want wrong-dimension", pe.Code, pe.Data.Kind)
		}
		if pe.Data.What != "budget raise refused: the loop is parked on the usd cap — a raise lifts that dimension; the tokensCap param is not the door for it" {
			t.Fatalf("what = %q", pe.Data.What)
		}

		// The wrong BUDGET: a named budgetId must name the parked budget.
		dayB, ref, err := f.s.CreateBudget(store.BudgetActor{Kind: "user", ID: "u1"}, store.NewBudgetIn{
			Scope: "loop", Subject: loopID, Window: "day", USD: capFloat(100)})
		if err != nil || ref.Kind != "" {
			t.Fatalf("the day budget: %v %v", ref, err)
		}
		_, err = raise(fmt.Sprintf(`{"id":"%s","usdCap":100,"budgetId":"%s","reason":"x"}`, loopID, dayB.ID))
		pe = budgetErrorOf(t, err)
		if pe.Code != protocol.ErrBudgetRefused.Code || pe.Data.Kind != "wrong-budget" {
			t.Fatalf("the wrong budget got code %d kind %q, want wrong-budget", pe.Code, pe.Data.Kind)
		}
		if pe.Data.What != "budget raise refused: the loop is parked on budget "+budgetID+" — a raise lifts the cap the hit tripped, not another budget's" {
			t.Fatalf("what = %q", pe.Data.What)
		}

		// A smuggled actor is a parse error the caller sees.
		_, err = raise(fmt.Sprintf(`{"id":"%s","usdCap":100,"reason":"x","actor":"evil"}`, loopID))
		msg := badParamsOf(t, err)
		if !strings.Contains(msg, "actor is not a param — the actor is the connection (the daemon's peer credential), never a value a caller chooses") {
			t.Fatalf("the smuggled actor got %q, want the refusal naming the actor", msg)
		}

		// The reason is the audit's own words: required, 1–512 bytes.
		_, err = raise(fmt.Sprintf(`{"id":"%s","usdCap":100}`, loopID))
		msg = badParamsOf(t, err)
		if !strings.Contains(msg, "reason is required (1–512 bytes of text)") {
			t.Fatalf("the missing reason got %q, want the refusal naming the bound", msg)
		}

		// A raise names exactly ONE new cap — two is a parse error.
		_, err = raise(fmt.Sprintf(`{"id":"%s","usdCap":100,"tokensCap":5,"reason":"x"}`, loopID))
		msg = badParamsOf(t, err)
		if !strings.Contains(msg, "name exactly one new cap (usdCap, tokensCap, wallMsCap or stepsCap) — named 2") {
			t.Fatalf("the two caps got %q, want the refusal counting them", msg)
		}

		// The raise: the cap lifts first, the loop moves in the step's shape.
		res, err := raise(fmt.Sprintf(`{"id":"%s","usdCap":100,"reason":"room to finish"}`, loopID))
		if err != nil {
			t.Fatal(err)
		}
		got := res.(protocol.LoopRaiseResult)
		if got.LoopID != loopID || got.Raised == nil {
			t.Fatalf("raise answered %+v, want the loop with its raised cap", got)
		}
		if got.Raised.BudgetID != budgetID || got.Raised.Dimension != "usd" ||
			got.Raised.From == nil || *got.Raised.From != 10 || got.Raised.To != 100 {
			t.Fatalf("raised = %+v, want %s usd from 10 to 100", got.Raised, budgetID)
		}
		if !got.Resumed || got.Status != "waiting" || got.How != "step" || got.Refusal != nil {
			t.Fatalf("raise = %+v, want resumed waiting via step with no refusal", got)
		}
		if !strings.Contains(got.Note, "raised the usd cap of "+budgetID+" to 100") ||
			!strings.Contains(got.Note, "the parked step re-runs from its checkpoint; the loop resumes when it settles") {
			t.Fatalf("note = %q — it must name the cap it lifted and the door the step has", got.Note)
		}
		if c := capOf(t, f.s, budgetID); c != 100 {
			t.Fatalf("the cap read %v after the raise, want 100", c)
		}
		events, err := f.s.ListBudgetEvents(budgetID, 0)
		if err != nil {
			t.Fatal(err)
		}
		sawRaised := false
		for _, e := range events {
			if e.Kind != "raised" {
				continue
			}
			sawRaised = true
			if e.ActorKind != "user" || e.ActorID != "4321" {
				t.Fatalf("the raised row was stamped %s/%s — the peer credential, not a params value", e.ActorKind, e.ActorID)
			}
			if e.OldValue == nil || *e.OldValue != 10 || e.NewValue == nil || *e.NewValue != 100 {
				t.Fatalf("the raised row's numbers = %v / %v, want 10 → 100", e.OldValue, e.NewValue)
			}
		}
		if !sawRaised {
			t.Fatalf("budget audit = %v, want a raised row", kindsOfBudgetEvents(t, f.s, budgetID))
		}

		// The parked step re-runs from its checkpoint and the loop finishes.
		waitFor(t, "the re-run's model call", func() bool { return m.callCount() == 2 })
		pin := waitLoopDone(t, f, loopID)
		if pin.Loop.Status != "done" {
			t.Fatalf("loop status = %q after the raise, want done (runtime %s)", pin.Loop.Status, pin.RuntimeJSON)
		}
		row, err := f.s.ReadStep(step1)
		if err != nil {
			t.Fatal(err)
		}
		if row.Status != "done" {
			t.Fatalf("the raised step read %q, want done", row.Status)
		}
	})

	t.Run("runner", func(t *testing.T) {
		enter0, release0 := make(chan struct{}), make(chan struct{})
		f, m := costedFixture(t, []costedTurn{
			{deltas: []string{"the first step"}, enter: enter0, release: release0,
				usage: model.Usage{InputTokens: model.IntPtr(1000), OutputTokens: model.IntPtr(500)}, costUSD: 5},
			{deltas: []string{okJSON}},
			{deltas: []string{okJSON}},
		})
		initConn(t, f.d, context.Background(), "c1", "raiser")
		f.w.conns = []localrpc.ConnInfo{{ID: "c1", PeerUID: budgetPeerUID, PeerPID: 99}}
		sub := subscribe(t, f.d, "c1", protocol.SubscribeParams{Filter: protocol.Filter{ComponentRoot: f.root}})
		loopID := startStreamLoop(t, f, writeStreamBlueprint(t, "read-only", "one", "two"))

		<-enter0
		// A zero cap: the floor (0) already sits on it — the second step's
		// dispatch is refused before any step row exists.
		budgetID := makeLoopBudget(t, f.s, loopID, 0)
		close(release0)
		blockedOnCapHit(t, f, "c1", sub.SubscriptionID, loopID)

		res, err := f.d.Table.Dispatch(context.Background(), "c1", protocol.MethodLoopRaise,
			json.RawMessage(fmt.Sprintf(`{"id":"%s","usdCap":100,"reason":"go"}`, loopID)))
		if err != nil {
			t.Fatal(err)
		}
		got := res.(protocol.LoopRaiseResult)
		if got.Raised == nil || got.Raised.BudgetID != budgetID || got.Raised.Dimension != "usd" ||
			got.Raised.From == nil || *got.Raised.From != 0 || got.Raised.To != 100 {
			t.Fatalf("raised = %+v, want %s usd from 0 to 100", got.Raised, budgetID)
		}
		if !got.Resumed || got.Status != "running" || got.How != "runner" || got.Refusal != nil {
			t.Fatalf("raise = %+v, want resumed running via runner with no refusal", got)
		}
		if !strings.Contains(got.Note, "a fresh runner re-dispatches against the raised cap") {
			t.Fatalf("note = %q — the runner shape names its re-dispatch", got.Note)
		}
		pin := waitLoopSettledDiag(t, f, "c1", sub.SubscriptionID, loopID)
		if pin.Loop.Status != "done" {
			t.Fatalf("loop status = %q after the raise, want done (runtime %s)", pin.Loop.Status, pin.RuntimeJSON)
		}
		if n := m.callCount(); n != 3 {
			t.Fatalf("the model was called %d times, want 3 (the re-dispatch ran lane two)", n)
		}
	})
}

// (7) The epic's budget is the budget ROW (LC13): the epic view's budgetUsd
// is derived from the epic's lifetime budget, and the door that writes it is
// budget.set — never an epic write. An epic update that changes the plan
// leaves the cap alone, and a budget.remove (with its digest) reads the view
// back to nil (no USD cap, P31).
func TestEpicBudgetWritesThroughTheTable(t *testing.T) {
	d, _, s, _ := t15Daemon(t)

	roots, err := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	if err != nil || len(roots) == 0 {
		t.Fatalf("no root component: %v", err)
	}
	epic, err := s.CreateEpic(store.NewEpic{ComponentID: roots[0].ID, Title: "funded"})
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.GetEpic(epic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e.BudgetUSD != nil {
		t.Fatalf("a fresh epic's view carries budgetUsd %v — nil is no USD cap, not a hidden zero", *e.BudgetUSD)
	}

	// budget.set files the epic's lifetime budget; the view derives it.
	res, err := t15Dispatch(t, d, protocol.MethodBudgetSet, protocol.BudgetSetParams{
		Scope: "epic", SubjectID: epic.ID, Window: "lifetime", USDCap: capFloat(250)})
	if err != nil {
		t.Fatal(err)
	}
	budgetID := res.(protocol.BudgetSetResult).View.ID
	e, err = s.GetEpic(epic.ID)
	if err != nil || e.BudgetUSD == nil || *e.BudgetUSD != 250 {
		t.Fatalf("the epic view after the set = %v / %v, want budgetUsd 250", e.BudgetUSD, err)
	}

	// Raising it is a confirmed act (PM7.1), like every cap raise.
	_, err = t15Dispatch(t, d, protocol.MethodBudgetSet, protocol.BudgetSetParams{
		Scope: "epic", SubjectID: epic.ID, Window: "lifetime", USDCap: capFloat(400)})
	pe := budgetErrorOf(t, err)
	if pe.Code != protocol.ErrBudgetRefused.Code || pe.Data.Kind != "confirm-required" {
		t.Fatalf("the unconfirmed raise got code %d kind %q, want confirm-required", pe.Code, pe.Data.Kind)
	}
	if _, err := t15Dispatch(t, d, protocol.MethodBudgetSet, protocol.BudgetSetParams{
		Scope: "epic", SubjectID: epic.ID, Window: "lifetime", USDCap: capFloat(400), Confirm: pe.Data.Confirm}); err != nil {
		t.Fatal(err)
	}
	e, err = s.GetEpic(epic.ID)
	if err != nil || e.BudgetUSD == nil || *e.BudgetUSD != 400 {
		t.Fatalf("the epic view after the confirmed raise = %v / %v, want 400", e.BudgetUSD, err)
	}

	// A plan edit leaves the cap alone (the cap's door is budget.set).
	if _, err := s.UpdateEpic(store.EpicEdit{ID: epic.ID, Title: "funded v2"}); err != nil {
		t.Fatal(err)
	}
	e, err = s.GetEpic(epic.ID)
	if err != nil || e.Title != "funded v2" || e.BudgetUSD == nil || *e.BudgetUSD != 400 {
		t.Fatalf("the plan edit moved the cap: %v / %v", e.BudgetUSD, err)
	}

	// A confirmed remove reads the view back to nil.
	_, err = t15Dispatch(t, d, protocol.MethodBudgetRemove, protocol.BudgetRemoveParams{ID: budgetID})
	pe = budgetErrorOf(t, err)
	if pe.Code != protocol.ErrBudgetRefused.Code || pe.Data.Kind != "confirm-required" || pe.Data.Statement == "" || pe.Data.Confirm == "" {
		t.Fatalf("the unconfirmed remove got code %d kind %q, want the statement bound", pe.Code, pe.Data.Kind)
	}
	if _, err := t15Dispatch(t, d, protocol.MethodBudgetRemove,
		protocol.BudgetRemoveParams{ID: budgetID, Confirm: pe.Data.Confirm}); err != nil {
		t.Fatal(err)
	}
	e, err = s.GetEpic(epic.ID)
	if err != nil || e.BudgetUSD != nil {
		t.Fatalf("the epic view after the remove = %v / %v, want no USD cap", e.BudgetUSD, err)
	}
}

// (8) An agent CANNOT set a budget, by construction: the person's doors
// refuse epic authorship entirely (the person decides what an epic means —
// LC13), and the agent's records decode is STRICT — a budget figure the
// model types is an unknown field refused by name before it reaches the
// store. The catalog names no budget method at all: the budget plane's
// eight doors are the socket's, and the agent reaches them nowhere.
func TestAgentCannotSetABudget(t *testing.T) {
	_, _, s, root := t15Daemon(t)

	roots, err := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	if err != nil || len(roots) == 0 {
		t.Fatalf("no root component: %v", err)
	}
	pin, err := s.CreatePinnedLoop(store.NewLoop{Slug: "agent-budget", ComponentRoot: root, Root: root, Goal: "g", Status: "running"},
		"dev-loop", 1, "id: dev-loop\n", "{}")
	if err != nil {
		t.Fatal(err)
	}
	step := startLaneStep(t, s, pin.Slug, root, "planner")
	svc := NewRecordsService(s)
	ctx := stepctx.WithStepID(context.Background(), step)
	by := Actor{Kind: "agent", ID: "planner"}

	// The positive control: the agent files an epic through its own door,
	// and the view's budgetUsd is nil until a person's budget.set says more.
	out, f, err := svc.Do(ctx, by, "epic_create", json.RawMessage(
		fmt.Sprintf(`{"root_id":"%s","title":"funded"}`, roots[0].ID)))
	if err != nil || f != nil {
		t.Fatalf("the agent's epic_create: %v %v", f, err)
	}
	epic, ok := out.(store.Epic)
	if !ok {
		t.Fatalf("epic_create answered %T, want the epic", out)
	}
	if epic.BudgetUSD != nil {
		t.Fatalf("a model-authored epic carries budgetUsd %v — the field is not in the agent's schema", *epic.BudgetUSD)
	}

	// A budget figure the model TYPES is refused by name, in BOTH arms —
	// strict decode, before the store (an ignored extra would be a door the
	// model did not know it was walking through).
	for action, input := range map[string]string{
		"epic_create": fmt.Sprintf(`{"root_id":"%s","title":"funded","budget_usd":250}`, roots[0].ID),
		"epic_update": fmt.Sprintf(`{"epic_id":"%s","budget_usd":400}`, epic.ID),
	} {
		_, f, err := svc.Do(ctx, by, action, json.RawMessage(input))
		if err != nil {
			t.Fatalf("%s with a budget figure errored: %v — a refusal is a result", action, err)
		}
		if f == nil {
			t.Fatalf("%s with a budget figure was ADMITTED — the agent set a budget through the records door", action)
		}
		want := `refused: json: unknown field "budget_usd" — identity fields (project, loop, component, author) are stamped by the step, and every other field belongs in the schema`
		if f.Sentence != want {
			t.Fatalf("%s refused with %q, want the sentence naming the field", action, f.Sentence)
		}
	}

	// The catalog names no budget WRITE: the plane's writes are the socket's
	// human doors, and an agent tool named after one is a door the guard
	// tests exist to keep shut. Amended 2026-11-01 (S55 T10): the premise
	// that NO spec may name the plane is superseded by the seven read twins
	// — the reads the socket answers, projected as tools over the SAME seam
	// (the T10 parity test pins them field for field) — allowed by method;
	// the write doors and budget.vocabulary stay the person's, and a spec
	// that names "budget" without being a twin is still refused here.
	budgetReadTwins := map[string]bool{ // the seven S55 T10 reads, by method
		"loop.control": true, "loop.requestStop": true, "budget.list": true,
		"budget.show": true, "budget.effective": true, "spend.report": true,
		"watchdog.list": true,
	}
	budgetMethods := map[string]bool{
		"budget.list": true, "budget.show": true, "budget.effective": true, "budget.set": true,
		"budget.remove": true, "budget.vocabulary": true, "spend.report": true, "loop.raise": true,
	}
	for _, sp := range records.Catalog() {
		if budgetMethods[sp.Method] && !budgetReadTwins[sp.Method] {
			t.Errorf("catalog spec %q is named after a budget-plane method %s — the budget write doors are the person's", sp.Name, sp.Method)
		}
		if !budgetReadTwins[sp.Method] && (strings.Contains(sp.Name, "budget") || strings.Contains(sp.Method, "budget")) {
			t.Errorf("catalog spec %q (%s) names the budget plane without being a T10 read twin", sp.Name, sp.Method)
		}
	}
}

// (9) The vocabulary comes from the DAEMON (rule 4): budget.vocabulary
// answers the closed sets, and a line in the client that carries a copy of
// one of those sets is a second truth that drifts when the set widens. The
// walk is the proof: every non-test source file in the CLI and the app is
// scanned for a line carrying three or more of a set's literals — the shape
// of a copy (a single use of one word is vocabulary, not a copy).
func TestBudgetVocabularyHasNoClientCopy(t *testing.T) {
	d, _, _, _ := t15Daemon(t)

	res, err := t15Dispatch(t, d, protocol.MethodBudgetVocabulary, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	voc := res.(protocol.BudgetVocabularyResult)
	same := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}
	if !same(voc.Scopes, []string{"workspace", "component", "epic", "loop"}) {
		t.Fatalf("scopes = %v", voc.Scopes)
	}
	if !same(voc.Windows, []string{"lifetime", "day"}) {
		t.Fatalf("windows = %v", voc.Windows)
	}
	if !same(voc.Dimensions, []string{"usd", "tokens", "wall-time", "steps"}) {
		t.Fatalf("dimensions = %v", voc.Dimensions)
	}
	if !same(voc.UnknownPolicies, []string{"proceed", "stop"}) {
		t.Fatalf("unknown policies = %v", voc.UnknownPolicies)
	}
	if !same(voc.Statuses, []string{"ok", "warn", "hit"}) {
		t.Fatalf("statuses = %v", voc.Statuses)
	}
	if !same(voc.EventKinds, []string{"created", "changed", "removed", "raised", "hit", "warned"}) {
		t.Fatalf("event kinds = %v", voc.EventKinds)
	}
	for _, kind := range []string{
		"no-dimension", "negative-cap", "warn-pct", "workspace-subject", "unknown-subject", "duplicate",
		"confirm-required", "confirm-mismatch", "wrong-budget", "wrong-dimension",
		"no-cap", "below-floor", "lowering", "running", "stopped", "waiting-question", "blocked",
		"idle", "terminal", "storeless",
	} {
		if !budgetKindIn(voc.RefusalKinds, kind) {
			t.Fatalf("refusal kinds are missing %q: %v", kind, voc.RefusalKinds)
		}
	}

	// The client-side copy walk (2026-11-01: 235 files, zero flagged lines).
	scopeLits := []string{`"workspace"`, `"component"`, `"epic"`, `"loop"`}
	dimLits := []string{`"usd"`, `"tokens"`, `"wall-time"`, `"steps"`}
	walked, flagged := 0, []string{}
	for _, dir := range []string{"../../cmd/aeon", "../../ui/src"} {
		err := filepath.WalkDir(dir, func(path string, ent fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if ent.IsDir() {
				name := ent.Name()
				if name == "node_modules" || strings.HasPrefix(name, ".") {
					return fs.SkipDir
				}
				return nil
			}
			name := ent.Name()
			if strings.HasSuffix(name, "_test.go") {
				return nil
			}
			switch strings.ToLower(filepath.Ext(name)) {
			case ".go", ".ts", ".tsx", ".js":
			default:
				return nil
			}
			walked++
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(raw), "\n") {
				scopeHits, dimHits := 0, 0
				for _, lit := range scopeLits {
					if strings.Count(line, lit) > 0 {
						scopeHits++
					}
				}
				for _, lit := range dimLits {
					if strings.Count(line, lit) > 0 {
						dimHits++
					}
				}
				if scopeHits >= 3 || dimHits >= 3 {
					flagged = append(flagged, fmt.Sprintf("%s:%d: %s", path, i+1, strings.TrimSpace(line)))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if walked < 235 {
		t.Fatalf("walked %d files, want >= 235 (2026-11-01: 235) — the walk must cover the client", walked)
	}
	if len(flagged) != 0 {
		t.Fatalf("client lines carrying a copy of the budget closed set: %v — the vocabulary comes from the daemon (budget.vocabulary)", flagged)
	}
}

// budgetKindIn is the small closed-set membership the vocabulary check needs
// (the package already has a `contains` in another test file — one name, one
// meaning).
func budgetKindIn(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
