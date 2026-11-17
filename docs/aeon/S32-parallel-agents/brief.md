# Brief — S32 Parallel agents

**Implements:** `design.md`/`plan.md`/task files — promoted from [`SKETCH.md`](SKETCH.md) 2026-10-28, after a
real conversation resolved its seven open questions against the live code (not re-guessed). **Decision it
implements:** [P33](../ARCHITECTURE.md) (2026-10-24, at the user's direction). **Prefix:** `PA`. **Stage:**
5 · Fleet. **Depends on:** S8 (blueprints, `internal/loop`), S7 (recovery), S31 (built — the event shape
already carries `stepId`/`agent`, so this slice adds none there), S38 (built — the `task`/`depends_on`
mechanism this slice's scheduling rests on). **Interacts with:** S16 (git/forge actions — this slice is the
first caller to need more than one working tree), S17 (iterating lanes), P28 (memory budget). **Repo:**
Aeon lives at `aeon/` in this repository (P24) · Go 1.26 · `cgo` enabled (B3)

---

## 0. What changed between the sketch and this brief

The sketch (2026-09-07-ish, pre-S38) posed seven open questions and left them for "the brief writer." They
were worked through directly against the real code rather than re-derived from first principles, and two
real, load-bearing discoveries came out of it:

1. **The dependency graph this slice needs already exists.** `task.depends_on` (`internal/store/
   schema.go`'s `task_new` table) is a JSON array of task ids, already settable by the agent that creates a
   task (`task_create`'s own tool description: *"gates completion until each is done"*), and already read
   by the engine — `internal/loop/escalate.go` walks it transitively today, to block dependents when a task
   fails. This slice does not invent a scheduling graph; it teaches the engine to treat the **existing**
   graph as a *readiness* signal, not only a *failure-propagation* one.
2. **The merge step is not a local git operation.** `forge.merge` → `ForgeActions.MergePR` calls the
   forge's remote API (`a.Forge.MergePR`); there is no local `git merge` anywhere in a task's ladder. What
   *is* local and genuinely shared today is `GitActions.CreateBranch`'s `git checkout` and every
   tool-resolved file write (`ToolContext.WorkspaceRoot`) — both mutate ONE directory in place. That is the
   real collision, and it is a disk-contention problem, not a merge-semantics one.

## 1. What this is

A loop can have more than one agent working at the same time — either several items of one `iterate` lane
(the devops pipeline's "build one task" ladder, run for several tasks at once) or several fixed sibling
lanes that do not depend on each other (a survey and a lint, today forced to run one after the other for no
reason). Both are governed by the **same** mechanism: **the daemon dispatches up to `maxParallelAgents`
ready units at once, each in its own throwaway git worktree, coordinated by a channel the loop's own
goroutine already owns.**

## 2. What exists today, measured — the gap is real, not assumed

- **The loop already runs as a background worker, just not expressed that way in prose until now.**
  `internal/loop/runner.go`'s `Runner.Start(ctx, id)` launches exactly one `go func() { Run(ctx, dep, id)
  }()` per loop id, tracked in a mutex-guarded set; the goroutine's only way of talking to the rest of the
  system is **writing to the store and emitting events** (`emit(dep, "loop.blocked", …)`), which is how
  P29's "the daemon owns loop execution, never a detached process" already works in code, not only in the
  architecture doc. **This slice does not introduce background workers — it adds a second, inner layer of
  them:** one goroutine per concurrently-dispatched position, launched *from* the loop's own goroutine,
  coordinated with a channel, never detached from it.
- **The single-position assumption lives in exactly one place.** `AdvanceLoop` (`internal/loop/advance.go:91`)
  decides one `Position` and, for `ActRunModelStep`, calls `dep.Model(ctx, st, step, prompt)` **synchronously**
  inside the loop's one goroutine. `Decide`/`decideStep` (`internal/loop/decide.go:35,125`) return one
  `Action`. Nothing else in the store or the protocol layer assumes one running step per loop — `step` has
  no unique constraint preventing several `running` rows for the same loop, and `StepsByStatus` already
  reads across every component, not one row at a time.
- **`RuntimeState.Position` is singular; `StepStates`/`LaneStates` are already maps.** (`internal/loop/
  state.go`). The structural gap is exactly one field wide.
- **The shared-worktree collision is real and specific.** `GitActions.CreateBranch` (`internal/action/
  git_actions.go:94`) runs `git checkout <branch>` in one `dir`; every file tool resolves against
  `ToolContext.WorkspaceRoot`. Two concurrently-running `implement` steps in the same directory collide on
  disk regardless of which branch either believes it is on.
- **A related-but-distinct mechanism already exists and must not be confused with this slice's worktrees.**
  `component_root` (`internal/store/scope.go`'s header comment, `schema.go`'s migration 0001) already
  treats a *human-created, permanent* git worktree as a **separate component** with its own loop history.
  This slice's worktrees are the opposite: **daemon-created, ephemeral, scoped to one task's or one lane's
  single run, and never become a component** — they are scratch space that disappears when the work
  finishes, tracked in a new table (design.md PA7), not `component_root`.
- **`maxParallelAgents` has no column yet**, and the ROADMAP amendment proposing one predates S38 — it
  named `project.maxParallelAgents`, but S38 renamed that table to `component`. **The cap belongs on
  `component`,** corrected here before a migration ships the wrong table.

## 3. What is decided (by the user, 2026-10-24 and this conversation)

| | |
| --- | --- |
| **PA1** | Parallelism is allowed, and opted into per **component** (not "project" — S38 correction). `component.maxParallelAgents`, integer, **default 1**. At 1 the engine behaves exactly as today — same events, same order, same rows |
| **PA2** | The cap is a ceiling, not a target. The daemon runs *up to* N ready units; it never invents parallel work the blueprint and the task graph did not make independent |
| **PA3** | Only the daemon dispatches. A1 (nothing spawns), AS4 (async is a suspension) and K9 (tools serial within a step) are unchanged |
| **PA4** | **Two parallel shapes, one mechanism.** An `iterate` lane opts in with `parallel: true`; readiness among its dynamically-created items comes from the **existing** `task.depends_on` graph plus status (a task is ready when every dependency reached a successful terminal state). A set of fixed sibling lanes opts in by sharing a `parallel_group` name; the **existing** `Step.Produces` field is validated for disjointness across a group at blueprint-validation time — overlapping `produces` in the same group is an `invalid` diagnostic, not a runtime race |
| **PA5** | **Worktree per concurrently-dispatched unit, for both shapes.** No locking, no path-overlap bookkeeping at runtime — PA4's `produces` check is a blueprint-authoring-time sanity check, not the runtime's conflict prevention. The runtime's actual safety is: different directory, every time more than one unit runs at once |
| **PA6** | A gate or question inside one concurrently-running unit suspends only that unit. Siblings are isolated by PA5 and have no reason to stop. The loop reaches `waiting` only when every active position is blocked or waiting |
| **PA7** | A task failure blocks its transitive dependents (unchanged — `escalate.go` already does this) and does nothing to any unit it has no edge to. Cancelling independent, in-flight work on an unrelated failure is refused by name, not by omission |

## 4. Definition of done

| | |
| --- | --- |
| **D1** | A two-lane blueprint with a declared `parallel_group` on independent lanes runs both at once when `maxParallelAgents ≥ 2`, and serially at 1 — same rows and same final state either way |
| **D2** | An `iterate: tasks` lane with `parallel: true` and a real `depends_on` graph runs every ready task up to the cap at once; task completions and new readiness are discovered without polling (channel-driven, not a sleep loop) |
| **D3** | `kill -9` with two concurrently-running positions recovers both, each from its own checkpoint, and each from a correctly reattached or cleanly rebuilt worktree |
| **D4** | A gate inside one parallel unit suspends only that unit; siblings continue |
| **D5** | A failed task blocks its dependents and nothing else; an unrelated sibling already running is never cancelled |
| **D6** | Lowering `maxParallelAgents` while N > the new cap are running drains to the new cap without killing anything in flight |
| **D7** | The S31 loop view shows concurrently-running positions interleaved with no UI change beyond what S31 already shipped (verified, not assumed — S31's event shape already carries `stepId`/`agent`) |
| **D8** | At `maxParallelAgents = 1`, every existing test in `internal/loop` passes unmodified — the serial path is not a special case of the parallel path, it is byte-identical behavior proven by the existing suite |

## 5. The seams

**Inside `internal/loop`.** `Decide`/`decideStep` gain a sibling that returns a **set** of ready actions
instead of one; `AdvanceLoop`'s `ActRunModelStep` case gains a fan-out path. The serial case (cap 1) must
call through the same code the current `Decide`/single-dispatch path uses — D8 is not negotiable.

**`internal/action` (S16).** Two new `GitActions` methods, `AddWorktree`/`RemoveWorktree`, same shape as
`CreateBranch`/`CommitAndPush` (`ctx, dir, …) (GitResult, error)`).

**`internal/store`.** `component.max_parallel_agents` (new column); a new `loop_worktree` table (design.md
PA7) — deliberately not reusing `component_root`, which means something else (§2).

**Recovery (S7).** `DetermineCase`/`RecoverStep` are unchanged in kind; they now run once per active
position instead of once per loop, and a new pre-check (does this position's `worktree_path` still exist,
does `git worktree list` agree) runs before either.

## 6. What this is not

- **Not a new concurrency primitive.** The mechanism is `go func()` + a channel, coordinated from the
  loop's own existing goroutine — the same shape `Runner.Start` already uses one level up, not a worker
  pool, not a job queue.
- **Not a spawned OS process.** P29/P33 already forbid this, with their own reasoning on record; this
  slice does not reopen that question.
- **Not a rebase or merge-conflict resolver.** A task's branch merges via the forge's API against whatever
  the feature branch's tip is at merge time — exactly how any team's multi-PR workflow already behaves.
  This slice adds no merge logic.
- **Not automatic parallelism.** Every unit that runs concurrently does so because a blueprint author
  declared `parallel_group`/`parallel: true` and an agent's `depends_on` graph (or the lack of an edge
  between two lanes) says it may. Nothing infers independence from silence.

## 7. Risks, named where they live

- **R1 — worktree lifecycle is the slice's real correctness risk**, same shape as the sketch's original
  concern, now concrete: a crash between "worktree created" and "task row written" must not leak a
  directory nobody can find, and a crash between "task settled" and "worktree removed" must not leave one
  `git worktree list` still reports. Mitigated by making `loop_worktree` the durable record of intent
  *before* the filesystem operation, not after (write-ahead, same discipline tool receipts already use for
  `bash`, S7 P27).
- **R2 — `internal/loop`'s single-position assumption is load-bearing in more places than `AdvanceLoop`.**
  `Position` appears in `Result`, in the tracker's phase logic (`phases.go`), and in several JSON shapes
  already on the wire. Widening it is a careful, file-by-file audit, not a type rename.
- **R3 — S16's code actions must not assume a single writer**, named in the sketch and now answered:
  they don't need to stop assuming it, because PA5 means each concurrent caller already has its own `dir`.
  The risk is narrower than the sketch feared: no `internal/action` code changes for conflict-avoidance,
  only the two new worktree methods.

---

**Amended 2026-11-04 by S60** — the worktree ledger gained the `integration`
owner (integration refs listed, never deleted), and T0's open unit-workspace gap
closed as S60 T5's named default: the step-workspace seam (`WorkDir` threaded to
`runOneUnit`, gated on the git seam — `applyEvidenceWorkDir` the single writer).
See `S60-merge-queue/tasks/T5.md`.
