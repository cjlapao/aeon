package history

import (
	"fmt"
	"sort"

	"github.com/cjlapao/aeon/internal/store"
)

// GroupBy names one of the four lane groupings (H1). All four are a plain
// GROUP BY over columns this slice already reads — no new join shape, only a
// different column the GROUP BY keys on (H1). "loop" is the default and is
// the merge itself; the other three regroup the live steps.
type GroupBy string

const (
	// ByLoop groups by loop — the default, the merge (one entry per loop,
	// time-ordered).
	ByLoop GroupBy = "loop"
	// ByPhase groups by step.phase.
	ByPhase GroupBy = "phase"
	// ByTask groups by step.taskid.
	ByTask GroupBy = "task"
	// ByAgent groups by step.agent.
	ByAgent GroupBy = "agent"
)

// Valid reports whether a group-by is one of the four named in H1. A value
// nobody compiled is a grouping nobody granted, so an unknown one is a
// refusal by name (DB19), not a silent default.
func (g GroupBy) Valid() bool {
	switch g {
	case "", ByLoop, ByPhase, ByTask, ByAgent:
		return true
	}
	return false
}

// Lane is one group in a lane view: the grouping key, the loops that fall in
// it, and the floored cost (H6) of its steps. For the by-loop lane a lane is
// a single loop; for the other three a lane's steps may span loops, which is
// the whole point of a grouping "by phase" — one phase across the project's
// loops, not one loop's view of a phase.
type Lane struct {
	// Key is the grouping value: a loop slug, a phase, a taskid or an agent.
	Key string
	// Loops is the set of loops that fall in this group. For the by-loop lane
	// it is the one loop; for the others it is every loop with a step in the
	// group, time-ordered (the merge's order) so a viewer can see which loops
	// a phase spanned.
	Loops []Entry
	// Cost is the floored rollup of this group's steps (H6).
	Cost Rollup
}

// Lanes returns the projection grouped one of four ways (H1).
//
// By loop, it is the merge itself, one lane per entry — so the default view
// and the "group by loop" view are the same rows, and a viewer switching
// groupings never sees the loop's facts change, only its grouping. By phase,
// task or agent, it regroups the live steps of the loops in scope: an
// archived loop's steps are gone from the live tables (archived = deleted),
// so an archived loop is in the by-loop lane but in no step grouping — the
// honest shape, since the list does not decode the cold-stored state (Q3).
func Lanes(s *store.Store, opts Options, by GroupBy) ([]Lane, error) {
	if !by.Valid() {
		return nil, errBadGroupBy(by)
	}
	if by == "" || by == ByLoop {
		entries, err := Merge(s, opts)
		if err != nil {
			return nil, err
		}
		lanes := make([]Lane, 0, len(entries))
		for _, e := range entries {
			lanes = append(lanes, Lane{
				Key:   e.Slug,
				Loops: []Entry{e},
				Cost:  e.Cost,
			})
		}
		return lanes, nil
	}

	// The other three group the live steps. The --since filter is on the
	// LOOP's timeline (the merge's axis), so it narrows which loops are in
	// scope, then their steps are grouped.
	loops, err := liveLoops(s, opts)
	if err != nil {
		return nil, err
	}

	// One accumulator per group, keyed by the grouping value. The cost is the
	// floored rollup of the group's steps; the loops set is which loops
	// contributed a step to the group.
	costBy := map[string]*Rollup{}
	loopsBy := map[string][]Entry{}
	for _, l := range loops {
		steps, err := s.ListSteps(store.Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot})
		if err != nil {
			return nil, err
		}
		keyOf := keyFor(by)
		e := Entry{
			Kind:      KindLive,
			ID:        l.ID,
			Slug:      l.Slug,
			Title:     l.Title,
			Goal:      l.Goal,
			Status:    l.Status,
			StartedAt: l.StartedAt,
			Timeline:  l.UpdatedAt,
		}
		seenInGroup := map[string]bool{}
		for _, step := range steps {
			key := keyOf(step)
			if key == "" {
				// A step with no value in the grouping column (an unassigned
				// taskid, e.g.) does not belong to a group by that column;
				// it is not dropped silently, it is simply not in this
				// grouping — the by-loop lane still shows its loop.
				continue
			}
			r, ok := costBy[key]
			if !ok {
				r = &Rollup{}
				costBy[key] = r
			}
			r.addStep(step.Cost)
			if !seenInGroup[key] {
				loopsBy[key] = append(loopsBy[key], e)
				seenInGroup[key] = true
			}
		}
	}

	// Order the lanes by key, so a re-run of the same fixture is identical
	// (a map is not ordered, and a view that reorders on every render is a
	// view a person cannot trust).
	lanes := buildLanes(costBy, loopsBy)
	return lanes, nil
}

// liveLoops reads the live loops in scope, narrowed by --since on the
// timeline — the same axis the merge uses, so the lanes never disagree with
// the merge about which loops a date range means.
func liveLoops(s *store.Store, opts Options) ([]store.Loop, error) {
	loops, err := s.ListLoops(opts.Scope)
	if err != nil {
		return nil, err
	}
	if opts.Since == "" {
		return loops, nil
	}
	since, err := parseSince(opts.Since)
	if err != nil {
		return nil, err
	}
	kept := loops[:0]
	for _, l := range loops {
		if at, ok := parseTime(l.UpdatedAt); ok && !at.Before(since) {
			kept = append(kept, l)
		}
	}
	return kept, nil
}

// keyFor returns the grouping column's value for a step. An empty value means
// the step has no value in that column and belongs to no group by it (it is
// not dropped — the by-loop lane still shows its loop).
func keyFor(by GroupBy) func(store.Step) string {
	switch by {
	case ByPhase:
		return func(s store.Step) string { return s.Phase }
	case ByTask:
		return func(s store.Step) string { return s.TaskID }
	case ByAgent:
		return func(s store.Step) string { return s.Agent }
	default:
		return func(store.Step) string { return "" }
	}
}

// buildLanes turns the per-group accumulators into ordered lanes.
func buildLanes(costBy map[string]*Rollup, loopsBy map[string][]Entry) []Lane {
	keys := make([]string, 0, len(costBy))
	for k := range costBy {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lanes := make([]Lane, 0, len(keys))
	for _, k := range keys {
		c := costBy[k]
		if c == nil {
			// A group with a key but no costed steps (every step unknown, or
			// none) still renders — its cost is the rollup it accumulated.
			c = &Rollup{}
		}
		l := Lane{
			Key:   k,
			Loops: loopsBy[k],
			Cost:  *c,
		}
		if l.Loops == nil {
			l.Loops = []Entry{}
		}
		lanes = append(lanes, l)
	}
	return lanes
}

// errBadGroupBy names the value it refused, the way a second formatter would
// be a second truth: an unknown grouping is refused by name, not defaulted.
func errBadGroupBy(g GroupBy) error {
	return &badGroupBy{GroupBy: g}
}

// badGroupBy is a refused grouping that names itself (DB19): a value nobody
// compiled is a grouping nobody granted, and the message carries the value
// that was refused so a caller cannot mistake it for a default.
type badGroupBy struct{ GroupBy GroupBy }

func (e *badGroupBy) Error() string {
	return fmt.Sprintf("history: unknown grouping %q — the four are %s, %s, %s and %s",
		e.GroupBy, ByLoop, ByPhase, ByTask, ByAgent)
}
