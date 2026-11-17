package loop

import (
	"context"

	"github.com/cjlapao/aeon/internal/store"
)

// seed_tasks.go — an iterate lane runs over the loop's tasks, and the loop's task list has to come from somewhere.
//
// The planner files its tasks into the store as task rows. The runtime state carries its own list of task ids (the thing an
// iterate lane walks), and it was only ever filled by the parallel path. A loop run one agent at a time (a component's cap of
// 1, the default) went straight from the plan to a build lane that saw no tasks and skipped it as "not needed", and then
// skipped everything after it. The store is the source of truth for what the plan produced, so the serial path reads it too.

// seedTasksFromStore fills the runtime task list from the loop's task rows when the lane about to run iterates over tasks and
// the list is still empty. Finished work (complete, merged, failed, skipped, superseded) is not queued again. It changes only
// the in-memory state; the action that follows saves it with everything else.
func seedTasksFromStore(ctx context.Context, dep Deps, st *LoopState) {
	_ = ctx
	if dep.Store == nil || st.Blueprint == nil || len(st.Tasks) > 0 {
		return
	}
	iterating := false
	for _, l := range st.Blueprint.Lanes {
		if l.ID == st.Position.Lane && len(l.Ladder) > 0 {
			iterating = true
			break
		}
	}
	if !iterating {
		return
	}
	rows, err := dep.Store.ListTasks(store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot})
	if err != nil {
		return
	}
	for _, t := range rows {
		switch store.TaskStatus(t.Status) {
		case store.StatusComplete, store.StatusMerged, store.StatusFailed, store.StatusSkipped, store.StatusSuperseded:
			continue
		}
		st.Tasks = append(st.Tasks, t.TaskID)
	}
}
