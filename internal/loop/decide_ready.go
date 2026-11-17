package loop

import (
	"sort"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// DecideReady is S32's PA8: the sibling of Decide that returns a SET of ready
// actions instead of one. At cap == 1 it returns exactly Decide's own result,
// implemented by CALLING Decide — never a reimplementation that happens to
// agree — because D8's byte-identity requirement is a test, and the surest way
// to pass it is to have no second code path for the serial case at all.
//
// At cap > 1 it evaluates the lanes for a declared-independent set (PA4
// Shape A — a parallel_group whose members are all reachable) and returns
// them together, bounded by the cap. The cap is a ceiling, not a target (PA2):
// the engine returns every ready unit up to the cap, and never invents work
// the blueprint did not make independent.
func DecideReady(bp *blueprint.Blueprint, st LoopState, cap int) []Action {
	if cap <= 1 {
		return []Action{Decide(bp, st)}
	}
	ready := readySet(bp, st, cap)
	if len(ready) == 0 {
		// No group is reachable right now: run the single decision the serial
		// path makes, so a cap > 1 loop never decides to nothing. It advances
		// serially until a parallel_group becomes reachable, and byte-identically
		// at cap 1 (which never reaches here).
		return []Action{Decide(bp, st)}
	}
	return ready
}

// readySet is the cap > 1 evaluation (PA8/PA4). It combines the two shapes the
// design declares independent, bounded together by the cap:
//
//	Shape A — readyGroup: the current position's parallel_group, whose members
//	  are all reachable (every member's When guard passes) at once.
//	Shape B — readyTasks: an iterate lane with Parallel: true, whose ready
//	  items come from task.depends_on plus status, not "the next in ord".
//
// Each unit is decided by running Decide on a copy of the state whose position
// is that unit — the SAME code the serial path uses, so a parallel unit and the
// serial unit of the same lane cannot disagree. Shape A is tried first (the
// declared set the position sits in); Shape B fills the remaining budget, so a
// lane that is both a group member and an iterate lane cannot double-launch the
// same item.
func readySet(bp *blueprint.Blueprint, st LoopState, cap int) []Action {
	if bp == nil {
		return nil
	}
	var out []Action
	out = append(out, readyGroup(bp, st, cap)...)
	if len(out) < cap {
		out = append(out, readyTasks(bp, st, cap-len(out))...)
	}
	return out
}

// readyGroup is PA4 Shape A: the ready lanes of the parallel_group the current
// position belongs to. A lane is ready when its When guard passes (an absent
// guard passes, the usual convention) and Decide on that lane's position is not
// already terminal.
func readyGroup(bp *blueprint.Blueprint, st LoopState, cap int) []Action {
	group := ""
	for _, l := range bp.Lanes {
		if l.ID == st.Position.Lane {
			group = l.ParallelGroup
			break
		}
	}
	if group == "" {
		return nil
	}
	var out []Action
	seen := map[string]bool{}
	for _, l := range bp.Lanes {
		if l.ParallelGroup != group || seen[l.ID] {
			continue
		}
		seen[l.ID] = true
		if l.When != nil {
			ok, err := l.When.Evaluate(st.Values())
			if err != nil || !ok {
				continue
			}
		}
		unitSt := st
		unitSt.Position = Position{Lane: l.ID}
		a := Decide(bp, unitSt)
		switch a.Kind {
		case ActDone, ActIdle, ActWaiting, ActBlocked, ActSkip:
			continue
		}
		out = append(out, a)
		if len(out) >= cap {
			break
		}
	}
	return out
}

// readyTasks is PA4 Shape B: for an iterate lane declared Parallel, readiness
// comes from task.depends_on plus status, not "the next in ord". A task is
// ready when its own status is pending and every task it depends on has
// reached a successful terminal (complete/merged/skipped/superseded). One
// ActRunModelStep-shaped action is returned per ready task, bounded by the
// remaining budget. The readiness walk is escalate.go's transitive depends_on
// shape (the same code the serial path already relies on); the successful
// terminal set is the one the design names, not a guess.
func readyTasks(bp *blueprint.Blueprint, st LoopState, budget int) []Action {
	if budget <= 0 || st.TaskRows == nil {
		return nil
	}
	var out []Action
	for _, l := range bp.Lanes {
		if l.Iterate == "" || !l.Parallel {
			continue
		}
		// The store's task rows are the source of truth (the pinned st.Tasks can
		// lag a just-created row); sorted so the ready order is deterministic
		// and the cap bounds a stable prefix, not a random map walk.
		ids := sortedTaskIDs(st.TaskRows)
		for _, item := range ids {
			if len(out) >= budget {
				return out
			}
			row, ok := st.TaskRows[item]
			if !ok {
				continue
			}
			if store.TaskStatus(row.Status) != store.StatusPending {
				continue
			}
			if !allDepsSettled(st.TaskRows, row.DependsOn) {
				continue
			}
			unitSt := st
			unitSt.Position = Position{Lane: l.ID, Item: item}
			a := Decide(bp, unitSt)
			switch a.Kind {
			case ActDone, ActIdle, ActWaiting, ActBlocked, ActSkip:
				continue
			}
			out = append(out, a)
		}
	}
	return out
}

// sortedTaskIDs returns the task ids in ascending order — a deterministic
// ready order so the cap bounds a stable prefix (a map walk would let the
// bounded set depend on iteration order, which a person reading "which units
// launched" cannot predict).
func sortedTaskIDs(rows map[string]store.Task) []string {
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// allDepsSettled is PA4 Shape B's gate: every dependency must be in a
// successful terminal, and a dependency no row is known for is treated as not
// settled (a missing row is never "done" — an unknown is refused, never a
// pass). An empty depends_on is settled: nothing to wait on.
func allDepsSettled(rows map[string]store.Task, deps []string) bool {
	for _, d := range deps {
		row, ok := rows[d]
		if !ok || !successfulTerminal(row.Status) {
			return false
		}
	}
	return true
}

// successfulTerminal is the closed set of task statuses the design names as
// "done enough to unblock a dependent": complete, merged, skipped,
// superseded. A failed or blocked dependency is NOT a successful terminal —
// it blocks its dependents (escalate.go's fail-item / block answers), it does
// not release them, so a pending dependent of a failed task stays not-ready.
func successfulTerminal(status string) bool {
	switch store.TaskStatus(status) {
	case store.StatusComplete, store.StatusMerged, store.StatusSkipped, store.StatusSuperseded:
		return true
	}
	return false
}
