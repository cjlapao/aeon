# Design — S17 The full ladder (Stage 3 · Car)

**Date:** 2026-09-06
**Status:** approved (design); not yet implemented
**Stage:** 3 · Car, fourth of four — **the car**
**Depends on:** S8 (blueprints), S11 (the guard), S13 (handlers), S16 (code actions).
**Inherits:** L1–L10 and the ladder's shape from `docs/_archive/aeon-v1-horizontal/07-loop-decide` and
[`08-loop-execute`](../../_archive/aeon-v1-horizontal/08-loop-execute/plan.md). New decisions start at **L27**.

---

## Context

**This slice builds almost no new mechanism.** Lanes exist (S8), handlers exist (S13), code actions exist (S16), the
guard exists (S11), the store's fourteen task states and their transition machine exist (S4). What does not exist is the
**method** — the sequence, the gates, the round caps, and the send-backs that make those parts a development process
rather than a pile of capabilities.

So this is mostly **composition and authorship**, and the risk is concentrated in two places: the arithmetic of round
caps, and the ordering of tasks that depend on each other.

### The protocol already answers most of it

`opencode/skills/dev-loop-protocol/SKILL.md` is 793 lines describing a working method, and reading it settles several
questions this design would otherwise have had to invent. Four in particular.

**Counters are already two-level.** v1 separates them explicitly:

| Level | Counters | Cap |
| --- | --- | --- |
| **Task** | `retries`, `security_rounds`, `review_rounds` | **2**, and `capExceeded` ⇒ escalate |
| **Loop** | `total_failed_gates`, `splits_done`, `qa_rounds` | loop-wide |

That split is the answer to a question that bites: *is `review_rounds` per-task or per-loop?* Per-loop means task three
inherits task one's exhausted budget and cannot be reviewed at all. Per-task means a twenty-task loop can spend forty
review rounds. **v1's answer is both, at different levels** — and it is inherited rather than invented (L28).

**Dependencies are validated at plan load, not discovered at merge.** `tasks_derive` refuses a batch whose `depends_on`
names a task that does not exist, a self-dependency, or a duplicate id — because *"all three produce a task that can
never complete, and catching them here names the plan instead of surfacing a mystery at merge time."*

**The review gate is on review *state*, not threads.** *"Not every server can resolve conversation threads… So the loop
gates on the review STATE, which every server supports."* Deferred non-blocking findings live in a durable backlog file,
not as merge-blocking threads. That is server-agnosticism, and it is a constraint on the review lane rather than a
preference (L34).

**And verification has three states.** The protocol is explicit: an OOM-killed linter means *"lint is **unverified**,
which is not the same as clean. Do not report `verification=pass` on an unverified gate."*

That is [P31](../ARCHITECTURE.md#8-product-decisions) again — **its fifth appearance**, after cost, the `bash` receipt,
blueprint validation and liveness. A verify lane that can only say pass or fail has to lie about the case where the
check did not run, and it will lie in the reassuring direction (L33).

### The problem S8's `iterate` did not have to solve

S8's `iterate` is a **forward walk**: `(lane, item, step)`, item-major, monotonic. That was the right shape and it costs
nothing until items have an order they must respect.

Tasks have `depends_on`. So either the walk must skip blocked items and revisit them — making the position
non-monotonic, which the shape was chosen to avoid — or the order must be settled before the walk begins.

**Settle it at load** (L31): topologically sort the plan's tasks, refuse a cycle there, and let `iterate` stay the
forward walk it was designed as. The sort is deterministic, so a loop remains reproducible, and a cycle is named against
the plan rather than discovered as a task that never completes.

### What makes this a product claim rather than a capability

Every earlier demo showed a mechanism behaving correctly. **A ladder where nothing ever rejects has not been
demonstrated** — `verify`, `security`, `review` and `qa` are decoration if they only ever pass.

So the demo carries a **deliberate defect** that a downstream lane catches and sends back (L30). The claim is not *"it
built a feature"*; it is *"it built a feature, **caught its own mistake**, and fixed it inside its round cap."* That is
the method working, and it is the only version of this demo that proves anything the earlier ones did not.

## Goal

The full `dev-loop` ladder, running: investigate, plan, one approval gate, then per task — issue, implement, verify,
security, push, PR, review, merge — with send-backs bounded by two-level caps, and a real feature merged at the end.

## Decisions

L1–L10 and L20–L26 are inherited. New:

| # | Decision | Why |
| --- | --- | --- |
| L27 | **A phase is what a lane records.** `TRACKED_PHASES` is the set of lanes carrying phase state; an **iterating** lane records per-item task state instead | Two names for one thing would become two sources of truth about progress. And an iterating lane's progress genuinely is per-item — a single `build` phase status cannot say which of five tasks is in review |
| L28 | **Counters are two-level, inherited from v1**: task-level `retries`/`security_rounds`/`review_rounds` capped at 2, loop-level `total_failed_gates`/`splits_done`/`qa_rounds` | Per-loop alone starves late tasks; per-task alone lets a twenty-task loop spend forty rounds. v1 already separated them, and the separation is the answer rather than a compromise |
| L29 | **`escalate` collapses to `block` in an unattended loop** | Escalating to a human nobody can reach is a loop that stops without saying why. Same collapse as G7's `ask` → deny, and consistent for the same reason |
| L30 | **The demo carries a deliberate defect caught by a downstream lane** | A ladder where nothing rejects has not been demonstrated. `verify`, `security`, `review` and `qa` are decoration until one of them sends something back |
| L31 | **Tasks are topologically sorted at plan load, and a cycle is refused there**; `iterate` stays a forward walk | v1 already refuses a bad `depends_on` at load, *"instead of surfacing a mystery at merge time"*. Sorting there keeps S8's monotonic position and keeps the loop reproducible |
| L32 | **A redo re-enters carrying the rejection's reason**, not as a bare retry | Re-running `implement` with no information is the same call that just failed, and it spends a round reproducing the same output. S8's `with:` is the mechanism; the reason is what belongs in it |
| L33 | **`verify` reports `pass`, `fail`, or `unverified`** — and `unverified` is never reported as `pass` | P31's fifth instance. The protocol names the case exactly: an OOM-killed linter found nothing because it did not run, which is not the same as finding nothing |
| L34 | **The review gate is on review *state*, and deferred findings go to a durable backlog** | Not every forge can resolve threads. Gating on something a forge may not implement makes the gate unreachable there; gating on state works everywhere |

## Architecture

Almost nothing new. The pieces that are:

```
aeon/internal/loop/
  phases.go     lane → phase state; TRACKED_PHASES (L27)
  counters.go   two-level counters and caps (L28)
  redo.go       reject routing, with the reason carried (L32)
  escalate.go   escalate | fail-item | block, and the unattended collapse (L29)

aeon/internal/plan/
  derive.go     load tasks; validate depends_on; topologically sort (L31)
  sort_test.go

aeon/internal/blueprint/blueprints/
  dev-loop.yaml   the full ladder — every lane now runs

aeon/internal/handler/bodies/
  verifier-engineer.md   + the unverified outcome (L33)
```

### The ladder

```
clarify ──▶ discovery ──▶ legal? ──▶ plan ──▶ [PLAN GATE] ──▶ build (iterate: tasks) ──▶ qa? ──▶ done
                                                                    │
                                        ┌───────────────────────────┴──────────────────────────┐
                                        │  issue → implement → verify → security? → push →     │
                                        │  pr → review → merge                                 │
                                        └──────────────────────────────────────────────────────┘
                                              rejects route to `rework: implement`,
                                              bounded per task at 2 (L28)
```

**One approval gate** is the designed shape — the protocol's own summary says *"investigates → plans → (one approval
gate) → builds task-by-task → verifies each task → commits, running unattended."* Escalations open others, but they are
exceptions rather than steps.

### The three verification states (L33)

| Outcome | Means | Routes as |
| --- | --- | --- |
| `pass` | the check ran and found nothing | `ok` |
| `fail` | the check ran and found something | `reject` |
| **`unverified`** | **the check did not run** — OOM-killed, timed out, tooling absent | **`reject`, with a distinct reason** |

`unverified` routes as a reject because the work cannot proceed on an unknown, but its **reason differs** and that
matters for L32: a redo after `fail` should fix the finding; a redo after `unverified` should make the check run. Those
are different instructions, and collapsing them wastes a round.

### Counters, and where they leak

```
task-level (cap 2)            loop-level
  retries                       total_failed_gates
  security_rounds               splits_done
  review_rounds                 qa_rounds
```

The failure this prevents is specific: **a per-loop-only `review_rounds` means the fifth task cannot be reviewed at
all**, because the first two consumed the budget — and it presents as a loop that escalates for no visible reason on a
task that was never reviewed once.

The loop-level counters exist for the opposite failure: `total_failed_gates` is the ceiling that stops a twenty-task loop
from spending forty rounds and calling it progress.

## The demo

See [`DEMO.md`](DEMO.md): a real feature, specified as a task, built by Aeon, **caught being wrong by its own verify
lane**, fixed inside its cap, reviewed, and merged — with a human at one gate. Plus the arithmetic shown: a task that
exhausts its retries escalates while its siblings keep their own budgets; and an `unverified` lint reports as neither
pass nor fail.

## Testing

| What | How |
| --- | --- |
| **A reject actually happens** | The demo's deliberate defect, and a test: `verify` returning `fail` routes to `rework: implement` and the round is consumed (L30) |
| Two-level counters | A five-task loop where tasks 1 and 2 each consume two `review_rounds`: **task 3 still has two of its own**. The test that fails against a per-loop-only implementation (L28) |
| The loop ceiling | `total_failed_gates` stops a loop whose tasks each individually stay inside their caps |
| `escalate` unattended | Collapses to `block`, naming the counter and that the loop is unattended (L29) |
| Topological sort | A plan with `depends_on` runs in dependency order; a **cycle is refused at load** naming the cycle; a duplicate id and a self-dependency likewise (L31) |
| The position stays monotonic | `iterate` never revisits an earlier item — asserted on the position sequence, since that is what the sort buys |
| A redo carries its reason | The re-entered step's input contains the rejection's reason; asserted by a handler spy reading its own input (L32) |
| `unverified` ≠ `pass` | A verify step whose linter was OOM-killed reports `unverified`; **a test asserts it is never recorded as `pass`** (L33) |
| `unverified` ≠ `fail`'s redo | The two produce **different** reasons in the redo's input, because they call for different work |
| Review state, not threads | The merge gate reads review **state**; a forge with no thread API still reaches the gate. Deferred findings land in the backlog file (L34) |
| Phases | Every tracked lane records a phase status; the iterating lane records **per-item** task state and no single `build` phase status (L27) |
| `depends_on` at completion | A task cannot complete while a dependency is incomplete — S4's machine, driven |
| The full ladder end to end | Against the fake forge: every lane runs, in order, with one gate |

**Two rows carry the slice.** *"A reject actually happens"*, because a ladder that only passes has not been shown to
work. And *"two-level counters"*, because a per-loop-only implementation passes every other test here and starves the
fifth task.

## Out of scope

- **No new mechanism.** If a lane seems to need one, that is a finding — S8 through S16 built the parts.
- **No new tools, no new actions.** The nine and the eight are complete.
- **No second protocol as a blueprint.** `proposal-brief`, `development-map` and `conversational-map` remain unexpressed;
  `hotfix` becomes expressible here and is **S23's** to author if wanted. Expressing one is authorship, and the format
  was already proven by two.
- **No dogfooding.** Aeon does not run this ladder on Aeon's own repository in this slice. A loop that breaks the daemon
  running it is a bad first demonstration, and the dogfooding question belongs after S23's first-run work.
- **No cleanup or branch deletion.** Inherited from S16's N8 and N9, and the reason is unchanged.
- **No retrieval-resolved context.** S22.
- **No blueprint builder.** Never promised for the alpha.

## Amendments to earlier slices

| Where | Change | Status |
| --- | --- | --- |
| S13 | `verifier-engineer`'s handoff gains the `unverified` outcome (L33). Its body already carries the OOM consequence, which S13's T4 kept for exactly this reason | T6 |
| S8 | `dev-loop.yaml` stops being data that validates and becomes a ladder that runs. Its lane set is unchanged; its `sets:` and `on_reject` edges are now exercised | T7 |
| `../ARCHITECTURE.md` §8 P31 | Records verification as the **fifth** place the three-state shape appeared | **landed with this design** |

## Open questions

None. Which lanes are tracked phases is authorship, recorded in `TRACKED_PHASES` and reviewable there.
