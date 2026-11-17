package loop

// Two-level counters (L28) — the one piece of arithmetic in S17, isolated
// from the routing that consumes it (T3–T4) because **a per-loop-only
// implementation passes every other test in the slice**: the question is
// "is review_rounds per-task or per-loop?" and v1's answer, inherited here
// rather than invented, is BOTH, at different levels.
//
// Per-loop only → task three inherits tasks one and two's exhausted budget
// and cannot be reviewed at all: a loop escalating for no visible reason on
// a task nobody reviewed once. Per-task only → a twenty-task loop spends
// forty review rounds and calls it progress. The loop ceiling (Q11) is what
// stops the second; per-task budgets are what refuse the first.
//
// THE LEVEL IS A PROPERTY OF THE COUNTER (I2), never of the call site:
// there is one Bump, level comes from the name, and no
// IncrementTask/IncrementLoop pair exists for a caller to choose between. A
// counter whose level depends on who increments it WILL be incremented at
// both, and the bug appears as a budget that drains twice as fast on some
// tasks and not others.

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

// Level is which row a counter's budget lives on.
type Level string

const (
	LevelTask Level = "task"
	LevelLoop Level = "loop"
)

// LoopCeiling is the loop-wide bound on failed gates (Q11: its value — 10).
// Twenty tasks each rejecting once is twenty rejections, none of them
// individually capped; without a ceiling that is "a loop that runs all day
// producing send-backs". Reaching it is not an escalation (D3): a human
// answering a question would not fix a loop-wide non-convergence.
const LoopCeiling = 10

// LoopCeilingOutcome is the ceiling's decision as DATA, so the engine (T4)
// consumes one definition and cannot drift from it: reaching the ceiling
// BLOCKS. "escalate" here would be a comment pretending to be a table.
const LoopCeilingOutcome = "block"

// Counter is one entry of the closed table. Cap 0 means uncapped.
type Counter struct {
	Name  string
	Level Level
	Cap   int
}

// counters is the closed set: three task counters at cap 2 each (v1's
// protocol bounds, published by the store at Caps() — TestCapsMatchStoreMachine
// pins the two publications to one truth), and three loop-wide counters
// where total_failed_gates carries the ceiling and qa_rounds v1's bound,
// while splits_done is a ledger count with no cap.
var counters = map[string]Counter{
	"retries":            {Name: "retries", Level: LevelTask, Cap: 2},
	"security_rounds":    {Name: "security_rounds", Level: LevelTask, Cap: 2},
	"review_rounds":      {Name: "review_rounds", Level: LevelTask, Cap: 2},
	"total_failed_gates": {Name: "total_failed_gates", Level: LevelLoop, Cap: LoopCeiling},
	"splits_done":        {Name: "splits_done", Level: LevelLoop, Cap: 0},
	"qa_rounds":          {Name: "qa_rounds", Level: LevelLoop, Cap: 2},
}

// Lookup answers the counter's level and cap BY NAME — the only way a level
// is ever learned (I2).
func Lookup(name string) (Counter, bool) {
	c, ok := counters[name]
	return c, ok
}

// Table publishes the closed set as data (the CLI, the app panel and any
// future surface read it; a second enumeration elsewhere is forbidden and
// the store-suite convention is the check).
func Table() []Counter {
	order := []string{"retries", "security_rounds", "review_rounds", "total_failed_gates", "splits_done", "qa_rounds"}
	out := make([]Counter, 0, len(order))
	for _, name := range order {
		out = append(out, counters[name])
	}
	return out
}

// BumpResult is where a bump landed. CapExceeded means the counter is AT
// its cap — inclusive reaching (Caps()): reaching 2 of 2 is the report that
// bounds the loop, and the value never moves past the cap. AtLoopCeiling
// distinguishes the loop-wide non-convergence (a block) from a task
// struggling (an escalation) — T4 decides between them, from these flags,
// never by re-deriving the arithmetic.
type BumpResult struct {
	Counter       Counter
	Value         int
	CapExceeded   bool
	AtLoopCeiling bool
}

// Counters is the engine's one door to both levels. One entry point; the
// level comes from the name.
type Counters struct {
	store *store.Store
}

func NewCounters(st *store.Store) *Counters { return &Counters{store: st} }

// Bump advances one counter by name. taskID is required for task-level
// counters and refused for loop-level ones — misuse is answered by naming
// the counter and its level, so a call site that guessed wrong fails
// legibly instead of draining the wrong budget.
func (c *Counters) Bump(sc store.Scope, taskID, name string) (BumpResult, error) {
	counter, ok := Lookup(name)
	if !ok {
		return BumpResult{}, fmt.Errorf("loop: unknown counter %q — the set is closed: %v", name, names())
	}
	switch counter.Level {
	case LevelTask:
		if taskID == "" {
			return BumpResult{}, fmt.Errorf("loop: counter %q is task-level — it needs a task id; the level is the counter's, not the caller's", name)
		}
		if _, _, err := c.store.BumpTaskCounter(sc, taskID, name, counter.Cap); err != nil {
			return BumpResult{}, err
		}
		// Report the PERSISTED value, read back from the row. A value
		// computed arithmetically here could drift from the table under a
		// same-tick Now() collision (the measured T2 walk: two tasks in one
		// loop, both written inside one clock tick — GetTask's Ord tie-break
		// made an arithmetic report off by one). The row is the truth;
		// budgets are low-traffic, and the read-back is one extra row read
		// per bump.
		rows, err := c.TaskCounters(sc, taskID)
		if err != nil {
			return BumpResult{}, err
		}
		value := rows[name]
		return BumpResult{Counter: counter, Value: value, CapExceeded: counter.Cap > 0 && value >= counter.Cap}, nil
	case LevelLoop:
		if taskID != "" {
			return BumpResult{}, fmt.Errorf("loop: counter %q is loop-level — it does not take a task id; the level is the counter's, not the caller's", name)
		}
		if _, _, err := c.store.BumpLoopCounter(sc, name, counter.Cap); err != nil {
			return BumpResult{}, err
		}
		rows, err := c.LoopCounters(sc)
		if err != nil {
			return BumpResult{}, err
		}
		value := rows[name]
		return BumpResult{
			Counter: counter, Value: value,
			CapExceeded:   counter.Cap > 0 && value >= counter.Cap,
			AtLoopCeiling: name == "total_failed_gates" && value >= LoopCeiling,
		}, nil
	}
	// Unreachable through the closed table; the compiler would not let a
	// stray Level value fall through silently, and this is the belt.
	return BumpResult{}, fmt.Errorf("loop: counter %q has unknown level %q", name, counter.Level)
}

// DecideUnverified is Q8's answer in code: a gate that could not RUN
// charges ONLY the loop-wide counter — `total_failed_gates` and nothing
// else. A tool that will not run is not the handler's failure (O4), so no
// task budget moves and no `then:` ever fires from it; the counter it DOES
// charge is the one that bounds it, so repeated unverified ends in the
// ceiling block (Q11), never in an infinite redo.
func (c *Counters) DecideUnverified(sc store.Scope) (value int, atCeiling bool, err error) {
	res, err := c.Bump(sc, "", "total_failed_gates")
	if err != nil {
		return 0, false, err
	}
	return res.Value, res.AtLoopCeiling, nil
}

// TaskCounters reads one task's budgets as the table knows them — for
// `aeon loop show --counters`, the app panel, and the tests. It reads the
// table, never a second enumeration (the columns come from the same map
// that decides the bump).
func (c *Counters) TaskCounters(sc store.Scope, taskID string) (map[string]int, error) {
	task, err := c.store.GetTask(sc, taskID)
	if err != nil {
		return nil, err
	}
	return map[string]int{
		"retries":         task.Retries,
		"security_rounds": task.SecurityRounds,
		"review_rounds":   task.ReviewRounds,
	}, nil
}

// LoopCounters reads the loop-wide counters the same way.
func (c *Counters) LoopCounters(sc store.Scope) (map[string]int, error) {
	loop, err := c.store.GetLoop(sc)
	if err != nil {
		return nil, err
	}
	return map[string]int{
		"total_failed_gates": loop.TotalFailedGates,
		"splits_done":        loop.SplitsDone,
		"qa_rounds":          loop.QARounds,
	}, nil
}

// LoopCeilingReached is the engine's question at every gate failure: has
// the loop as a whole stopped converging? Reaching it BLOCKS
// (LoopCeilingOutcome, D3) — the same collapse discipline as G7's ask →
// deny: the answer that stops the loop says why in the terms a person acts
// on, and a human answering a question cannot fix it.
func (c *Counters) LoopCeilingReached(sc store.Scope) (bool, int, error) {
	loop, err := c.store.GetLoop(sc)
	if err != nil {
		return false, 0, err
	}
	return loop.TotalFailedGates >= LoopCeiling, loop.TotalFailedGates, nil
}

func names() []string {
	out := make([]string, 0, len(counters))
	for _, tc := range Table() {
		out = append(out, tc.Name)
	}
	return out
}

// --- the single decision point (S17 T4) -------------------------------------

// Exhaustion is one decision made in one place: the bump happens, the
// loop's ceiling is consulted, and the attempt is read from the PERSISTED
// values — per task for task-level names (D2), per loop for lane-level
// legacy names. Whoever asks "is this task out of budget" asks this method,
// because a second implementation is a second truth (the S8 per-loop map
// was exactly that second truth, charging every task from one ledger).
//
// It returns the attempt the redo should carry (0 for the counter-less
// form) and whether the then: action must now run.
func (c *Counters) DecideExhaustion(ctx context.Context, st *LoopState, sc store.Scope, taskID, name string, max int) (attempt int, exhausted bool, err error) {
	if name == "" {
		return 0, false, nil
	}
	if _, known := Lookup(name); known && taskID != "" {
		before, err := c.TaskCounters(sc, taskID)
		if err != nil {
			return 0, false, err
		}
		res, err := c.Bump(sc, taskID, name)
		if err != nil {
			return 0, false, err
		}
		// The attempt is how many times THIS task has been sent back:
		// the read value plus this rejection. The persisted value only
		// ever raises it — a second writer that moved the row past the
		// read means reality is ahead. It must never LOWER it: at the
		// cap the bump clamps and returns the cap, and letting that
		// pull attempt back to the cap (3 → 2) is exactly the rejection
		// that must read as exhausted (measured here: every budget then
		// re-ran its ladder forever until the loop ceiling caught it).
		attempt = before[name] + 1
		if res.Value > attempt {
			attempt = res.Value
		}
		// Every task-level gate failure also charges the loop's ceiling
		// (D3): the loop as a whole either converges or it does not.
		if _, err := c.Bump(sc, "", "total_failed_gates"); err != nil {
			return 0, false, err
		}
		if reached, _, err := c.LoopCeilingReached(sc); err != nil {
			return 0, false, err
		} else if reached {
			return attempt, true, nil
		}
		return attempt, attempt > max, nil
	}
	// Lane-level legacy ledger (the gate-revise `revisions` shape): the
	// per-loop map keeps its S8 behaviour — those edges are human
	// instructions, not task budgets.
	if st.Counters == nil {
		st.Counters = map[string]int{}
	}
	st.Counters[name]++
	return st.Counters[name], st.Counters[name] > max, nil
}
