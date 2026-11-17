# Design — S32 Parallel agents

Fills in `brief.md`: PA4's two parallel shapes get their exact schema and algorithm; PA5's worktree
mechanism gets its table and lifecycle; PA6/PA7 get their exact engine hook points. New rules PA8–PA15
cover what the brief only named. Cited from `plan.md` the way every other Fleet slice's lettered rules are.

## PA4, concretely — two shapes, one readiness question

**Shape A — fixed sibling lanes.** `blueprint.Lane` gains:
```go
type Lane struct {
    // ... existing fields unchanged ...
    Parallel      bool   `yaml:"parallel,omitempty" json:"parallel,omitempty"`      // Shape B, below
    ParallelGroup string `yaml:"parallel_group,omitempty" json:"parallelGroup,omitempty"` // Shape A
}
```
Lanes sharing a non-empty `ParallelGroup` are a declared-independent set. `internal/blueprint/validate.go`
(`Validate(b Blueprint, opts ValidationOptions) Report`, confirmed this session at `validate.go:84`) gains
a pass: for every `ParallelGroup` value, collect the `Produces` of every lane carrying it, and raise
`invalid` (not `unverifiable` — this is checkable at author time, no model call needed) if any two overlap.
A lane with no `When` guard narrowing it out of the current run is a candidate; `Decide`'s sibling (PA9)
reads the group membership, not this validation pass, at runtime — validation is the author-time guard
rail, not the runtime's source of truth.

**Shape B — iterate-lane items.** `Lane.Parallel = true` on an `iterate` lane (the devops pipeline's
`build` lane, `iterate: tasks`) means: **readiness per item comes from `task.depends_on` plus status,
not from "the next one in `ord`."** A task is **ready** iff `status == pending` and every id in
`depends_on` has a status in `{complete, merged, skipped, superseded}` (confirmed this session,
`internal/store/machine.go`'s `TaskStatus` enum — these are the successful terminals; `failed`/`blocked`
never satisfy a dependency). This is the exact graph `escalate.go` already walks for failure propagation
(`internal/loop/escalate.go:223-250`); PA9 below reuses it for readiness, not a second graph.

**Why this is enough, and nothing new is invented:** the devops pipeline's "T1-T5 parallel, then T6, then
T7, then T8-T10 parallel" shape is not a wave number — it falls out of T6 depending on all of T1-T5, T7
depending on T6, and T8-T10 depending on T7. The engine never reasons about "waves"; it only ever asks
"which pending tasks have every dependency satisfied right now," which is cheap and already indexed
(`ListTasks`, `ORDER BY ord, taskid`, confirmed this session — the new readiness filter runs over the same
rows).

## PA5, concretely — the worktree table and lifecycle

```sql
CREATE TABLE loop_worktree (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  component_root TEXT NOT NULL,
  loop_slug      TEXT NOT NULL,
  -- owner_kind/owner_id generalise over both PA4 shapes: a Shape-A lane
  -- has no task row to hang this on, a Shape-B item does.
  owner_kind    TEXT NOT NULL CHECK (owner_kind IN ('lane','task')),
  owner_id      TEXT NOT NULL,   -- lane id, or task id
  path          TEXT NOT NULL,
  branch        TEXT NOT NULL,
  base_sha      TEXT NOT NULL,
  created_at    TEXT NOT NULL,
  removed_at    TEXT,            -- NULL while the worktree is live — the recovery signal (PA12)
  FOREIGN KEY (component_root, loop_slug) REFERENCES loop (component_root, slug) ON DELETE CASCADE,
  UNIQUE (component_root, loop_slug, owner_kind, owner_id, removed_at)
);
```

**Deliberately not `component_root`** (brief §2's own distinction) — that table means "a human already
made this worktree and it is a permanent second component." This table means "the daemon made this,
nobody outside the loop knows it exists, and it is gone before the loop's next person-visible state
change." Never surfaced on any socket method, any CLI listing, or any UI — it is implementation detail of
one running loop, the same epistemic status as a file descriptor.

**Lifecycle, write-ahead (R1's mitigation):**
1. Before dispatch, **insert the `loop_worktree` row** (intent recorded) inside the SAME transaction that
   marks the task/position as about to run.
2. **Then** run `git worktree add <path> -b <branch> <base_sha>` (the new `GitActions.AddWorktree`,
   `internal/action/git_actions.go`, same signature shape as `CreateBranch`).
3. The unit's ladder runs with `WorkspaceRoot` = `path` (PA10).
4. On the unit reaching a terminal state (done/failed/skipped/superseded for Shape B; the lane's own
   terminal for Shape A), run `GitActions.RemoveWorktree(ctx, dir, path)`, **then** set `removed_at`.

A crash between steps 1–2 leaves a row with `removed_at IS NULL` and no real worktree — recovery (PA12)
tells the two apart by asking git, not by trusting the row alone. A crash between 3–4 leaves a real,
abandoned worktree with a live row — recovery resumes it, the same case `DetermineCase` already handles
for an ordinary step, just pointed at a different `WorkspaceRoot`.

**Base.** `base_sha` is the feature branch's tip **at the moment the worktree is created**, recorded so a
later audit can see what each unit actually started from; it is never rebased mid-run (brief §6 — merge
conflicts resolve at the forge, the same as any multi-PR workflow).

## PA6/PA7 — the exact hook points

**PA6 (gate suspends only its own unit).** `AdvanceLoop`'s existing `ActAskGate`/suspension path is
per-`Position` already (confirmed: `Result{..., Position: st.Position}`, `advance.go`); PA9's fan-out
(below) keeps each concurrently-dispatched position's own goroutine waiting on its own result, so a
suspension there simply means that one goroutine returns a `waiting` result into PA9's channel while
siblings' goroutines are untouched. **No new suspension code — the existing per-position suspension,
reached from more than one goroutine instead of one.**

**PA7 (failure blocks dependents only).** `escalate.go`'s transitive walk (`internal/loop/escalate.go:223`)
is unchanged. The one new discipline: PA9's dispatcher must not treat "a sibling failed" as a reason to
cancel a context any other goroutine is running on — contexts are per-unit, not shared, so this is an
absence of a wrong behavior rather than new code.

## PA8 — `Decide`'s sibling: a set, not one

```go
// internal/loop/decide.go — new, beside Decide (unchanged)
func DecideReady(bp *blueprint.Blueprint, st LoopState, cap int) []Action
```
At `cap == 1`, `DecideReady` returns a slice holding exactly `Decide`'s own result — **implemented by
calling `Decide` for the single-action case**, not a reimplementation that happens to agree (D8's
byte-identity requirement is a test, and the surest way to pass it is to not have a second code path for
cap 1 at all). At `cap > 1`, it evaluates every lane: a non-`iterate` lane behaves as `Decide` already
does; an `iterate` lane with `Parallel: true` returns every ready item (PA4 Shape B) up to the remaining
budget; a set of lanes sharing a live `ParallelGroup` (PA4 Shape A) that are all currently reachable
(their `When` guards pass) are returned together, also bounded by the remaining budget.

## PA9 — the fan-out: goroutines the loop's own goroutine owns, coordinated by a channel

```go
// inside Run (runner.go) / a new AdvanceLoopParallel, called when cap > 1
results := make(chan unitResult, cap)
for _, action := range ready {
    go func(a Action) {
        res, err := runOneUnit(ctx, dep, st, a)   // PA10 wraps dep.Model with worktree setup/teardown
        results <- unitResult{action: a, result: res, err: err}
    }(action)
}
for pending > 0 {
    r := <-results
    pending--
    // persist r's outcome, update task/lane state, re-run DecideReady for
    // anything newly ready (a sibling's completion may satisfy a dependent's
    // depends_on), launch any new ready unit up to the remaining budget
}
```
This is literally the pattern from the user's own framing: a Go function per concurrently-running unit,
communicating back to the loop's own goroutine over a channel, which is itself the one `go func()`
`Runner.Start` already launches per loop — **one more layer of the same mechanism, not a new one.** No
worker pool, no job queue package: `cap` is small (a human-set ceiling, PA2), so `cap` live goroutines is
the entire scheduling data structure.

## PA10 — `WorkspaceRoot` per unit, not per loop

`runOneUnit` resolves the unit's `WorkspaceRoot`: Shape B items and Shape A lanes both get PA5's worktree
path; `dep.Model`'s call signature is unchanged (it already threads a workspace path per call — the
existing per-loop `Root`/`ComponentRoot` split, `internal/store/loops.go:138`'s `orDefault(in.Root,
in.ComponentRoot)`, is the precedent this slice extends from "once per loop" to "once per concurrently
running unit").

## PA11 — the validator pass (Shape A's author-time guard)

`internal/blueprint/validate.go`'s new pass, run once per blueprint load: group lanes by `ParallelGroup`,
union their `Produces`, raise `invalid` with both lane ids named on any overlap. A lane with an empty
`ParallelGroup` is never checked against anything — it opts out by omission, matching every other optional
blueprint field's convention.

## PA12 — recovery: per position, plus the worktree reconciliation pass

Startup recovery (S7) gains one pass before its existing per-step `DetermineCase` walk: for every
`loop_worktree` row with `removed_at IS NULL`, ask git (`git worktree list` against the component's primary
checkout) whether the path is real.
- **Row says live, git agrees** → proceed to the position's ordinary `DetermineCase` (unchanged), using
  the worktree's path as `WorkspaceRoot`.
- **Row says live, git disagrees (crashed between insert and `add`, PA5 step 1→2)** → the position never
  actually started in that worktree; recreate it (same `base_sha`, same branch name — `AddWorktree` is
  idempotent against an already-existing branch the same way `CreateBranch` already is, `git_actions.go:
  94-131`'s `Reused` path) and proceed.
- **Row says live, the position itself already settled (crashed between 3→4)** → remove the worktree, set
  `removed_at`, no position to recover.

## PA13 — `RuntimeState.Position` widens to a set, carefully

`RuntimeState.Position Position` (`internal/loop/state.go`) becomes `RuntimeState.Positions []Position` —
**R2's file-by-file audit, not a type rename alone.** `StepStates`/`LaneStates` need no change (already
maps, already tolerate several live keys). At `cap == 1`, `Positions` holds exactly one element always —
every reader that only ever expected one (the tracker's `phases.go`, any JSON consumer) keeps working
unmodified by reading `Positions[0]` where it used to read `Position`, and a migration helper decodes an
old single-`Position` JSON blob into a one-element `Positions` slice for backward reads of existing rows
(additive, D8).

## PA14 — the cap lives on `component`

```sql
ALTER TABLE component ADD COLUMN max_parallel_agents INTEGER NOT NULL DEFAULT 1
  CHECK (max_parallel_agents BETWEEN 1 AND 16);
```
**Correction from the pre-S38 ROADMAP amendment**, which named `project.maxParallelAgents` before S38
renamed that table. `component.update`'s validator refuses a value outside the range, naming it (the same
convention every other bounded field in this codebase uses).

## PA15 — lowering the cap mid-run (D6)

PA9's loop reads `component.max_parallel_agents` **fresh, each time it is about to launch a new unit** —
never cached for the duration of a loop's run. Lowering it never touches `pending`/the in-flight
goroutines; it only reduces how many *new* units get launched as existing ones finish, which is exactly
"drain to the new cap."

## Test map

| # | Test | Proves |
| --- | --- | --- |
| 1 | `maxParallelAgents = 1`, full existing `internal/loop` suite | D8 — byte-identical serial behavior |
| 2 | Two sibling lanes, same `parallel_group`, disjoint `produces`, cap ≥ 2 → both dispatch concurrently, both complete, final state matches the serial run of the same blueprint | D1, PA8/PA9 |
| 3 | Overlapping `produces` in one `parallel_group` → `invalid` diagnostic at validation, never reaches the runtime | PA11 |
| 4 | Ten tasks with the brief's T1-T5/T6/T7/T8-T10 dependency shape, cap ≥ 5 → the three "waves" emerge from `depends_on` alone, no wave concept in the engine | D2, PA4 Shape B |
| 5 | `kill -9` with two running positions → both recover, each from its own worktree (one reattached, one rebuilt if the crash landed before `add`) | D3, PA12 |
| 6 | A gate inside one parallel unit → only that unit suspends; a sibling started before it keeps running and later completes | D4, PA6 |
| 7 | One task fails with an independent sibling in flight → the sibling is never cancelled; the failed task's dependents are blocked (existing `escalate.go` test, re-run under concurrency) | D5, PA7 |
| 8 | Lower the cap mid-run with N > new cap active → nothing in flight is killed, no new unit launches past the new cap until one finishes | D6, PA15 |
| 9 | Two concurrently-running positions, S31's event stream, rendered | D7 (confirm, don't assume, per the brief) |
| 10 | Worktree write-ahead: crash immediately after the `loop_worktree` insert, before `git worktree add` → recovery recreates it, no orphaned row, no orphaned directory | R1, PA12 |
