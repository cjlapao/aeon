package loop

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// S17 T2 — two-level counters and their caps (L28, D2, D3, I2). The one
// piece of arithmetic in the slice, isolated because **a per-loop-only
// implementation passes every other test here** — which is precisely the
// failure TestTaskThreeKeepsItsOwnBudget exists to catch.

func openCountersStore(t *testing.T) (*store.Store, store.Scope) {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sc := store.Scope{LoopSlug: "counters-loop", ComponentRoot: "/project"}
	if _, err := st.CreateLoop(store.NewLoop{
		Slug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot, Root: "/project", Goal: "two-level counters", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
	return st, sc
}

func addTasks(t *testing.T, st *store.Store, sc store.Scope, ids ...string) {
	t.Helper()
	for i, id := range ids {
		if _, err := st.CreateTask(sc, store.NewTask{TaskID: id, Ord: i, Title: "task " + id}); err != nil {
			t.Fatal(err)
		}
	}
}

// TestTaskThreeKeepsItsOwnBudget — FIVE tasks. Tasks 1 and 2 each consume
// two review_rounds; task 3 still has **2 of 2** (D2, C2). A per-loop-only
// implementation shows task 3 at 0 of 2 and this is the test that fails it.
func TestTaskThreeKeepsItsOwnBudget(t *testing.T) {
	st, sc := openCountersStore(t)
	addTasks(t, st, sc, "T1", "T2", "T3", "T4", "T5")
	c := NewCounters(st)

	for _, who := range []string{"T1", "T2"} {
		for i := 0; i < 2; i++ {
			r, err := c.Bump(sc, who, "review_rounds")
			if err != nil {
				t.Fatalf("task %s review_rounds bump %d: %v", who, i+1, err)
			}
			if r.Value != i+1 {
				t.Fatalf("task %s review_rounds = %d, want %d", who, r.Value, i+1)
			}
		}
		// Both spend their full budget: at 2 of 2 the cap is reported.
		if r, err := c.Bump(sc, who, "review_rounds"); err != nil || r.Value != 2 || !r.CapExceeded {
			t.Fatalf("task %s at the cap: value=%d exceeded=%v err=%v — want 2 of 2 reported", who, r.Value, r.CapExceeded, err)
		}
	}

	// Task 3 keeps its own budget: untouched, and its remaining budget is
	// the full 2 of 2.
	got, err := c.TaskCounters(sc, "T3")
	if err != nil {
		t.Fatal(err)
	}
	if got["review_rounds"] != 0 || got["retries"] != 0 || got["security_rounds"] != 0 {
		t.Fatalf("T3's counters moved with T1/T2's spend: %v — a per-loop-only implementation fails HERE (D2)", got)
	}
	for _, id := range []string{"T4", "T5"} {
		got, err := c.TaskCounters(sc, id)
		if err != nil {
			t.Fatal(err)
		}
		if got["review_rounds"] != 0 {
			t.Fatalf("%s's review_rounds = %d, want 0", id, got["review_rounds"])
		}
	}
}

// TestLoopCeilingStopsTheLoop — twenty tasks each rejecting once, none
// capped individually: the LOOP stops at total_failed_gates (D3). Without
// the ceiling that is twenty send-backs nobody bounded — "a loop that runs
// all day producing send-backs".
func TestLoopCeilingStopsTheLoop(t *testing.T) {
	st, sc := openCountersStore(t)
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = string(rune('a'+i)) + "-task"
	}
	addTasks(t, st, sc, ids...)
	c := NewCounters(st)

	// Each task rejects exactly once; every reject charges the loop ceiling
	// — none of them is individually capped.
	var last BumpResult
	for _, id := range ids {
		r, err := c.Bump(sc, id, "retries")
		if err != nil {
			t.Fatal(err)
		}
		if r.CapExceeded {
			t.Fatalf("%s capped individually at retries=%d — the point of this test is that none of them is", id, r.Value)
		}
		last, err = c.Bump(sc, "", "total_failed_gates")
		if err != nil {
			t.Fatal(err)
		}
	}
	if last.Value != LoopCeiling {
		t.Fatalf("loop ceiling settled at %d, want the clamp at %d", last.Value, LoopCeiling)
	}
	if !last.AtLoopCeiling {
		t.Fatal("the twentieth gate did not report AtLoopCeiling — the loop would keep producing send-backs")
	}
	if !last.CapExceeded {
		t.Fatal("reaching the ceiling must report capExceeded (T4 turns it into a block)")
	}
}

// TestCounterLevelIsAPropertyOfTheCounter — no API lets a caller choose a
// level; the level comes from the name (I2). A counter whose level depends
// on who increments it WILL be incremented at both, and the bug surfaces as
// a budget that drains twice as fast on some tasks and not others.
func TestCounterLevelIsAPropertyOfTheCounter(t *testing.T) {
	for _, name := range []string{"retries", "security_rounds", "review_rounds"} {
		c, ok := Lookup(name)
		if !ok || c.Level != LevelTask {
			t.Errorf("%s: level = %v, want %s", name, c.Level, LevelTask)
		}
	}
	for _, name := range []string{"total_failed_gates", "splits_done", "qa_rounds"} {
		c, ok := Lookup(name)
		if !ok || c.Level != LevelLoop {
			t.Errorf("%s: level = %v, want %s", name, c.Level, LevelLoop)
		}
	}

	// One bump entry, not an IncrementTask/IncrementLoop pair for a caller
	// to choose between: exactly one exported method whose name carries
	// "Bump".
	rt := reflect.TypeOf(&Counters{})
	bumps := []string{}
	for i := 0; i < rt.NumMethod(); i++ {
		if strings.Contains(rt.Method(i).Name, "Bump") {
			bumps = append(bumps, rt.Method(i).Name)
		}
	}
	if len(bumps) != 1 {
		t.Fatalf("Counters exposes %d bump methods %v — the level must come from the name, never from the call site (I2)", len(bumps), bumps)
	}

	// Misuse is refused by name at the engine door: a task-level counter
	// with no task, and a loop-level counter carrying one.
	st, sc := openCountersStore(t)
	addTasks(t, st, sc, "T1")
	c := NewCounters(st)
	if _, err := c.Bump(sc, "", "retries"); err == nil || !strings.Contains(err.Error(), "retries") || !strings.Contains(err.Error(), "task-level") {
		t.Errorf("task counter without a task id: err = %v — must name the counter and its level", err)
	}
	if _, err := c.Bump(sc, "T1", "total_failed_gates"); err == nil || !strings.Contains(err.Error(), "loop-level") {
		t.Errorf("loop counter with a task id: err = %v — must refuse, the level is the counter's (I2)", err)
	}
	if _, err := c.Bump(sc, "T1", "review_rounds_extra"); err == nil || !strings.Contains(err.Error(), "review_rounds_extra") {
		t.Errorf("unknown counter: err = %v — must name the name it refused", err)
	}
}

// TestTaskCountersAreIndependent — incrementing task 1's counter does not
// move task 2's, for all three task-level counters.
func TestTaskCountersAreIndependent(t *testing.T) {
	st, sc := openCountersStore(t)
	addTasks(t, st, sc, "T1", "T2")
	c := NewCounters(st)
	for _, name := range []string{"retries", "security_rounds", "review_rounds"} {
		if _, err := c.Bump(sc, "T1", name); err != nil {
			t.Fatal(err)
		}
	}
	moved, err := c.TaskCounters(sc, "T2")
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range moved {
		if v != 0 {
			t.Errorf("T2's %s = %d after T1's bumps — task-level counters are per task", name, v)
		}
	}
}

// TestCapExceededIsReported — at 2 of 2, capExceeded is true, and the
// counter does NOT go to 3. Reaching the cap IS the report (the engine's
// T4 turns the flag into escalate/fail-item/block); the clamp keeps a
// misbehaving caller from spending a budget that is not there.
func TestCapExceededIsReported(t *testing.T) {
	st, sc := openCountersStore(t)
	addTasks(t, st, sc, "T1")
	c := NewCounters(st)
	if r, err := c.Bump(sc, "T1", "retries"); err != nil || r.Value != 1 || r.CapExceeded {
		t.Fatalf("1 of 2 reported value=%d exceeded=%v err=%v", r.Value, r.CapExceeded, err)
	}
	r, err := c.Bump(sc, "T1", "retries")
	if err != nil || r.Value != 2 || !r.CapExceeded {
		t.Fatalf("at the cap: value=%d exceeded=%v err=%v — want 2 and true", r.Value, r.CapExceeded, err)
	}
	// A further bump cannot push it to 3.
	r, err = c.Bump(sc, "T1", "retries")
	if err != nil || r.Value != 2 || !r.CapExceeded {
		t.Fatalf("past the cap: value=%d exceeded=%v err=%v — the counter stops at the cap", r.Value, r.CapExceeded, err)
	}
}

// TestCountersSurviveSuspendAndRecovery — S6's suspension and S7's recovery
// preserve counts: a step that regains its budget by crashing can loop
// forever. S5 stored the step deadline absolutely for the same reason.
func TestCountersSurviveSuspendAndRecovery(t *testing.T) {
	dir := t.TempDir()
	env := func(string) string { return "" }
	st, err := store.Open(store.Options{Dir: dir, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: "survive", ComponentRoot: "/p"}
	if _, err := st.CreateLoop(store.NewLoop{Slug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot, Root: "/p", Goal: "g", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateTask(sc, store.NewTask{TaskID: "T1", Ord: 0, Title: "t"}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCounters(st).Bump(sc, "T1", "review_rounds"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCounters(st).Bump(sc, "", "total_failed_gates"); err != nil {
		t.Fatal(err)
	}
	st.Close() // the process dies

	st2, err := store.Open(store.Options{Dir: dir, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	c := NewCounters(st2)
	task, err := c.TaskCounters(sc, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if task["review_rounds"] != 1 {
		t.Fatalf("review_rounds reset across a restart: %d — the cap is defeated", task["review_rounds"])
	}
	loop, err := c.LoopCounters(sc)
	if err != nil {
		t.Fatal(err)
	}
	if loop["total_failed_gates"] != 1 {
		t.Fatalf("total_failed_gates reset across a restart: %d", loop["total_failed_gates"])
	}
}

// TestSchemaFreshEqualsMigrated — Q1's answer, recorded as an assertion:
// the counter columns arrived with S4's 0001, so this slice adds NO
// migration. If a future counter needs a column it follows S4's discipline
// (numbered, one transaction, ledger-recorded) and the store suite's
// fresh-equals-migrated equivalence still holds.
//
// The count is the ledger's, not this slice's: 0026 (route metadata, the
// model-management wave), 0027 (step_message.provenance, S18's M26), 0028
// (route.wire_capabilities + route.no_price, S18's Q8/O8), 0029 (route_probe,
// S18 T3's probe cache), 0030 (step.thinking_dropped, S18 T5), 0031
// (loop.allow_outward, S19 T6), 0032 (trigger_schedule + trigger_fire,
// S20 T1) and 0033 (workspace_root/goal + started_by/trigger_id, S20 T2)
// and 0034 (trigger_gap, S20 T4) moved it after S17 closed. The claim asserted here is still
// S17's — the engine added none.
func TestSchemaFreshEqualsMigrated(t *testing.T) {
	m := store.Migrations()
	// The count is the LEDGER's, not a slice's to own, and the ledger moved
	// twice since this comment was first written: 41→42 (S35's proposal),
	// 42→43 (S26's roadmap_item), then 43→44 (S29's bug) and 44→45 (S37's
	// agent_import widening — renumbered from 0043 at merge, main's numbers
	// took it first) joined the FROZEN numbered block, while the file-based
	// path (internal/store/migrations/*.sql, adopted 2026-10-30 for parallel
	// worktrees) appended S30's graph tables and S32's ledger on top. So the
	// total is MEASURED here, never hardcoded — pinning it would make every
	// additive slice edit this loop test, which is not what it is for. What
	// it IS for is S17 T2's claim: THE ENGINE added no migration. The frozen
	// block is checked at its own end (it cannot shrink, and its last entry
	// is S37's — nobody back-edits the block), and the claim is checked
	// directly: no migration in the whole ledger is named like the counters.
	frozen := 0
	for _, mm := range m {
		if mm.ID > 1_000_000_000_000 {
			break // the first 14-digit timestamp ID is a file, not a literal
		}
		frozen++
	}
	if frozen < 45 {
		t.Fatalf("%d frozen legacy migrations — the block (1–45) is frozen; it cannot shrink", frozen)
	}
	last := m[frozen-1]
	if last.ID != frozen || !strings.Contains(last.Name, "proposal_agent_import") {
		t.Fatalf("the last legacy migration is %d/%s, want %d/0045_proposal_agent_import — S37 T1's agent_import widening (after S26's roadmap_item 0043 and S29's bug 0044; renumbered from 0043 at merge) — the range-and-count could-not-say record before the S38 pair is 0034 (the counters themselves still arrived with nobody's migration, exactly as S17 T2 answered)", last.ID, last.Name, frozen)
	}
	for _, mm := range m {
		if strings.Contains(strings.ToLower(mm.Name), "counter") {
			t.Fatalf("migration %d/%s is named like the counters — S17 T2 answered Q1 as NO migration (the counter columns exist since 0001); a new migration belongs to whoever needs the column, and nobody needs this one", mm.ID, mm.Name)
		}
	}
}

// TestLoopCeilingIsABlockNotAnEscalation — reaching the ceiling blocks: the
// loop as a whole is not converging, which differs from one task struggling,
// and a human answering a question would not fix it (D3). The decision is
// data here so T4's engine consumes one definition.
func TestLoopCeilingIsABlockNotAnEscalation(t *testing.T) {
	if LoopCeilingOutcome != "block" {
		t.Errorf("the ceiling's outcome is %q, want block — escalating stops the loop for a human who could not fix it", LoopCeilingOutcome)
	}
	st, sc := openCountersStore(t)
	c := NewCounters(st)
	for i := 0; i < LoopCeiling; i++ {
		if _, err := c.Bump(sc, "", "total_failed_gates"); err != nil {
			t.Fatal(err)
		}
	}
	reached, value, err := c.LoopCeilingReached(sc)
	if err != nil {
		t.Fatal(err)
	}
	if !reached || value != LoopCeiling {
		t.Fatalf("LoopCeilingReached = %v/%d, want true/%d", reached, value, LoopCeiling)
	}
}

// TestCapsMatchStoreMachine — store's machine.Caps() is S4's published
// bound (retries ≤ 2 …); the engine's table and the store's publication are
// one truth read two ways. Drift here means a cap that reports at one layer
// and enforces at another.
func TestCapsMatchStoreMachine(t *testing.T) {
	machine := store.Caps()
	for _, name := range []string{"retries", "security_rounds", "review_rounds", "qa_rounds"} {
		c, ok := Lookup(name)
		if !ok {
			t.Fatalf("%s missing from the counter table", name)
		}
		if c.Cap != machine[name] {
			t.Errorf("%s: engine cap %d, store machine cap %d — one definition", name, c.Cap, machine[name])
		}
	}
}

// TestLoopCountersReportTheTable — qa_rounds caps at 2 (v1's bound);
// splits_done carries no cap (Cap 0) and never reports exceeded.
func TestLoopCountersReportTheTable(t *testing.T) {
	st, sc := openCountersStore(t)
	c := NewCounters(st)
	for i := 0; i < 5; i++ {
		r, err := c.Bump(sc, "", "splits_done")
		if err != nil {
			t.Fatal(err)
		}
		if r.CapExceeded {
			t.Fatalf("splits_done is uncapped (Cap 0); exceeded at %d", r.Value)
		}
	}
	if v, err := c.LoopCounters(sc); err != nil || v["splits_done"] != 5 {
		t.Fatalf("splits_done = %d (err %v), want 5", v["splits_done"], err)
	}
	r, err := c.Bump(sc, "", "qa_rounds")
	if err != nil || r.Value != 1 {
		t.Fatalf("qa_rounds bump: %v %v", r, err)
	}
	if r, err := c.Bump(sc, "", "qa_rounds"); err != nil || r.Value != 2 || !r.CapExceeded {
		t.Fatalf("qa_rounds at cap: %v %v — want 2/2 reported", r, err)
	}
	if r, err := c.Bump(sc, "", "qa_rounds"); err != nil || r.Value != 2 {
		t.Fatalf("qa_rounds past cap: %v %v — clamped at 2", r, err)
	}
}
