package daemon

// budget_seam.go — the daemon's budget boundary (S55 T7): the store-backed
// seam behind the loop engine's pre-dispatch check (E1), the runtime's
// in-step check (E2) and the start-time refusal (E3) — and the in-process
// `loop.raise` op that lifts a cap a loop is parked on and re-runs the work
// the cap stopped.
//
// # The seam, not the engine
//
// The DECISION is the pure budget engine's (internal/budget): the daemon
// reads the applicable budgets and their spends from the store, hands the
// pair to Evaluate, and returns the verdict. The engine imports no store and
// the store imports no engine; this file is the one place the two shapes
// meet, and it caches NOTHING across calls (N8) — every check re-reads,
// because spend moves between checks and a cached floor is a cap that lies.
//
// # Warns are recorded here, not by the engine
//
// The engine's warn dedup is the `warned` map it is handed (the store's
// LastBudgetWarned); RECORDING a fresh warn is this seam's one write,
// because a warned row is the store's memory of what the engine already
// said. Each new warn is recorded, in order, before the verdict is returned:
// the check that crossed the line leaves the row, and the next check in the
// period is deduped by it.
//
// # Raise is in-process for T7
//
// The loop.raise op lives on the daemon, not the socket (the socket door is
// T8): it pins the loop, gates on loop.CheckRaise, lifts the cap through the
// store's RaiseBudget (the raised event; LC12's cap-only-goes-up rule), and
// then moves the loop by the hit's shape — an E1 pre-dispatch block goes back
// to running under a fresh runner, an E2 in-step suspension goes to waiting
// and re-runs the parked step through the step-continue seam. The ordering
// (budget first, then the loop's CAS, then the kick) is what makes a crash
// in the window recoverable: the loop is left waiting/cap-hit, a state
// CheckResume admits, so a resume re-runs the parked step (S31's tolerance
// for a step its row says is suspended). Nothing is undone on a lost race:
// the cap is already up, and taking it back down is not a raise.

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/cjlapao/aeon/internal/budget"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// budgetNum renders a cap or floor number the way the engine's sentence does
// (whole numbers without a trailing .0), so a refusal and a hit that name the
// same number agree byte-for-byte.
func budgetNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// budgetVerdictFor is the answer E1 (the loop engine's pre-dispatch check)
// and E2 (the runtime's in-step check) both read for "may this loop spend":
// the budgets that apply to the loop, their spend as of now, the engine's
// verdict. An empty loop id (a step with no loop) and a storeless daemon
// both answer an OK verdict with nothing to say (V13: no store, no budget) —
// not an error: a loop that does not exist has no cap over it, and a daemon
// with no store has nothing to enforce.
func (d *Daemon) budgetVerdictFor(ctx context.Context, loopID string) (budget.Verdict, error) {
	if d.store == nil || loopID == "" {
		return budget.Verdict{OK: true}, nil
	}
	applicable, err := d.store.ApplicableBudgets(loopID)
	if err != nil {
		return budget.Verdict{}, fmt.Errorf("daemon: the budgets of loop %s: %w", loopID, err)
	}
	if len(applicable) == 0 {
		return budget.Verdict{OK: true}, nil
	}
	return d.evaluateBudgets(applicable)
}

// startBudgetVerdictFor is E3's answer: the budgets that WOULD apply to a
// loop about to start — the epic's (the goal's), the component's and its
// root's, and the workspace's. The loop row does not exist yet, so its own
// loop-scope budget is not among them: a start can only be stopped by a cap
// that already exists over its ground.
func (d *Daemon) startBudgetVerdictFor(componentRoot, epicID string) (budget.Verdict, error) {
	if d.store == nil {
		return budget.Verdict{OK: true}, nil
	}
	applicable, err := d.store.ApplicableBudgetsFor(componentRoot, epicID)
	if err != nil {
		return budget.Verdict{}, fmt.Errorf("daemon: the budgets of the start: %w", err)
	}
	if len(applicable) == 0 {
		return budget.Verdict{OK: true}, nil
	}
	return d.evaluateBudgets(applicable)
}

// evaluateBudgets is the seam's core: read each applicable budget's spend
// from the store, hand the pair to the pure engine, and record the warns the
// engine crossed. It caches nothing (N8): a floor read at one check is a
// stale floor at the next, and spend does not wait.
func (d *Daemon) evaluateBudgets(applicable []store.Budget) (budget.Verdict, error) {
	now := time.Now().UTC()
	pure := make([]budget.Budget, len(applicable))
	spends := make(map[string]budget.Spend, len(applicable))
	for i, b := range applicable {
		sp, err := d.store.SpendFor(b, now)
		if err != nil {
			return budget.Verdict{}, fmt.Errorf("daemon: the spend of budget %s: %w", b.ID, err)
		}
		pure[i] = toBudget(b)
		spends[b.ID] = toBudgetSpend(sp)
	}
	warned, err := d.store.LastBudgetWarned()
	if err != nil {
		return budget.Verdict{}, fmt.Errorf("daemon: the warn record: %w", err)
	}
	verdict := budget.Evaluate(pure, spends, warned, now)
	for _, w := range verdict.Warns {
		if err := d.store.RecordBudgetWarned(w.BudgetID, w.Dimension, w.SpendFloor, w.Cap); err != nil {
			return budget.Verdict{}, fmt.Errorf("daemon: record the budget warn %s/%s: %w", w.BudgetID, w.Dimension, err)
		}
		// S55 T8: the warn is a screen event, not just an audit row — the
		// spend keeps going, and the person the cap is over should see the
		// line cross. The warn is fresh (the record deduped it), so it is
		// emitted exactly once.
		d.publishBudgetEvent(protocol.KindBudgetWarn, d.budgetEventRoot(w.Scope, w.SubjectID, ""), map[string]any{
			"budgetId": w.BudgetID, "scope": w.Scope, "subjectId": w.SubjectID,
			"dimension": w.Dimension, "spendFloor": w.SpendFloor, "cap": w.Cap, "pct": w.Pct,
		})
	}
	return verdict, nil
}

// publishBudgetEvent is the one door the budget plane's events (warn, hit)
// leave the daemon through: the table's event log plus the delivery-time
// notify hint — the hint now resolved by the class resolver inside
// publish itself (S58 T4 deleted the switch and its threading; one
// resolution point, not two).
func (d *Daemon) publishBudgetEvent(kind, componentRoot string, payload map[string]any) {
	d.publish(kind, componentRoot, payload)
}

// budgetEventRoot is the event's componentRoot (the subscription key): the
// spend is checked at the subject, but the SCREEN that subscribes is the
// component's — a component-scope budget is read at the component's root, a
// loop-scope budget at the loop's component, and an epic or workspace
// budget at no component (the workspace screen reads it through its own
// subscription, not a component root).
func (d *Daemon) budgetEventRoot(scope, subjectID, loopID string) string {
	if d.store == nil {
		return ""
	}
	switch scope {
	case "component":
		if r, err := d.store.RootOf(subjectID); err == nil && r != "" {
			return r
		}
		return subjectID
	case "loop":
		id := subjectID
		if _, err := d.store.ReadLoopByID(id); err != nil && loopID != "" {
			id = loopID
		}
		if lp, err := d.store.ReadLoopByID(id); err == nil {
			return lp.ComponentRoot
		}
		return ""
	default: // epic | workspace
		return ""
	}
}

// toBudget maps the store's budget row onto the engine's pure read shape.
// The caps stay pointers: NULL is no cap, zero is a cap (P31's two empties).
func toBudget(b store.Budget) budget.Budget {
	return budget.Budget{
		ID: b.ID, Scope: b.Scope, Subject: b.Subject, Window: b.Window,
		USD: b.USD, Tokens: b.Tokens, WallMs: b.WallMs, Steps: b.Steps,
		UnknownPolicy: b.UnknownPolicy, WarnPct: b.WarnPct,
	}
}

// toBudgetSpend maps the store's spend read onto the engine's. A nil tokens
// total (no priced tokens at all) is a zero floor with the unknown count
// beside it (P31: the count is the honesty; the zero is only the floor).
func toBudgetSpend(sp store.BudgetSpend) budget.Spend {
	var tokens int64
	if sp.Tokens != nil {
		tokens = *sp.Tokens
	}
	return budget.Spend{
		USD: sp.USDAtLeast, USDUnknown: sp.USDUnknown,
		Tokens: tokens, TokensUnknown: sp.TokensUnknown,
		WallMs: sp.WallMs, Steps: sp.Steps,
	}
}

// toRuntimeVerdict maps the engine's verdict onto the runtime's plain-data
// shape (the runtime is pure: it sees a verdict, it never sees the engine).
// The hit the runtime suspends on is Hits[0] — the tightest breach, the one
// whose sentence the step's pause carries.
func toRuntimeVerdict(v budget.Verdict) runtime.BudgetVerdict {
	if v.OK {
		return runtime.BudgetVerdict{OK: true}
	}
	if len(v.Hits) > 0 {
		return runtime.BudgetVerdict{OK: false, Reason: v.Hits[0].Sentence(), CapHit: toRuntimeCapHit(v.Hits[0])}
	}
	// Evaluate sets OK = (no hits), so this is unreachable — named anyway,
	// because a refusal that names no hit is one the step must fail closed on.
	return runtime.BudgetVerdict{OK: false, Reason: "the budget refused the step and named no hit — fail toward stop"}
}

func toRuntimeCapHit(h budget.Hit) *runtime.CapHit {
	return &runtime.CapHit{
		BudgetID: h.BudgetID, Scope: h.Scope, Subject: h.SubjectID,
		Dimension: h.Dimension, Window: h.Window,
		SpendFloor: h.SpendFloor, Cap: h.Cap, Reason: h.Reason, Exact: h.Exact,
	}
}

// recoverBudgetCheck binds the in-step budget seam (E2) to the loop a
// recovered step belongs to: the recovered re-run prices itself against the
// same budgets the live path does, so a crash cannot spend past a cap the
// live path would have refused. Nil when the seam is absent (a storeless
// daemon) — the check is then simply not there, the same absence-is-off the
// control plane's doors follow (V13).
func (d *Daemon) recoverBudgetCheck(loopSlug, componentRoot string) func(context.Context) (runtime.BudgetVerdict, error) {
	if d.Table.Budget == nil || d.Table.LookupLoopID == nil {
		return nil
	}
	return func(ctx context.Context) (runtime.BudgetVerdict, error) {
		return d.Table.Budget(ctx, d.Table.LookupLoopID(loopSlug, componentRoot))
	}
}

// RaiseLoop is the in-process `loop.raise` op (S55 T7): lift the cap a loop
// is parked on, and re-run the work the cap stopped. T8's socket door feeds
// it the person's reason, which rides the raise control event's audit (the
// cap's numbers are the closed keys, LC10; the reason is the person's
// words, 1–512 bytes, checked by the door, not here).
//
// The ORDER is the crash argument: the budget is lifted first (the raised
// event, audited), then the loop's CAS, then the kick. A crash between the
// CAS and the kick leaves the loop waiting/cap-hit — the state CheckResume
// admits, so a resume re-runs the parked step. Nothing is undone on a lost
// race: the cap is already up, and lowering it back is not a raise.
func (d *Daemon) RaiseLoop(ctx context.Context, actor store.Actor, loopID string, newCap float64, reason string) (loop.LoopRaiseResult, *store.ControlRefusal, error) {
	if d.store == nil {
		return loop.LoopRaiseResult{LoopID: loopID}, &store.ControlRefusal{
			Kind: "storeless", Detail: "this daemon has no store — budgets are rows, and there are none to raise"}, nil
	}
	st, err := loop.Pinned(ctx, d.store, loopID)
	if err != nil {
		return loop.LoopRaiseResult{LoopID: loopID}, nil, err
	}
	if ref := loop.CheckRaise(st); ref != nil {
		return loop.LoopRaiseResult{LoopID: loopID}, ref, nil
	}
	if st.CapHit == nil {
		// waiting/cap-hit is the crash window the E2 raise's CAS committed
		// and its kick did not: the cap record the block carried was
		// consumed by that CAS, and the step is still suspended. There is
		// no budget id left on the pin to lift — the remedy is the resume
		// door, which admits exactly this state.
		detail := "the loop is parked on a cap but carries no cap record — its raise already moved it; resume it (loop.resume) to re-run the parked step"
		if !(st.Status == "waiting" && st.LastReason == "cap-hit") {
			detail = "the loop is parked on a cap but carries no cap record — there is nothing to raise"
		}
		return loop.LoopRaiseResult{LoopID: loopID}, &store.ControlRefusal{Kind: "no-cap", Detail: detail}, nil
	}
	if newCap <= st.CapHit.SpendFloor {
		return loop.LoopRaiseResult{LoopID: loopID}, &store.ControlRefusal{
			Kind: "below-floor",
			Detail: fmt.Sprintf("raising %s to %s would not clear the hit (the spend floor is already %s) — raise it above the floor",
				st.CapHit.Dimension, budgetNum(newCap), budgetNum(st.CapHit.SpendFloor))}, nil
	}

	// 1. Lift the cap (the store's RaiseBudget: the cap update and the
	// raised event in one transaction). The actor maps onto the budget
	// audit's closed kinds: a person raises a budget as a person; an agent
	// or a trigger raising on their own is a client act, stamped as one.
	if _, ref, err := d.store.RaiseBudget(store.BudgetActor{Kind: budgetActorKind(actor), ID: actor.ID}, st.CapHit.BudgetID, st.CapHit.Dimension, &newCap); err != nil {
		return loop.LoopRaiseResult{LoopID: loopID, Budget: st.CapHit.BudgetID}, nil, fmt.Errorf("daemon: raise the budget %s: %w", st.CapHit.BudgetID, err)
	} else if ref.Kind != "" {
		return loop.LoopRaiseResult{LoopID: loopID, Budget: st.CapHit.BudgetID}, &store.ControlRefusal{Kind: ref.Kind, Detail: ref.Detail}, nil
	}

	// 2. The hit's shape decides the re-run's shape (the same discriminator
	// ResumeLoop uses for its parked step): the last step's row still
	// suspended is the E2 in-step hit — the step re-runs through the
	// step-continue seam, from its checkpoint. Anything else (the block was
	// pre-dispatch, or there is no step row at all) is E1 — a fresh runner
	// re-dispatches against the raised cap.
	how := "runner"
	if st.LastStepID > 0 && d.Table.Questions != nil {
		status, _, err := d.Table.Questions.StepState(st.LastStepID)
		if err != nil {
			return loop.LoopRaiseResult{LoopID: loopID, Budget: st.CapHit.BudgetID}, nil, fmt.Errorf("daemon: read the step state for the raise of %s: %w", loopID, err)
		}
		if status == "suspended" {
			how = "step"
		}
	}

	// 3. Move the loop (the store's raise CAS: the status move, the pin
	// clear, the raise control event — one transaction; closed-key detail
	// only, LC10 — the reason rides the event's own person column, never
	// the detail).
	detail := map[string]any{
		"budgetId":   st.CapHit.BudgetID,
		"scope":      st.CapHit.Scope,
		"subjectId":  st.CapHit.Subject,
		"dimension":  st.CapHit.Dimension,
		"spendFloor": st.CapHit.SpendFloor,
		"cap":        newCap,
		"window":     st.CapHit.Window,
		"how":        how,
	}
	var parked store.Parked
	var refusal store.ControlRefusal
	if how == "step" {
		parked, refusal, err = d.store.RaiseLoopToWaiting(loopID, actor.Kind, actor.ID, reason, detail)
	} else {
		parked, refusal, err = d.store.RaiseLoopToRunning(loopID, actor.Kind, actor.ID, reason, detail)
	}
	if err != nil {
		return loop.LoopRaiseResult{LoopID: loopID, Budget: st.CapHit.BudgetID}, nil, err
	}
	if refusal.Kind != "" {
		// A lost race: the loop left the cap between the gate and the CAS.
		// The cap is lifted (the raised event says so); the loop did not
		// move, and the refusal names the door its real state has.
		r := refusal
		return loop.LoopRaiseResult{LoopID: loopID, Budget: st.CapHit.BudgetID, Status: parked.Status}, &r, nil
	}

	// 4. The kick, in the shape's order: E2 flips the parked step back to
	// running (the step-continue seam — the only door that turns a cap step
	// around) and the step's own completion hands the loop back; E1 starts
	// the fresh runner, whose first dispatch re-evaluates the budget
	// against the raised cap.
	if how == "step" {
		if err := d.Table.ContinueCapStep(st.LastStepID); err != nil {
			return loop.LoopRaiseResult{LoopID: loopID, Budget: st.CapHit.BudgetID, Raised: true, Status: parked.Status, How: how,
				Note: "the cap is raised and the loop is waiting, but the parked step refused the re-run — resume it to try again"}, nil, err
		}
	} else if d.loopRunner == nil {
		return loop.LoopRaiseResult{LoopID: loopID, Budget: st.CapHit.BudgetID}, nil, fmt.Errorf("daemon: raise %s: the loop runner is not up", loopID)
	} else if !d.loopRunner.Start(d.loopCtx, loopID) {
		// Start refused: an entry for the loop is still in the runner's
		// active map. A LIVE runner is impossible here — a live runner
		// would mean the loop was running when this raise's CAS committed
		// (blocked or waiting into running), and the CAS would have been
		// refused instead. The entry is the parked loop's own runner in its
		// exit path: the block is emitted BEFORE the goroutine leaves the
		// map, and a blocked loop holds no goroutine as an invariant.
		// Acting on the old note — "that runner carries the re-dispatch" —
		// is the stall: the departing runner re-dispatches nothing, and the
		// loop sits running forever with nobody behind it. Let the old
		// runner settle, then start the fresh one.
		if !d.loopRunner.Wait(loopID, 5*time.Second) {
			return loop.LoopRaiseResult{LoopID: loopID, Budget: st.CapHit.BudgetID, Raised: true, Status: parked.Status, How: how}, nil,
				fmt.Errorf("daemon: raise %s: the parked loop's runner did not settle and no fresh runner started — the cap is raised and the loop is running in the store; a resume carries it", loopID)
		}
		if !d.loopRunner.Start(d.loopCtx, loopID) {
			// Wait reported the map empty (or the entry settled) yet Start
			// refused anyway: the only remaining cause is a draining
			// daemon, which refuses every new runner. The cap is raised and
			// the loop is running in the store; the process is going away
			// and cold recovery on the next start carries the loop.
			return loop.LoopRaiseResult{LoopID: loopID, Budget: st.CapHit.BudgetID, Raised: true, Status: parked.Status, How: how}, nil,
				fmt.Errorf("daemon: raise %s: the cap is raised and the loop is running in the store, but the daemon is shutting down and the fresh runner was refused — the next start recovers the loop", loopID)
		}
	}
	note := fmt.Sprintf("raised the %s cap of %s to %s", st.CapHit.Dimension, st.CapHit.BudgetID, budgetNum(newCap))
	hint := "a fresh runner re-dispatches against the raised cap"
	if how == "step" {
		hint = "the parked step re-runs from its checkpoint; the loop resumes when it settles"
	}
	return loop.LoopRaiseResult{
		LoopID: loopID, Budget: st.CapHit.BudgetID,
		Raised: true, Status: parked.Status, How: how,
		Note: note, HowHint: hint,
	}, nil, nil
}

// budgetActorKind maps the loop-control actor onto the budget audit's closed
// kinds (user|cli|system): a person raises a budget as a person; an agent or
// a trigger raising on their own is a client act, stamped as one. `system`
// is the engine's own (a hit, a warn) — never a caller's.
func budgetActorKind(a store.Actor) string {
	if a.Kind == "user" {
		return "user"
	}
	return "cli"
}
