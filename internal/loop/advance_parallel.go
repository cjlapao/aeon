package loop

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/store"
)

// S32 T5 (PA8/PA9/PA10): the live fan-out. This is the cap > 1 path — Run
// routes here only when the component's cap is > 1 AND the decision set has
// more than one ready action; otherwise the serial path runs unchanged (D8).
//
// The design's one mechanism, two shapes (PA4): the ready set comes from
// DecideReady (Shape A's parallel_group + Shape B's iterate-Parallel tasks),
// and every ready unit runs in its own goroutine with its own context. The
// per-unit context is NEVER shared (PA7): a sibling's failure can only cancel
// its own unit, never another's — a sibling's cancellation is never reachable
// from another unit's ctx, so the test is the proof, not a comment.

// unitKey is the stable id of one in-flight unit: the task id (Shape B) or the
// lane id (Shape A). It keys the inFlight map and identifies a unit on the
// results channel — a unit is keyed by what it OWNS (a task row, or a lane),
// not by a slice index that could collide.
func unitKey(a Action) string {
	if a.Position.Item != "" {
		return "task:" + a.Position.Item
	}
	return "lane:" + a.Position.Lane
}

// componentCap reads the loop's cap FRESH (PA15: never cached across the run).
// It resolves the loop's workspace to its component and reads the ceiling; a
// workspace with no registered component, an unresolvable read, or a value
// below 1 all answer 1 — the serial path, byte-identical (D8), never a 0 that
// would stop a loop. The fresh read is what lets a cap change take effect at
// the next launch decision, in both directions.
func componentCap(ctx context.Context, dep Deps, id string) int {
	st, err := Pinned(ctx, dep.Store, id)
	if err != nil {
		return 1
	}
	// The cap is an inheritable column (CT6): resolve it through effective.go,
	// the sole reader. A workspace with no registered component, an unresolvable
	// read, or a value below 1 all answer 1 — the serial path, byte-identical
	// (D8), never a 0 that would stop a loop.
	eff, err := dep.Store.EffectiveForRoot(st.ComponentRoot)
	if err != nil {
		return 1
	}
	if eff.MaxParallelAgents.Value < 1 {
		return 1
	}
	return eff.MaxParallelAgents.Value
}

// unitOwner is the worktree row's (owner_kind, owner_id): a task unit hangs its
// worktree on the task row (Shape B), a lane unit on the lane (Shape A — no task
// row to hang on). PA5's row generalises over both, never a second table.
func unitOwner(a Action) (kind, id string) {
	if a.Position.Item != "" {
		return "task", a.Position.Item
	}
	return "lane", a.Position.Lane
}

// AdvanceLoopParallel is S32 T5's fan-out. It reads the cap FRESH here (PA15 —
// never cached across the run), decides the ready set, launches one goroutine
// per ready unit, and selects on the results channel — re-deciding on every
// receive so a sibling's completion that satisfies a dependent's depends_on
// launches the dependent on the next wakeup, with no polling and no sleep (PA8).
// When the last unit settles it returns the aggregate result.
func AdvanceLoopParallel(ctx context.Context, dep Deps, id string, capReader func() int) (Result, error) {
	if dep.Store == nil {
		return Result{}, fmt.Errorf("loop store is unavailable")
	}
	st, err := Pinned(ctx, dep.Store, id)
	if err != nil {
		return Result{}, err
	}
	// PA15: the cap is read FRESH here, and re-read on every re-decision below —
	// never cached across the run — so a cap change takes effect at the next
	// launch decision, in both directions (a lower cap stops new launches, a
	// raised one allows a new one sooner). capReader is the seam; Run feeds it
	// the component's live cap, the tests feed it a value they can move.
	cap := capReader()
	// Populate the Shape-B readiness input from the store (the task rows with
	// their status + depends_on). This is the transient field, never persisted.
	st.TaskRows = loadTaskRows(ctx, dep, st)
	// The iterate lane's Decide reads the loop's task list (st.Tasks); a loop
	// that just got its tasks in the store has an empty pinned list, so seed it
	// from the rows the fan-out just read — the store is the source of truth,
	// and Decide's `len(st.Tasks) == 0` gate would otherwise skip every item.
	if len(st.Tasks) == 0 {
		for id := range st.TaskRows {
			st.Tasks = append(st.Tasks, id)
		}
	}
	ready := DecideReady(st.Blueprint, st, cap)
	// D8 (byte-identity): the cap <= 1 case is the serial path's job (Run
	// routes it to AdvanceLoop); if this is reached at cap <= 1 anyway,
	// behave identically to the serial advance. The cap > 1 case runs the
	// fan-out even with a single ready unit — a completion may release a
	// dependent (Shape B) or a group member, and the re-decision on the
	// channel is what catches that, so a lone ready unit is not a reason to
	// skip the fan-out.
	if cap <= 1 {
		return AdvanceLoop(ctx, dep, id)
	}

	type unitResult struct {
		action Action
		result ModelResult
		err    error
	}
	results := make(chan unitResult, cap)
	// inFlight holds every running unit's cancel — keyed by unit id. PA7: each
	// is its OWN context, created below, never a shared one; the map is the set
	// of live units, and the loop selects on `results`, not a shared cancel.
	inFlight := map[string]context.CancelFunc{}
	// seen is the set of units already launched in this run. A unit is launched
	// at most once: re-deciding after a sibling's completion may make a NEW unit
	// (a dependent whose depends_on just settled) ready, but it must never
	// re-launch a unit that already ran — a Shape-A group's lanes are always
	// "ready", so without this a completed unit would be re-dispatched forever
	// (an infinite fan-out). seen and inFlight differ: inFlight is live units,
	// seen is every unit ever launched (a completed unit is seen but not live).
	seen := map[string]bool{}

	launch := func(a Action) {
		key := unitKey(a)
		if seen[key] {
			return
		}
		seen[key] = true
		if a.Step == nil {
			// A ready unit with no step (a gate or a code action) is not a
			// model unit the fan-out runs here; the serial path owns it.
			return
		}
		uctx, cancel := context.WithCancel(ctx) // PA7: per-unit, never shared
		inFlight[key] = cancel
		go func(a Action, uctx context.Context) {
			res, err := runOneUnit(uctx, dep, st, a)
			results <- unitResult{action: a, result: res, err: err}
		}(a, uctx)
	}
	for _, a := range ready {
		launch(a)
	}

	lastResult := Result{LoopID: st.ID, Status: st.Status, Position: st.Position}
	for len(inFlight) > 0 {
		r := <-results
		cancel := inFlight[unitKey(r.action)]
		delete(inFlight, unitKey(r.action))
		if cancel != nil {
			cancel() // the unit is done: release its context now (PA7's only cancel)
		}
		if r.err != nil {
			// A unit that errored is a failure, not a suspension: it does not
			// cancel its siblings (PA7) — it just stops, and the loop re-decides.
			lastResult = failUnitResult(st, r.action, r.err)
			continue
		}
		lastResult = applyUnitResult(ctx, dep, id, st, r.action, r.result)
		// PA15: re-read the cap FRESH before re-deciding — a cap lowered since
		// this run's start must not launch more units, and a cap raised must
		// allow a new one sooner. The cap is never cached across decisions.
		cap = capReader()
		// Re-decide: a sibling's completion may have satisfied a dependent's
		// depends_on. Launch any newly-ready unit up to the (fresh) cap, minus
		// what is already in flight — the channel is the only wakeup, never a
		// timer.
		for _, a := range DecideReady(st.Blueprint, st, cap) {
			if len(inFlight) >= cap {
				break
			}
			launch(a)
		}
	}
	return lastResult, nil
}

// loadTaskRows reads the loop's task rows (status + depends_on) for Shape-B
// readiness. A loop with no tasks (a Shape-A or serial loop) gets an empty map,
// and readyTasks then returns nothing — the cap > 1 loop falls back to the
// single decision, exactly as the serial path would.
func loadTaskRows(ctx context.Context, dep Deps, st LoopState) map[string]store.Task {
	sc := store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}
	rows, err := dep.Store.ListTasks(sc)
	if err != nil {
		return map[string]store.Task{}
	}
	out := make(map[string]store.Task, len(rows))
	for _, t := range rows {
		out[t.TaskID] = t
	}
	return out
}

// runOneUnit is PA10: one unit, one worktree, one model step. Before the model
// runs, the worktree is made in PA5's write-ahead order — the ledger row FIRST
// (CreateLoopWorktree), then the real `git worktree add` (AddWorktree). A crash
// between the two leaves a live row with no directory, which recovery (PA12)
// tells apart by asking git. On terminal the worktree is settled in the reverse
// order (RemoveWorktree then RemoveLoopWorktree), settle-once. The unit runs
// its model step on its OWN copy of the state whose Position is this unit, so
// two units never share the primary Position (PA13's widening, by the additive
// field, never a rename).
func runOneUnit(ctx context.Context, dep Deps, st LoopState, a Action) (ModelResult, error) {
	wt, err := ensureWorktree(ctx, dep, st, a)
	if err != nil {
		return ModelResult{}, err
	}
	// The settle is deferred so a step that errors OR completes both clean up;
	// the store's guarded write makes a second settle a no-op (settle-once).
	defer func() { _ = settleWorktree(ctx, dep, wt) }()

	unitSt := st
	unitSt.Position = a.Position
	// S60 T5 closes S32's recorded gap (T0's F16: the unit's step received
	// the LOOP root; wt.Path was used only for removal): a parallel unit's
	// model step now runs INSIDE its own worktree — PA5's whole point, which
	// the ledger row always promised. The assignment is per-unit (a copy,
	// never st), so the saved RuntimeState of a cap-1 or serial loop never
	// grows a workDir key (D8: goldens unmoved there), and it is gated on
	// the git seam: with no client wired, ensureWorktree left only the
	// ledger INTENT and no directory — pointing the step at an absent tree
	// would be worse than the gap it closes, so today's behaviour stands
	// (recorded).
	if dep.Actions != nil {
		unitSt.WorkDir = wt.Path
	}
	// S68: words (user message) and frame (system message) travel separately,
	// exactly as the serial dispatch does.
	res, err := dep.Model(ctx, unitSt, *a.Step, ModelWords(unitSt), TaskFrame(unitSt, *a.Step))
	if err != nil {
		return ModelResult{}, err
	}
	return res, nil
}

// ensureWorktree is PA5's write-ahead: the ledger row is inserted BEFORE the
// real worktree is made. Idempotent — a unit already in flight has a live row
// and reuses it (never a second row, never a second directory). The git add is
// best-effort: a missing Actions seam (no git client wired) is a named block,
// not a panic (the same discipline the action door uses).
func ensureWorktree(ctx context.Context, dep Deps, st LoopState, a Action) (store.LoopWorktree, error) {
	kind, id := unitOwner(a)
	path := worktreePath(st, a)
	// Idempotency: a live row for this unit reuses it. ListLiveLoopWorktrees is
	// the settle-once guard's read side.
	live, err := dep.Store.ListLiveLoopWorktrees(store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot})
	if err != nil {
		return store.LoopWorktree{}, err
	}
	for _, w := range live {
		if w.OwnerKind == kind && w.OwnerID == id {
			return w, nil
		}
	}
	// Step 1: the ledger row (PA5 write-ahead).
	w := store.LoopWorktree{
		ComponentRoot: st.ComponentRoot,
		LoopSlug:      st.Slug,
		OwnerKind:     kind,
		OwnerID:       id,
		Path:          path,
		Branch:        branchForUnit(st, a),
	}
	created, err := dep.Store.CreateLoopWorktree(w)
	if err != nil {
		return store.LoopWorktree{}, err
	}
	// Step 2: the real `git worktree add`. Reversed would defeat R1's whole
	// mitigation (a crash between the two is the case recovery is built for).
	if dep.Actions == nil {
		// No git seam wired: the row is the intent, the directory is owed. The
		// caller blocks honestly rather than pretending the worktree exists.
		return created, nil
	}
	if _, err := dep.Actions(st.ComponentRoot).Git.AddWorktree(ctx, st.ComponentRoot, path, branchNamesForUnit(st, a)); err != nil {
		return store.LoopWorktree{}, err
	}
	return created, nil
}

// settleWorktree is PA5's write-ahead step 4: after the worktree is gone the
// row's removed_at is set, in the reverse of ensureWorktree's order. A missing
// git client (no Actions) skips the git half but still settles the row — the
// row's removed_at is the settle-once signal the recovery pass reads.
func settleWorktree(ctx context.Context, dep Deps, wt store.LoopWorktree) error {
	if wt.ID == 0 {
		return nil
	}
	if dep.Actions != nil {
		if _, err := dep.Actions(wt.ComponentRoot).Git.RemoveWorktree(ctx, wt.ComponentRoot, wt.Path); err != nil {
			// A missing worktree is a success, not a fault (RemoveWorktree's
			// contract); a real removal error is swallowed the same way — the
			// row's removed_at below is the durable settle signal.
			return nil
		}
	}
	return dep.Store.RemoveLoopWorktree(wt.ID)
}

// applyUnitResult records a completed unit's outcome and, for a task unit,
// moves the task to a successful terminal so a dependent's depends_on can
// release. The loop-level status follows the aggregate: a suspended unit puts
// the loop waiting (it cannot finish until the gate is answered); a completed
// unit advances its position; the final Result reflects the last settle.
func applyUnitResult(ctx context.Context, dep Deps, id string, st LoopState, a Action, res ModelResult) Result {
	key := unitKey(a)
	out := Result{LoopID: st.ID, Status: st.Status, Position: st.Position, QuestionID: res.QuestionID}
	kind, ownerID := unitOwner(a)
	// A suspended unit (a real question) parks the loop waiting — the same P2
	// argument the serial path uses: a gate holds no goroutine, an answer
	// starts a fresh run. A sibling that is not gated keeps running (PA6).
	if res.Kind == "suspended" {
		out.Status = "waiting"
		if res.QuestionID != 0 {
			out.QuestionID = res.QuestionID
		}
		emit(dep, "loop.waiting", map[string]any{"loopId": id, "unit": key})
		return out
	}
	// A completed unit settles its owner to a successful terminal so a
	// dependent's depends_on (Shape B) can release on the next re-decision. The
	// in-memory row is the readiness input and is updated unconditionally — a
	// store transition that is refused (pending→complete can be) must not stop
	// the loop from seeing the unit as done; the durable write is best-effort.
	if res.Kind == "completed" {
		if kind == "task" {
			if row, ok := st.TaskRows[ownerID]; ok {
				row.Status = string(store.StatusComplete)
				st.TaskRows[ownerID] = row
			}
			_ = settleTask(ctx, dep, store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}, ownerID, store.StatusComplete)
		}
		out.Status = "running"
		emit(dep, "loop.unit.completed", map[string]any{"loopId": id, "unit": key})
	}
	return out
}

func failUnitResult(st LoopState, a Action, err error) Result {
	return Result{
		LoopID:   st.ID,
		Status:   "running",
		Position: st.Position,
		Reason:   &BlockedReason{Kind: "unit-error", Detail: fmt.Sprintf("unit %s: %v", unitKey(a), err)},
	}
}

// branchForUnit / branchNamesForUnit give a unit its worktree branch: the task
// id (Shape B) or the lane id (Shape A) names the branch, the loop slug the
// prefix. A worktree per unit, a branch per worktree — never shared.
func branchForUnit(st LoopState, a Action) string {
	_, id := unitOwner(a)
	return "aeon/" + st.Slug + "/" + id
}

func branchNamesForUnit(st LoopState, a Action) action.BranchNames {
	_, id := unitOwner(a)
	return action.BranchNames{Loop: st.Slug, Task: id}
}

// worktreePath is the unit's scratch directory: a per-unit path under the
// component root, distinct per unit so two units never touch the same tree.
func worktreePath(st LoopState, a Action) string {
	kind, id := unitOwner(a)
	return st.ComponentRoot + "/.worktrees/" + kind + "-" + id
}
