# Plan — S32 Parallel agents

## What is settled before planning

Everything in `brief.md` (PA1–PA7, the four DoD items, the seams) and `design.md`'s PA4–PA15. In
particular: two parallel shapes, one mechanism (PA4); worktree-per-concurrent-unit for both, write-ahead
recorded in `loop_worktree` before the filesystem operation (PA5); `Decide` stays untouched, `DecideReady`
is the new sibling and at `cap == 1` calls through it unchanged (PA8, D8); the fan-out is `go func()` per
ready unit plus a channel the loop's own existing goroutine selects on (PA9) — one more layer of the
pattern `Runner.Start` already uses, not a new subsystem; the cap lives on `component`, corrected from the
pre-S38 "project" naming (PA14).

## How we will do it

Schema and blueprint-schema first (nothing else is testable without them), then `RuntimeState.Position`'s
widening **before** `DecideReady`/the fan-out — D8's byte-identity requirement is easiest to prove when the
type can already hold a one-element set before anything tries to put two elements in it. Git worktree
support lands beside the widening since the fan-out task needs both. Recovery and the gate/failure/drain
semantics each get their own task because each is a different kind of correctness claim (crash safety,
suspension scope, blast radius, operational control) and deserves its own test mass. Close-out proves D7
(the UI claim) rather than assuming it, per the brief's own insistence.

## Open questions this plan answers

- **Q1** (does `AddWorktree`'s branch-already-exists path need its own `Reused` handling, or does
  `CreateBranch`'s existing pattern already cover it?) — T3's own investigation: read `git_actions.go:
  94-131` before writing `AddWorktree`, reuse the pattern rather than inventing a second one.
- **Q2** (where exactly does `phases.go`'s tracker need to change for `Positions` being plural?) — T4's
  first step: `trackedLane` and friends read `st.Position` today; `VERIFY` every read site before deciding
  whether the tracker needs real multi-position awareness or can keep reading "the first/primary position"
  for its own narrower purpose (the tracker explicitly does not track iterating lanes today, `phases.go`'s
  own comment — confirm Shape A's tracked, non-iterating lanes are the only ones the tracker ever sees
  plural for, and that it can reasonably show "most advanced of the group" or similar rather than needing
  a redesign).
- **Q3** (how many of the twelve-ish `step`/`loop`/`protocol` JSON shapes that embed `Position` actually
  reach a wire client, versus being purely internal?) — T4 answers this by grepping, not guessing, before
  deciding which shapes need an additive `positions` field alongside a kept, back-compat `position` (the
  first/primary one) versus which can just change.

## Task sequence

### T1 — Schema: the cap and the worktree ledger ([`tasks/T1.md`](tasks/T1.md))

Migration (next free number, measured at seeding): `ALTER TABLE component ADD COLUMN
max_parallel_agents` (PA14) and the new `loop_worktree` table (PA5). Store accessors:
`ComponentMaxParallelAgents(id)`, `CreateLoopWorktree`/`ListLiveLoopWorktrees`/`RemoveLoopWorktree`
(guarded UPDATE setting `removed_at`, same claimed-row discipline every other settle-once table in this
codebase uses). **Depends: nothing (first).**

### T2 — Blueprint schema and the validator pass ([`tasks/T2.md`](tasks/T2.md))

`blueprint.Lane` gains `Parallel`/`ParallelGroup` (PA4). `internal/blueprint/validate.go` gains the
`Produces`-disjointness pass over `ParallelGroup` membership (PA11), raising `invalid` with both lane ids
named. Fixture blueprints: a passing two-lane `parallel_group` example and a failing overlapping-produces
one, for the validator's own test suite. **Depends: nothing — parallel-safe with T1.**

### T3 — Git worktree actions ([`tasks/T3.md`](tasks/T3.md))

Answers Q1. `GitActions.AddWorktree(ctx, dir, path string, names BranchNames) (GitResult, error)` and
`RemoveWorktree(ctx, dir, path string) (GitResult, error)`, same signature shape and `StrategyCheckThenAct`
/ `EffectLocal` classification as `CreateBranch` (`internal/action/class.go`). Tests against a real
throwaway git repo (this project's existing convention for S16-shaped tests — never a fake git). **Depends:
nothing — parallel-safe with T1/T2.**

### T4 — `RuntimeState.Position` → `Positions` ([`tasks/T4.md`](tasks/T4.md))

Answers Q2 and Q3. The widening (PA13): `Positions []Position`, a backward-read decoder for existing
single-`Position` JSON rows, every internal reader audited (`phases.go`, `escalate.go`, any JSON-facing
protocol shape named by Q3's grep) updated to read `Positions[0]` where a single primary position is still
the right semantic, or genuinely iterate where plural state now matters. **No behavior change at this
task** — this is purely the type widening and the back-compat decode path, proven by the existing suite
passing unmodified (an early, narrow slice of D8). **Depends: T1** (the migration's shape informs whether
any stored JSON needs a one-time rewrite versus a read-time decode — likely the latter, decided here).

### T5 — `DecideReady` and the worktree-aware fan-out ([`tasks/T5.md`](tasks/T5.md))

PA8's `DecideReady(bp, st, cap) []Action`, calling `Decide` for the `cap == 1` case verbatim. PA9's
fan-out inside `Run`/a new `AdvanceLoopParallel`: one `go func()` per ready action, a results channel,
re-`DecideReady` on every completion. PA10's per-unit `WorkspaceRoot`: before a unit's first dispatch,
create its `loop_worktree` row and real worktree (T1/T3's functions, PA5's write-ahead order exactly);
on terminal, remove it. **This is the slice's largest task** — it is where PA4's two shapes, PA5's
lifecycle, PA8's decision, and PA9's coordination all meet. **Depends: T1, T2, T3, T4.**

### T6 — Recovery: per-position resume, worktree reconciliation ([`tasks/T6.md`](tasks/T6.md))

PA12: the startup pass that reconciles every live `loop_worktree` row against `git worktree list` before
`DetermineCase` runs for that position; the three cases (clean reattach, crashed-before-add, crashed-after-
settle) each get their own test with a real `kill -9`. Per-position `DetermineCase`/`RecoverStep` calls,
one per entry in `Positions`, not one per loop. **Depends: T5.**

### T7 — Gate scope, failure blast radius, cap draining ([`tasks/T7.md`](tasks/T7.md))

PA6 (a gate suspends only its own unit — a focused test proving a sibling keeps running, since the
mechanism itself needs no new code), PA7 (failure blocks dependents only — re-running `escalate.go`'s
existing test under real concurrency to prove the claim still holds with contexts that are genuinely
per-unit, not shared), PA15 (lowering the cap mid-run drains, never kills). **Depends: T5.**

### T8 — The two-shape acceptance tests ([`tasks/T8.md`](tasks/T8.md))

The brief's own D1/D2 as real, end-to-end tests (not unit tests of the pieces): a two-lane `parallel_group`
blueprint run at cap 1 and cap 2, same final state; a ten-task `depends_on`-graph blueprint producing the
brief's exact T1-T5/T6/T7/T8-T10 shape, with the engine never computing a "wave." **Depends: T5, T6, T7.**

### T9 — Close-out ([`tasks/T9.md`](tasks/T9.md))

D7 verified, not assumed: a real two-position run watched through S31's event stream/UI, confirming no UI
change was needed (or naming the one that was, if the brief's assumption was wrong — report either way).
Full DoD sweep (D1–D8) re-run together on a clean tree; measured numbers (worktree create/remove timing,
goroutine count under a real multi-task run) into Notes; `STATUS.md`/`ROADMAP.md` flipped to built.
**Depends: T1–T8.**

## Sequencing summary

```
T1 ─┬► T4 ─┬► T5 ─┬► T6 ─┐
T2 ─┤      │      ├► T7 ─┼► T8 ─► T9
T3 ─┘      │      │      │
           └──────┴──────┘
```

T1/T2/T3 are independent of each other. T4 needs T1 (the migration shapes the decode question). T5 is the
convergence point and the slice's largest task. T6/T7 are independent of each other once T5 exists. T8
needs all three of T5/T6/T7 proven. Nine tasks; the slice adds one schema change, one blueprint-schema
change, two new git actions, one type widening, one new decision function, one fan-out dispatcher, one
recovery pass, and zero new concurrency primitives beyond `go func()` and a channel.
