# Sketch — S32 Parallel agents

**Status:** sketch, no tasks. **Decision it implements:** [P33](../ARCHITECTURE.md) (2026-10-24, at the user's
direction). **Prefix:** `PA`. **Depends on:** S8 (blueprints, `internal/loop`), S7 (recovery), **S31** (the event
shape that already carries `stepId`/`agent`, so this slice adds none). **Interacts with:** S16 (one working tree,
several writers), S17 (iterating lanes), P28 (memory budget).
**Checked 2026-10-26 against P34** (`ARCHITECTURE.md`): no gap found at sketch depth — `maxParallelAgents`
is a `project.update` field, already a socket method; nothing else here implies a new CLI-only surface.
Whoever writes the full brief should still confirm no task invents a CLI-first control for this setting.

---

## 1. What this is

**A loop can have more than one agent working at the same time.** Today a loop has exactly one position — one lane,
one step ([`internal/loop/state.go`](../../../internal/loop/state.go) `Position`) — so a survey and a lint that
do not depend on each other still run one after the other. This slice lets the daemon run up to
`project.maxParallelAgents` steps of one loop at once.

## 2. What is decided (by the user, 2026-10-24)

| | |
| --- | --- |
| **PA1** | **Parallelism is allowed, and opted into per project.** `project.maxParallelAgents`, an integer, **default 1**. At 1 the engine must behave exactly as today — same events, same order, same rows |
| **PA2** | **The cap is a ceiling, not a target.** The daemon runs *up to* N ready steps; it never invents parallel work the blueprint did not make independent |
| **PA3** | **Only the daemon dispatches.** A1 (nothing spawns), AS4 (async is a suspension) and K9 (tools serial within a step) are unchanged — see P33 |

## 3. What is decided here as a sensible default (the brief writer may revisit, with reasons)

- **Range 1–16**, enforced by a `CHECK` in a new numbered migration (ROADMAP §7, S4 row) and by `project.update`'s
  validator with a refusal that names the value.
- **A suspended step does not hold a slot.** It has no process (P2); an answered question re-queues it for a slot.
- **Steering, stop and continue are already per `stepId`** (S31), so they need no change.

## 4. Open questions — the brief must answer each

| # | Question | Why it is hard |
| --- | --- | --- |
| **Q1** | **What makes two steps independent?** Explicit (`parallel:` on a lane group, a fan-out step) or inferred from the graph (no edge between them)? | Inference is magic that changes behaviour when someone edits an unrelated edge. Explicit is a blueprint schema change (`schemaVersion`, P15) |
| **Q2** | **One working tree, several writers.** Serialise writes per path? Per-step git worktree and a merge step? Refuse parallel lanes whose declared `produces` overlap? | Two agents editing the same file is the failure this whole feature can cause. S16's code actions must not assume a single writer |
| **Q3** | **Recovery with several `running` steps.** S7 today recovers one step per loop | Each must resume from its own checkpoint; a crash between "slot granted" and "step row created" must not lose or double-start a step |
| **Q4** | **Gates and questions while siblings run.** Does an approval gate in one lane pause the others? | A gate that means "stop everything" and one that means "this lane waits" are different, and the blueprint has to say which |
| **Q5** | **Failure of one parallel step.** Cancel siblings, let them finish, or block the join? | The default has to be the one that wastes least and surprises least |
| **Q6** | **Budgets.** P28's per-loop memory budget and any cost cap cover the sum of concurrent steps | A per-step budget × N silently multiplies the ceiling |
| **Q7** | **Lowering the cap while N > new cap are running.** | Never kill running work to honour a setting change; drain to the new cap |

## 5. Definition of done (sketch-level)

- A two-lane blueprint with independent lanes runs both at once when `maxParallelAgents ≥ 2`, and serially at 1 —
  **same rows and same final state either way**.
- `kill -9` with two steps mid-turn recovers both, from their own checkpoints.
- The S31 loop view shows the two agents interleaved with no UI change beyond what S31 already shipped.
